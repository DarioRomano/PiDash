package server

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DarioRomano/PiDash/internal/apps"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/power"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/tv"
)

func powerServer(t *testing.T, pin string, control power.Control) (*Server, *httptest.Server) {
	t.Helper()
	cfg := config.Default()
	cfg.Remote.PIN = pin
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	s := New(cfg, log, static, Deps{
		Kiosk: kiosk.New("", true, log), Apps: apps.NewManager(nil, &apps.Mock{}, log),
		Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}), Power: control,
		NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
	})
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return s, srv
}

func TestPowerFromTheTV(t *testing.T) {
	control := &power.Mock{}
	s, srv := powerServer(t, "", control)
	if code, _ := post(t, srv.URL+"/api/power/reboot", ""); code != 204 {
		t.Errorf("reboot: %d", code)
	}
	if code, _ := post(t, srv.URL+"/api/power/off", ""); code != 204 {
		t.Errorf("off: %d", code)
	}
	if got := control.Done(); got != "[reboot poweroff]" {
		t.Errorf("asked = %s", got)
	}
	if code, _ := post(t, srv.URL+"/api/power/format-disk", ""); code != 404 {
		t.Errorf("something else: %d, want 404", code)
	}
	if got := control.Done(); got != "[reboot poweroff]" {
		t.Errorf("an unknown request did something: %s", got)
	}

	// Restarting PiDash itself makes the program start again.
	if code, _ := post(t, srv.URL+"/api/power/restart", ""); code != 204 {
		t.Errorf("restart: %d", code)
	}
	select {
	case <-s.restart:
	case <-time.After(3 * time.Second):
		t.Error("PiDash was not restarted")
	}
}

func TestPowerNeedsThePINAndSaysWhenThePiRefuses(t *testing.T) {
	control := &power.Mock{}
	_, srv := powerServer(t, "4711", control)
	for _, action := range []string{"reboot", "off", "restart"} {
		if code, _ := post(t, srv.URL+"/api/power/"+action, ""); code != 401 {
			t.Errorf("%s without the PIN: %d, want 401", action, code)
		}
	}
	if got := control.Done(); got != "[]" {
		t.Errorf("done without the PIN: %s", got)
	}
	if code, _ := post(t, srv.URL+"/api/power/reboot", "", pinHeader, "4711"); code != 204 {
		t.Errorf("with the PIN: %d", code)
	}

	refuses := &power.Mock{Fail: io.ErrClosedPipe}
	_, srv = powerServer(t, "", refuses)
	if code, body := post(t, srv.URL+"/api/power/off", ""); code != 403 || strings.TrimSpace(body) == "" {
		t.Errorf("a Pi that refuses: %d %q", code, body)
	}
}
