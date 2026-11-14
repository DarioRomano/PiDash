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
	"github.com/DarioRomano/PiDash/internal/logring"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/tv"
)

func TestStatusReport(t *testing.T) {
	cfg := config.Default()
	cfg.Apps = []config.App{{ID: "netflix", Name: "Netflix", URL: "https://www.netflix.com", Mode: "desktop", Browser: "chromium"}}
	cfg.Calendar.URLs = []string{"https://calendar.example/private-secret-token.ics"}
	cfg.Home.Token = "home-secret-token"
	cfg.Photos.Synology.Password = "nas-secret-password"
	cfg.Remote.PIN = "4711"

	ring := logring.New(10)
	log := slog.New(ring.Handler(slog.NewTextHandler(io.Discard, nil)))
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	s := New(cfg, log, static, Deps{
		Kiosk: kiosk.New("", true, log), Apps: apps.NewManager(cfg.Apps, &apps.Mock{}, log),
		Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}),
		Version: "v9.9.9", Notices: []string{"Port 8080 was taken"},
		Remote: &input.Remote{}, Problems: ring,
		NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
	})
	srv := httptest.NewServer(s.mux)
	defer srv.Close()
	log.Warn("cannot reach the NAS", "err", "timeout")

	res, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var rep statusReport
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	if rep.Version != "v9.9.9" || rep.Started == 0 {
		t.Errorf("version/started = %q/%d", rep.Version, rep.Started)
	}
	if rep.TV == nil || rep.Remote == nil || rep.Remote.Devices == nil || rep.Remote.Last != nil {
		t.Errorf("tv = %+v, remote = %+v", rep.TV, rep.Remote)
	}
	if rep.Apps.Count != 1 {
		t.Errorf("apps = %+v", rep.Apps)
	}
	if len(rep.Notices) != 1 || len(rep.Problems) != 1 || rep.Problems[0].Text != "cannot reach the NAS: timeout" {
		t.Errorf("notices = %v, problems = %+v", rep.Notices, rep.Problems)
	}
	for _, secret := range []string{"secret", "4711"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the status report gives away %q:\n%s", secret, raw)
		}
	}
}

func TestStatusReportWithoutTheOptionalParts(t *testing.T) {
	srv, _, _ := newTestServer(t, nil)
	res, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var rep map[string]any
	if err := json.NewDecoder(res.Body).Decode(&rep); err != nil {
		t.Fatal(err)
	}
	if _, has := rep["remote"]; has {
		t.Error("no remote is read in this setup, so none should be reported")
	}
	for _, list := range []string{"sources", "notices", "problems"} {
		if _, ok := rep[list].([]any); !ok {
			t.Errorf("%s should be a list, also when empty: %v", list, rep[list])
		}
	}
}

type countingBrowser struct{ restarts int }

func (c *countingBrowser) Active(context.Context) bool { return true }
func (c *countingBrowser) Restart(context.Context) error {
	c.restarts++
	return nil
}

func TestOnlyTheBrowserOnThePiKeepsTheWatchdogQuiet(t *testing.T) {
	cfg := config.Default()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	browser := &countingBrowser{}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	dog := &kiosk.Watchdog{Browser: browser, Log: log, Now: func() time.Time { return now }}
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	s := New(cfg, log, static, Deps{
		Kiosk: kiosk.New("", true, log), Apps: apps.NewManager(nil, &apps.Mock{}, log),
		Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}), Watchdog: dog,
		NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
	})
	alive := func(from string) {
		req := httptest.NewRequest("POST", "/api/alive", nil)
		req.RemoteAddr = from
		rec := httptest.NewRecorder()
		s.mux.ServeHTTP(rec, req)
		if rec.Code != 204 {
			t.Fatalf("alive from %s: %d", from, rec.Code)
		}
	}
	ctx := context.Background()
	dog.Check(ctx)
	alive("127.0.0.1:40000") // the dashboard on the TV has loaded

	// A phone that has the dashboard open says nothing about the TV.
	now = now.Add(2 * time.Minute)
	alive("192.168.1.50:51234")
	now = now.Add(2 * time.Minute)
	if !dog.Check(ctx) || browser.restarts != 1 {
		t.Fatalf("reports from a phone kept the watchdog quiet: %d restarts", browser.restarts)
	}
	// The browser on the Pi itself does, over either kind of address.
	for _, from := range []string{"127.0.0.1:40000", "[::1]:40000"} {
		now = now.Add(2 * time.Minute)
		alive(from)
		now = now.Add(2 * time.Minute)
		if dog.Check(ctx) {
			t.Errorf("a report from %s did not count", from)
		}
	}
	if browser.restarts != 1 {
		t.Errorf("restarts = %d, want 1", browser.restarts)
	}
}
