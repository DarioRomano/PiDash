package server

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DarioRomano/PiDash/internal/apps"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/sound"
	"github.com/DarioRomano/PiDash/internal/tv"
)

func TestSoundOverHTTP(t *testing.T) {
	audio := sound.NewMock()
	cfg := config.Default()
	cfg.Remote.PIN = "4711" // the settings lock does not stand in the way of the volume
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	s := New(cfg, log, static, Deps{
		Kiosk: kiosk.New("", true, log), Apps: apps.NewManager(nil, &apps.Mock{}, log),
		Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}), Sound: audio,
		NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
	})
	srv := httptest.NewServer(s.mux)
	defer srv.Close()

	read := func(body string) map[string]sound.Output {
		var st sound.Status
		if err := json.Unmarshal([]byte(body), &st); err != nil || !st.Available {
			t.Fatalf("%v: %s", err, body)
		}
		out := map[string]sound.Output{}
		for _, o := range st.Outputs {
			out[o.ID] = o
		}
		return out
	}
	res, err := http.Get(srv.URL + "/api/sound")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if o := read(string(body)); !o["hdmi"].Active || o["speaker"].Active || o["speaker"].Volume != 40 {
		t.Fatalf("outputs = %+v", o)
	}

	if code, body := post(t, srv.URL+"/api/sound/use", `{"id":"speaker"}`); code != 200 || !read(body)["speaker"].Active || read(body)["hdmi"].Active {
		t.Errorf("use: %d %s", code, body)
	}
	if code, body := post(t, srv.URL+"/api/sound/volume", `{"id":"speaker","volume":65}`); code != 200 || read(body)["speaker"].Volume != 65 {
		t.Errorf("volume: %d %s", code, body)
	}
	if code, body := post(t, srv.URL+"/api/sound/mute", `{"id":"speaker","muted":true}`); code != 200 || !read(body)["speaker"].Muted {
		t.Errorf("mute: %d %s", code, body)
	}
	if code, _ := post(t, srv.URL+"/api/sound/test", `{"id":"speaker"}`); code != 200 || audio.Tested["speaker"] != 1 {
		t.Errorf("test sound: %d, played %d times", code, audio.Tested["speaker"])
	}
	if code, body := post(t, srv.URL+"/api/sound/use", `{"id":"garden"}`); code != 404 || !strings.Contains(body, "not there") {
		t.Errorf("unknown output: %d %s", code, body)
	}
	if code, _ := post(t, srv.URL+"/api/sound/explode", `{}`); code != 404 {
		t.Errorf("unknown action: %d", code)
	}
}

func soundServerFor(t *testing.T, audio *sound.Mock, follow bool) *Server {
	t.Helper()
	cfg := config.Default()
	cfg.Sound.FollowBluetooth = follow
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	return New(cfg, log, static, Deps{
		Kiosk: kiosk.New("", true, log), Apps: apps.NewManager(nil, &apps.Mock{}, log),
		Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}), Sound: audio,
		NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
	})
}

func activeOutput(audio *sound.Mock) string {
	for _, o := range audio.Status(context.Background()).Outputs {
		if o.Active {
			return o.ID
		}
	}
	return ""
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never happened: %s", what)
}

func TestTheSoundFollowsHeadphonesAndTheDashboardIsTold(t *testing.T) {
	audio := sound.NewMock()
	audio.Disconnect(sound.Speaker.ID) // PiDash starts with the TV alone
	s := soundServerFor(t, audio, true)
	events := s.hub.subscribe()
	defer s.hub.unsubscribe(events)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.followSound(ctx)
	told := func() string { // the outputs in the latest "sound" event, the one in use marked
		last := ""
		for {
			select {
			case m := <-events:
				if m.name == "sound" {
					var st sound.Status
					json.Unmarshal(m.data, &st)
					last = ""
					for _, o := range st.Outputs {
						last += o.ID
						if o.Active {
							last += "*"
						}
						last += " "
					}
				}
			default:
				return strings.TrimSpace(last)
			}
		}
	}
	eventually(t, "the dashboard is told the outputs at the start", func() bool { return told() == "hdmi*" })

	audio.Connect(sound.Speaker)
	eventually(t, "the sound moves to the headphones", func() bool { return activeOutput(audio) == "speaker" })
	eventually(t, "the dashboard is told", func() bool { return told() == "hdmi speaker*" })

	audio.Disconnect(sound.Speaker.ID)
	eventually(t, "the sound is back on the TV", func() bool { return activeOutput(audio) == "hdmi" })
	eventually(t, "the dashboard is told", func() bool { return told() == "hdmi*" })
}

func TestWithFollowingSwitchedOffTheSoundStaysPut(t *testing.T) {
	audio := sound.NewMock()
	audio.Disconnect(sound.Speaker.ID)
	s := soundServerFor(t, audio, false)
	events := s.hub.subscribe()
	defer s.hub.unsubscribe(events)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.followSound(ctx)
	time.Sleep(50 * time.Millisecond)
	for len(events) > 0 {
		<-events
	}
	audio.Connect(sound.Speaker)
	// The dashboard still learns that there is a new output to choose.
	eventually(t, "the dashboard is told about the new output", func() bool { return len(events) > 0 })
	time.Sleep(100 * time.Millisecond)
	if got := activeOutput(audio); got != "hdmi" {
		t.Errorf("the sound moved to %q although following is switched off", got)
	}
}
