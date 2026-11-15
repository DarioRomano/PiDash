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
	"github.com/DarioRomano/PiDash/internal/cec"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/tv"
)

type fakeTV struct{ standbys, wakes int }

func (f *fakeTV) Available() bool                                { return true }
func (f *fakeTV) Standby(context.Context) error                  { f.standbys++; return nil }
func (f *fakeTV) Wake(context.Context) error                     { f.wakes++; return nil }
func (f *fakeTV) PowerStatus(context.Context) (cec.Power, error) { return cec.PowerUnknown, nil }

func newTestServer(t *testing.T, mutate func(*config.Config)) (*httptest.Server, *fakeTV, *apps.Manager) {
	t.Helper()
	cfg := config.Default()
	cfg.Apps = []config.App{{ID: "netflix", Name: "Netflix", URL: "https://www.netflix.com", Mode: "desktop", Browser: "chromium"}}
	if mutate != nil {
		mutate(&cfg)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	set := &fakeTV{}
	am := apps.NewManager(cfg.Apps, &apps.Mock{}, log)
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}, "remote.html": {Data: []byte("<html>remote</html>")}}
	s := New(cfg, log, static, Deps{
		Kiosk: kiosk.New("", true, log), Apps: am,
		Input: input.NewService(cfg.Remote.MouseKeyboard, log),
		TV:    tv.New(tv.Options{TV: set, Log: log}),
		NewRunner: func(publish func(providers.Entry)) *providers.Runner {
			return providers.NewRunner(log, publish)
		},
	})
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv, set, am
}

func post(t *testing.T, url, body string, headers ...string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, strings.TrimSpace(string(b))
}

func TestPhoneMouseIsOffByDefault(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)

	var info remoteInfo
	res, _ := http.Get(srv.URL + "/api/remote")
	json.NewDecoder(res.Body).Decode(&info)
	res.Body.Close()
	if info.Input.Enabled || info.Input.Available || info.Input.PINRequired {
		t.Errorf("input should be off by default: %+v", info.Input)
	}
	if !info.TV.Available {
		t.Errorf("tv = %+v", info.TV)
	}

	if code, body := post(t, srv.URL+"/api/pointer", `{"dx":5,"dy":3}`); code != http.StatusForbidden || !strings.Contains(body, "mouse_keyboard") {
		t.Errorf("pointer while disabled: %d %q", code, body)
	}
	if code, _ := post(t, srv.URL+"/api/type", `{"text":"hello"}`); code != http.StatusForbidden {
		t.Errorf("typing while disabled: %d", code)
	}
}

func TestPINProtectsThePhoneMouseAndKeyboard(t *testing.T) {
	srv, _, _ := newTestServer(t, func(c *config.Config) {
		c.Remote.MouseKeyboard = true
		c.Remote.PIN = "4321"
	})

	var info remoteInfo
	res, _ := http.Get(srv.URL + "/api/remote")
	json.NewDecoder(res.Body).Decode(&info)
	res.Body.Close()
	if !info.Input.Enabled || !info.Input.PINRequired {
		t.Errorf("info = %+v", info.Input)
	}

	if code, _ := post(t, srv.URL+"/api/pointer", `{"dx":1}`); code != http.StatusUnauthorized {
		t.Errorf("no PIN: %d, want 401", code)
	}
	// The right PIN gets past the guard. (There is no /dev/uinput on the test
	// machine, so the request then fails for that reason, not for the PIN.)
	if code, body := post(t, srv.URL+"/api/pointer", `{"dx":1}`, "X-PiDash-PIN", "4321"); code == http.StatusUnauthorized || code == http.StatusTooManyRequests {
		t.Errorf("right PIN was rejected: %d %q", code, body)
	}
	// Guessing locks the PIN for a while, even for the right one.
	for i := 0; i < 5; i++ {
		if code, _ := post(t, srv.URL+"/api/type", `{"text":"x"}`, "X-PiDash-PIN", "000"+string(rune('0'+i))); code != http.StatusUnauthorized {
			t.Errorf("wrong PIN %d: %d, want 401", i, code)
		}
	}
	if code, _ := post(t, srv.URL+"/api/pointer", `{"dx":1}`, "X-PiDash-PIN", "4321"); code != http.StatusTooManyRequests {
		t.Errorf("after five wrong PINs: %d, want 429", code)
	}
}

func TestMissingPINDoesNotCountAsAGuess(t *testing.T) {
	g := pinGuard{pin: "4321"}
	for i := 0; i < 20; i++ {
		rec := httptest.NewRecorder()
		if g.allow(rec, httptest.NewRequest(http.MethodPost, "/", nil)) || rec.Code != http.StatusUnauthorized {
			t.Fatalf("request %d without PIN: %d", i, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set(pinHeader, "4321")
	if !g.allow(httptest.NewRecorder(), req) {
		t.Error("the right PIN should still work after many requests without one")
	}
	g.lockedUntil = time.Now().Add(-time.Second)
}

func TestPhoneButtonsReachTheDashboardWhenNoAppIsOpen(t *testing.T) {
	srv, _, am := newTestServer(t, nil)

	// Listen on the event stream like the dashboard does.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	events := make(chan string, 64)
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := res.Body.Read(buf)
			if n > 0 {
				events <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	waitFor := func(substr string) bool {
		deadline := time.After(3 * time.Second)
		for {
			select {
			case e := <-events:
				if strings.Contains(e, substr) {
					return true
				}
			case <-deadline:
				return false
			}
		}
	}
	if !waitFor("event: tv") {
		t.Fatal("the stream should start with the current state, including the TV")
	}

	if code, _ := post(t, srv.URL+"/api/input", `{"action":"Left"}`); code != http.StatusNoContent {
		t.Fatalf("input: %d", code)
	}
	if !waitFor(`"action":"left"`) {
		t.Error("the button press did not reach the dashboard")
	}

	// With an app open and no virtual keyboard, the press still goes to the
	// dashboard rather than being lost.
	if err := am.Start(context.Background(), "netflix"); err != nil {
		t.Fatal(err)
	}
	post(t, srv.URL+"/api/input", `{"action":"select"}`)
	if !waitFor(`"action":"select"`) {
		t.Error("the press was lost while an app was open")
	}
}

func TestTVPowerFromThePhone(t *testing.T) {
	srv, set, _ := newTestServer(t, nil)
	if code, body := post(t, srv.URL+"/api/tv", `{"power":"off"}`); code != http.StatusOK || !strings.Contains(body, `"asleep":true`) {
		t.Errorf("off: %d %s", code, body)
	}
	if code, body := post(t, srv.URL+"/api/tv", `{"power":"on"}`); code != http.StatusOK || !strings.Contains(body, `"asleep":false`) {
		t.Errorf("on: %d %s", code, body)
	}
	if set.standbys != 1 || set.wakes != 1 {
		t.Errorf("standbys=%d wakes=%d", set.standbys, set.wakes)
	}
	if code, _ := post(t, srv.URL+"/api/tv", `{"power":"reboot"}`); code != http.StatusBadRequest {
		t.Errorf("bad power value: %d", code)
	}
}
