package apps

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DarioRomano/PiDash/internal/config"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeBrowsers puts empty executables with the given names first in PATH.
func fakeBrowsers(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

// fakeAddonSite serves a minimal zip-looking file in place of addons.mozilla.org.
func fakeAddonSite(t *testing.T, status int, body string) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	old := UBlockURL
	UBlockURL = srv.URL
	t.Cleanup(func() { UBlockURL = old })
	return &hits
}

const youTubeUA = "Mozilla/5.0 (PS4; Leanback Shell) Gecko/20100101 Firefox/65.0 LeanbackShell/01.00.01.75 Sony PS4/ (PS4, , no, CH)"

func TestFirefoxWebAppWithAdBlock(t *testing.T) {
	bin := fakeBrowsers(t, "firefox")
	hits := fakeAddonSite(t, 200, "PK\x03\x04fake-xpi")
	data := t.TempDir()
	app := config.App{ID: "youtube", URL: "https://www.youtube.com/tv", Browser: config.BrowserFirefox, AdBlock: true, UserAgent: youTubeUA}

	launch, err := PrepareWebApp(app, data, []string{"WAYLAND_DISPLAY=wayland-0"}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(data, "profiles", "youtube")
	want := []string{filepath.Join(bin, "firefox"), "--kiosk", "--no-remote", "--profile", profile, "--class", "pidash-youtube", "https://www.youtube.com/tv"}
	if !slices.Equal(launch.Argv, want) {
		t.Errorf("argv = %q\nwant  %q", launch.Argv, want)
	}
	if !slices.Contains(launch.Env, "MOZ_ENABLE_WAYLAND=1") {
		t.Errorf("env should enable Wayland: %v", launch.Env)
	}

	xpi := filepath.Join(profile, "extensions", "uBlock0@raymondhill.net.xpi")
	if b, err := os.ReadFile(xpi); err != nil || string(b) != "PK\x03\x04fake-xpi" {
		t.Errorf("uBlock not installed into the profile: %v", err)
	}
	prefs, _ := os.ReadFile(filepath.Join(profile, "user.js"))
	for _, line := range []string{
		`user_pref("extensions.autoDisableScopes", 0);`,
		`user_pref("extensions.startupScanScopes", 1);`,
		`user_pref("general.useragent.override", "` + youTubeUA + `");`,
		`user_pref("media.eme.enabled", true);`,
		`user_pref("browser.aboutwelcome.enabled", false);`,
	} {
		if !strings.Contains(string(prefs), line) {
			t.Errorf("user.js is missing %s", line)
		}
	}

	// Second launch: the add-on is already there, so nothing is downloaded again.
	if _, err := PrepareWebApp(app, data, nil, quiet); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("add-on downloaded %d times, want once", n)
	}
}

func TestFirefoxLaunchesWithoutAdBlockWhenDownloadFails(t *testing.T) {
	fakeBrowsers(t, "firefox-esr")
	for name, site := range map[string]struct {
		status int
		body   string
	}{
		"server error":   {500, "oops"},
		"not an archive": {200, "<html>blocked</html>"},
	} {
		fakeAddonSite(t, site.status, site.body)
		data := t.TempDir()
		app := config.App{ID: "yt", URL: "https://www.youtube.com/", Browser: config.BrowserFirefox, AdBlock: true}
		launch, err := PrepareWebApp(app, data, nil, quiet)
		if err != nil {
			t.Fatalf("%s: launch should still work: %v", name, err)
		}
		if filepath.Base(launch.Argv[0]) != "firefox-esr" {
			t.Errorf("%s: argv[0] = %q", name, launch.Argv[0])
		}
		entries, _ := os.ReadDir(filepath.Join(data, "profiles", "yt", "extensions"))
		if len(entries) != 0 {
			t.Errorf("%s: a bad download must not be left in the profile: %v", name, entries)
		}
	}
}

func TestFirefoxWithoutUserAgentClearsOverride(t *testing.T) {
	fakeBrowsers(t, "firefox")
	data := t.TempDir()
	app := config.App{ID: "plain", URL: "https://example.com/", Browser: config.BrowserFirefox}
	if _, err := PrepareWebApp(app, data, nil, quiet); err != nil {
		t.Fatal(err)
	}
	prefs, _ := os.ReadFile(filepath.Join(data, "profiles", "plain", "user.js"))
	if !strings.Contains(string(prefs), `user_pref("general.useragent.override", "");`) {
		t.Error("user.js should reset the user agent override")
	}
	if strings.Contains(string(prefs), "autoDisableScopes") {
		t.Error("add-on settings should only be written with adblock = true")
	}
}

func TestChromiumWebApp(t *testing.T) {
	bin := fakeBrowsers(t, "chromium-browser")
	data := t.TempDir()
	profile := filepath.Join(data, "profiles", "netflix")
	os.MkdirAll(filepath.Join(profile, "Default"), 0o700)
	os.WriteFile(filepath.Join(profile, "Default", "Preferences"), []byte(`{"profile":{"exit_type":"Crashed","exited_cleanly":false}}`), 0o600)

	app := config.App{ID: "netflix", URL: "https://www.netflix.com/browse", Browser: config.BrowserChromium, UserAgent: "TestUA/1.0", Zoom: 1.5}
	launch, err := PrepareWebApp(app, data, []string{"DISPLAY=:0"}, quiet)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Argv[0] != filepath.Join(bin, "chromium-browser") || launch.Argv[len(launch.Argv)-1] != "https://www.netflix.com/browse" {
		t.Errorf("argv = %q", launch.Argv)
	}
	for _, flag := range []string{"--kiosk", "--ozone-platform=x11", "--user-data-dir=" + profile, "--user-agent=TestUA/1.0", "--force-device-scale-factor=1.50"} {
		if !slices.Contains(launch.Argv, flag) {
			t.Errorf("argv is missing %s: %q", flag, launch.Argv)
		}
	}
	prefs, _ := os.ReadFile(filepath.Join(profile, "Default", "Preferences"))
	if strings.Contains(string(prefs), "Crashed") || strings.Contains(string(prefs), `"exited_cleanly":false`) {
		t.Errorf("crash flag not cleared: %s", prefs)
	}
}

func TestMissingBrowserIsReportedClearly(t *testing.T) {
	fakeBrowsers(t) // empty PATH
	_, err := PrepareWebApp(config.App{ID: "a", URL: "https://example.com", Browser: config.BrowserFirefox}, t.TempDir(), nil, quiet)
	if err == nil || !strings.Contains(err.Error(), "Firefox is not installed") {
		t.Errorf("err = %v", err)
	}
}

func TestZoomReachesTheBrowser(t *testing.T) {
	plain := firefoxPrefs(config.App{ID: "a", URL: "https://example.org"})
	if !strings.Contains(plain, `user_pref("layout.css.devPixelsPerPx", "-1.0");`) {
		t.Errorf("without zoom Firefox must follow the screen:\n%s", plain)
	}
	big := firefoxPrefs(config.App{ID: "a", URL: "https://example.org", Zoom: 1.5})
	if !strings.Contains(big, `user_pref("layout.css.devPixelsPerPx", "1.50");`) {
		t.Errorf("zoom 1.5 is missing from the Firefox profile:\n%s", big)
	}
}
