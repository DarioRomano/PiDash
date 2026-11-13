package server

import (
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

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
