package apps

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

// uBlock Origin, as published on addons.mozilla.org. The "latest" address
// always serves the current signed release.
const uBlockID = "uBlock0@raymondhill.net"

// UBlockURL is where the ad blocker is downloaded from (a variable for tests).
var UBlockURL = "https://addons.mozilla.org/firefox/downloads/latest/ublock-origin/latest.xpi"

// WebLaunch is the prepared command for a web app.
type WebLaunch struct {
	Argv []string
	Env  []string
}

// PrepareWebApp builds the browser command for an app defined by a URL.
//
// Every web app gets its own browser profile under dataDir/profiles/<id>, so
// its logins and settings are separate from your normal browser and from the
// other apps, and survive restarts.
func PrepareWebApp(app config.App, dataDir string, env []string, log *slog.Logger) (WebLaunch, error) {
	if app.URL == "" {
		return WebLaunch{}, errors.New("not a web app")
	}
	profile := filepath.Join(dataDir, "profiles", app.ID)
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return WebLaunch{}, err
	}
	wayland := envValue(env, "WAYLAND_DISPLAY") != ""

	switch app.Browser {
	case config.BrowserFirefox:
		bin, err := findBinary("firefox", "firefox-esr")
		if err != nil {
			return WebLaunch{}, fmt.Errorf("Firefox is not installed (sudo apt install firefox): %w", err)
		}
		if err := os.WriteFile(filepath.Join(profile, "user.js"), []byte(firefoxPrefs(app)), 0o600); err != nil {
			return WebLaunch{}, err
		}
		if app.AdBlock {
			// Not fatal: without network the app still opens, just without the blocker.
			if err := ensureUBlock(profile); err != nil {
				log.Warn("could not install uBlock Origin; continuing without it", "app", app.ID, "err", err)
			}
		}
		if wayland {
			env = append(env, "MOZ_ENABLE_WAYLAND=1")
		}
		return WebLaunch{
			Argv: []string{bin, "--kiosk", "--no-remote", "--profile", profile, "--class", "pidash-" + app.ID, app.URL},
			Env:  env,
		}, nil

	case config.BrowserChromium:
		bin, err := findBinary("chromium", "chromium-browser")
		if err != nil {
			return WebLaunch{}, fmt.Errorf("Chromium is not installed (sudo apt install chromium): %w", err)
		}
		clearChromiumCrashFlag(profile)
		platform := "x11"
		if wayland {
			platform = "wayland"
		}
		argv := []string{bin,
			"--kiosk",
			"--ozone-platform=" + platform,
			"--user-data-dir=" + profile,
			"--class=pidash-" + app.ID,
			"--no-first-run",
			"--no-default-browser-check",
			"--password-store=basic",
			"--noerrdialogs",
			"--disable-infobars",
			"--disable-session-crashed-bubble",
			"--hide-crash-restore-bubble",
			"--disable-features=TranslateUI,Translate",
			"--autoplay-policy=no-user-gesture-required",
		}
		if app.UserAgent != "" {
			argv = append(argv, "--user-agent="+app.UserAgent)
		}
		return WebLaunch{Argv: append(argv, app.URL), Env: env}, nil
	}
	return WebLaunch{}, fmt.Errorf("unknown browser %q", app.Browser)
}

// firefoxPrefs returns the profile's user.js. It is rewritten on every launch,
// so changes in config.toml always take effect.
func firefoxPrefs(app config.App) string {
	var b strings.Builder
	b.WriteString("// Written by PiDash on every launch of this app. Edits here are overwritten.\n")
	pref := func(name string, value any) {
		var v string
		switch x := value.(type) {
		case string:
			v = strconv.Quote(x)
		default:
			v = fmt.Sprint(x)
		}
		fmt.Fprintf(&b, "user_pref(%q, %s);\n", name, v)
	}
	// Start straight into the page: no welcome screens, prompts or restored sessions.
	pref("browser.aboutwelcome.enabled", false)
	pref("browser.startup.homepage_override.mstone", "ignore")
	pref("startup.homepage_welcome_url", "")
	pref("startup.homepage_welcome_url.additional", "")
	pref("browser.shell.checkDefaultBrowser", false)
	pref("datareporting.policy.dataSubmissionPolicyBypassNotification", true)
	pref("browser.sessionstore.resume_from_crash", false)
	pref("toolkit.startup.max_resumed_crashes", -1)
	pref("browser.tabs.warnOnClose", false)
	pref("browser.translations.automaticallyPopup", false)
	// Video: play without a click, allow DRM, no "is now fullscreen" banner.
	pref("media.autoplay.default", 0)
	pref("media.autoplay.blocking_policy", 0)
	pref("media.eme.enabled", true)
	pref("full-screen-api.warning.timeout", 0)
	// Empty means "use Firefox's own"; written explicitly so removing
	// user_agent from the config also removes the override.
	pref("general.useragent.override", app.UserAgent)
	if app.AdBlock {
		// Enable the add-on placed in this profile's extensions folder without
		// asking, and look for it on every start.
		pref("extensions.autoDisableScopes", 0)
		pref("extensions.startupScanScopes", 1)
		pref("extensions.update.enabled", true)
	}
	return b.String()
}

// ensureUBlock downloads uBlock Origin into the profile once. Firefox keeps it
// up to date by itself afterwards.
func ensureUBlock(profile string) error {
	dir := filepath.Join(profile, "extensions")
	dst := filepath.Join(dir, uBlockID+".xpi")
	if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(UBlockURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", resp.Status)
	}
	const maxSize = 32 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return err
	}
	// An .xpi is a zip archive; refuse error pages and truncated downloads.
	if len(data) > maxSize || !bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		return errors.New("download: not a valid add-on file")
	}
	tmp := dst + ".part"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// clearChromiumCrashFlag prevents the "restore pages?" bubble after the app
// was stopped from the dashboard or the Pi lost power.
func clearChromiumCrashFlag(profile string) {
	prefs := filepath.Join(profile, "Default", "Preferences")
	data, err := os.ReadFile(prefs)
	if err != nil {
		return
	}
	fixed := bytes.ReplaceAll(data, []byte(`"exit_type":"Crashed"`), []byte(`"exit_type":"Normal"`))
	fixed = bytes.ReplaceAll(fixed, []byte(`"exited_cleanly":false`), []byte(`"exited_cleanly":true`))
	if !bytes.Equal(fixed, data) {
		_ = os.WriteFile(prefs, fixed, 0o600)
	}
}

func findBinary(names ...string) (string, error) {
	var firstErr error
	for _, n := range names {
		p, err := exec.LookPath(n)
		if err == nil {
			return p, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return "", firstErr
}

func envValue(env []string, key string) string {
	val := ""
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			val = v
		}
	}
	return val
}

// DataDir is where PiDash keeps per-app browser profiles:
// $XDG_DATA_HOME/pidash, normally ~/.local/share/pidash.
func DataDir() (string, error) {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "pidash"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "pidash"), nil
}
