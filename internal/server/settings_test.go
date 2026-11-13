package server

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DarioRomano/PiDash/internal/apps"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/settings"
	"github.com/DarioRomano/PiDash/internal/tv"
)

func newSettingsServer(t *testing.T, pin string) (*Server, *httptest.Server, string) {
	t.Helper()
	base := config.Default()
	base.Remote.PIN = pin
	path := filepath.Join(t.TempDir(), "settings.json")
	store, err := settings.Open(path, base)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var static fs.FS = fstest.MapFS{"settings.html": {Data: []byte("<html>settings</html>")}}
	s := New(base, log, static, Deps{
		Kiosk: kiosk.New("", true, log), Apps: apps.NewManager(nil, &apps.Mock{}, log),
		Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}),
		NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
		Settings:  store, BaseListen: base.Server.Listen,
	})
	s.weatherLookup = func(_ context.Context, location string) (string, error) {
		return location + ", Testland", nil
	}
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return s, srv, path
}

func put(t *testing.T, url, body string, headers ...string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
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

func restartRequested(s *Server) bool {
	select {
	case <-s.restart:
		return true
	case <-time.After(2 * time.Second):
		return false
	}
}

func TestSettingsPageSavesAndAsksForARestart(t *testing.T) {
	s, srv, path := newSettingsServer(t, "")

	res, _ := http.Get(srv.URL + "/api/settings")
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	for _, want := range []string{`"presets"`, `"youtube"`, `"consoleReady"`, `"weather"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("settings view is missing %s", want)
		}
	}
	if res, _ := http.Get(srv.URL + "/settings"); res.StatusCode != http.StatusOK {
		t.Errorf("settings page: %d", res.StatusCode)
	}

	// A mistake is explained and changes nothing.
	code, msg := put(t, srv.URL+"/api/settings", `{"photos":{"source":"synology","synology":{"url":"http://nas:5000","quality":"original"}}}`)
	if code != http.StatusUnprocessableEntity || !strings.Contains(msg, "username and password") {
		t.Errorf("invalid settings: %d %q", code, msg)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("invalid settings were written")
	}

	// Valid settings are stored, and the backend restarts to apply them.
	code, msg = put(t, srv.URL+"/api/settings", `{"weather":{"location":"Zurich","units":"metric"},
		"photos":{"source":"synology","intervalSeconds":40,"synology":{"url":"http://nas:5000","username":"pidash","password":"s3cret","quality":"original"}},
		"apps":[{"kind":"preset","preset":"netflix"}]}`)
	if code != http.StatusOK || !strings.Contains(msg, "restarting") {
		t.Fatalf("save: %d %q", code, msg)
	}
	if !restartRequested(s) {
		t.Error("saving should restart the backend")
	}
	saved, _ := os.ReadFile(path)
	if !strings.Contains(string(saved), "Zurich") || !strings.Contains(string(saved), "netflix") {
		t.Errorf("settings.json = %s", saved)
	}

	// The page never gets the password back.
	res, _ = http.Get(srv.URL + "/api/settings")
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if strings.Contains(string(body), "s3cret") || !strings.Contains(string(body), `"passwordSet":true`) {
		t.Errorf("settings view after saving: %s", body)
	}
}

func TestSettingsNeedThePINWhenOneIsSet(t *testing.T) {
	s, srv, path := newSettingsServer(t, "4321")
	body := `{"weather":{"location":"Zurich","units":"metric"}}`

	if code, _ := put(t, srv.URL+"/api/settings", body); code != http.StatusUnauthorized {
		t.Errorf("save without PIN: %d, want 401", code)
	}
	if code, _ := post(t, srv.URL+"/api/settings/test-synology", `{"url":"http://nas","username":"a","password":"b"}`); code != http.StatusUnauthorized {
		t.Errorf("NAS test without PIN: %d, want 401", code)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("settings were written without the PIN")
	}
	// Reading the settings stays open, like the rest of the remote.
	if res, _ := http.Get(srv.URL + "/api/settings"); res.StatusCode != http.StatusOK {
		t.Errorf("read: %d", res.StatusCode)
	}
	if code, _ := put(t, srv.URL+"/api/settings", body, "X-PiDash-PIN", "4321"); code != http.StatusOK {
		t.Errorf("save with PIN: %d", code)
	}
	if !restartRequested(s) {
		t.Error("no restart after saving with the PIN")
	}
}

func TestChecksOnTheSettingsPage(t *testing.T) {
	_, srv, _ := newSettingsServer(t, "")

	code, body := post(t, srv.URL+"/api/settings/test-weather", `{"location":" Zurich "}`)
	if code != http.StatusOK || !strings.Contains(body, "Zurich, Testland") {
		t.Errorf("weather check: %d %s", code, body)
	}
	if code, _ := post(t, srv.URL+"/api/settings/test-weather", `{"location":""}`); code != http.StatusUnprocessableEntity {
		t.Errorf("empty place: %d", code)
	}
	if code, msg := post(t, srv.URL+"/api/settings/test-synology", `{"url":"http://nas:5000","username":"pidash"}`); code != http.StatusUnprocessableEntity || !strings.Contains(msg, "password") {
		t.Errorf("NAS test without password: %d %q", code, msg)
	}
	// An address where nothing answers is explained, not a server error.
	code, msg := post(t, srv.URL+"/api/settings/test-synology", `{"url":"http://127.0.0.1:1","username":"a","password":"b"}`)
	if code != http.StatusBadGateway || !strings.Contains(msg, "cannot reach the NAS") {
		t.Errorf("unreachable NAS: %d %q", code, msg)
	}
}

func TestPortIsRememberedSoTheMessageShowsOnce(t *testing.T) {
	busy := takePort(t)
	base := config.Default()
	base.Server.Listen = "127.0.0.1:" + strconv.Itoa(busy)
	path := filepath.Join(t.TempDir(), "settings.json")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("x")}}

	start := func() (notices int, listen string, stop func()) {
		store, err := settings.Open(path, base)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := store.Effective()
		if err != nil {
			t.Fatal(err)
		}
		urlFile := filepath.Join(t.TempDir(), "url")
		s := New(cfg, log, static, Deps{
			Kiosk: kiosk.New("", true, log), Apps: apps.NewManager(nil, &apps.Mock{}, log),
			Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}),
			NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
			Settings:  store, BaseListen: base.Server.Listen, URLFile: urlFile,
		})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(urlFile); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		return len(s.clientConfig().Notices), cfg.Server.Listen, func() { cancel(); <-done }
	}

	notices, listen, stop := start()
	if notices != 1 || listen != base.Server.Listen {
		t.Errorf("first start: %d notices, listen %s", notices, listen)
	}
	stop()

	// Second start: straight to the remembered port, and nothing to announce.
	notices, listen, stop = start()
	defer stop()
	if notices != 0 || listen == base.Server.Listen {
		t.Errorf("second start: %d notices, listen %s; want no notice and the remembered port", notices, listen)
	}
}
