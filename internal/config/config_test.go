package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, body string) (Config, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(p)
}

func TestDefaultsWithoutFile(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Listen != ":8080" || cfg.Input.Keys["Enter"] != "select" || len(cfg.Apps) != 0 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestAppsGetDefaults(t *testing.T) {
	cfg, err := load(t, `
[[apps]]
id = "moonlight"
command = "moonlight-qt"
mode = "console"

[[apps]]
id = "steam-link"
name = "Steam Link"
command = "steamlink"
`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Apps) != 2 {
		t.Fatalf("apps = %+v", cfg.Apps)
	}
	if a := cfg.Apps[0]; a.Name != "moonlight" || a.Mode != AppModeConsole {
		t.Errorf("app 0 = %+v", a)
	}
	if a := cfg.Apps[1]; a.Name != "Steam Link" || a.Mode != AppModeDesktop {
		t.Errorf("app 1 = %+v", a)
	}
}

func TestInvalidConfigsAreRejected(t *testing.T) {
	cases := map[string]string{
		"unknown key":        "[display]\nbogus = 1\n",
		"bad safe area":      "[display]\nsafe_area = 0.9\n",
		"bad app id":         "[[apps]]\nid = \"My App\"\ncommand = \"x\"\n",
		"unit injection":     "[[apps]]\nid = \"a.service;b\"\ncommand = \"x\"\n",
		"missing cmd":        "[[apps]]\nid = \"a\"\n",
		"bad mode":           "[[apps]]\nid = \"a\"\ncommand = \"x\"\nmode = \"tty\"\n",
		"cmd and url":        "[[apps]]\nid = \"a\"\ncommand = \"x\"\nurl = \"https://example.com\"\n",
		"url without host":   "[[apps]]\nid = \"a\"\nurl = \"youtube.com/tv\"\n",
		"file url":           "[[apps]]\nid = \"a\"\nurl = \"file:///etc/passwd\"\n",
		"web app on console": "[[apps]]\nid = \"a\"\nurl = \"https://example.com\"\nmode = \"console\"\n",
		"bad browser":        "[[apps]]\nid = \"a\"\nurl = \"https://example.com\"\nbrowser = \"safari\"\n",
		"adblock chromium":   "[[apps]]\nid = \"a\"\nurl = \"https://example.com\"\nadblock = true\n",
		"adblock on program": "[[apps]]\nid = \"a\"\ncommand = \"x\"\nadblock = true\n",
		"photo source":       "[photos]\nsource = \"flickr\"\n",
		"synology no login":  "[photos]\nsource = \"synology\"\n[photos.synology]\nurl = \"http://nas:5000\"\n",
		"synology bad url":   "[photos]\nsource = \"synology\"\n[photos.synology]\nurl = \"nas\"\nusername = \"a\"\npassword = \"b\"\n",
		"synology quality":   "[photos]\nsource = \"synology\"\n[photos.synology]\nurl = \"http://nas:5000\"\nusername = \"a\"\npassword = \"b\"\nquality = \"best\"\n",
		"relative folder":    "[photos]\nsource = \"folder\"\n[photos.folder]\npath = \"Pictures\"\n",
		"interval too short": "[photos]\ninterval = \"1s\"\n",
		"latitude only":      "[weather]\nlatitude = 47.3\n",
		"latitude range":     "[weather]\nlatitude = 147.3\nlongitude = 8.5\n",
		"weather units":      "[weather]\nlocation = \"Zurich\"\nunits = \"kelvin\"\n",
		"weather interval":   "[weather]\nlocation = \"Zurich\"\ninterval = \"10s\"\n",
		"tv off format":      "[tv]\noff = \"11pm\"\n",
		"tv on range":        "[tv]\non = \"24:00\"\n",
		"tv same times":      "[tv]\noff = \"23:00\"\non = \"23:00\"\n",
		"short pin":          "[remote]\npin = \"12\"\n",
		"hold too long":      "[input]\napp_exit_hold = \"1m\"\n",
		"duplicate id":       "[[apps]]\nid = \"a\"\ncommand = \"x\"\n[[apps]]\nid = \"a\"\ncommand = \"y\"\n",
	}
	for name, body := range cases {
		if _, err := load(t, body); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestKeyMapInFileIsAddedToTheBuiltInKeys(t *testing.T) {
	cfg, err := load(t, "[input.keys]\nKeyA = \"select\"\nColorF0Red = \"Exit\"\nKeyW = \"none\"\nF1 = \"\"\n")
	if err != nil {
		t.Fatal(err)
	}
	keys := cfg.Input.Keys
	if keys["KeyA"] != "select" {
		t.Errorf("a key from the file should override the built-in one: %q", keys["KeyA"])
	}
	if keys["ColorF0Red"] != "exit" {
		t.Errorf("a new key should be added, lower-cased: %q", keys["ColorF0Red"])
	}
	if keys["Enter"] != "select" || keys["ArrowUp"] != "up" || keys["KeyH"] != "help" {
		t.Errorf("built-in keys must stay: %v", keys)
	}
	if _, ok := keys["KeyW"]; ok {
		t.Error(`"none" should switch a built-in key off`)
	}
	if _, ok := keys["F1"]; ok {
		t.Error("an empty value should switch a built-in key off")
	}
}

func TestExampleConfigIsValid(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "config.example.toml"))
	if err != nil {
		t.Fatalf("config.example.toml: %v", err)
	}
	if !strings.HasSuffix(cfg.Kiosk.Unit, ".service") {
		t.Errorf("kiosk unit = %q", cfg.Kiosk.Unit)
	}
}

func TestWebAppDefaults(t *testing.T) {
	cfg, err := load(t, `
[[apps]]
id = "netflix"
url = "https://www.netflix.com/browse"

[[apps]]
id = "youtube"
url = "https://www.youtube.com/tv"
browser = "firefox"
adblock = true
user_agent = "TV/1.0"
`)
	if err != nil {
		t.Fatal(err)
	}
	if a := cfg.Apps[0]; a.Browser != BrowserChromium || a.Mode != AppModeDesktop || a.AdBlock {
		t.Errorf("netflix = %+v", a)
	}
	if a := cfg.Apps[1]; a.Browser != BrowserFirefox || !a.AdBlock || a.UserAgent != "TV/1.0" {
		t.Errorf("youtube = %+v", a)
	}
}

func TestPhotoSettings(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if p := cfg.Photos; p.Source != PhotoSourceNone || p.Interval.Duration.Seconds() != 30 || p.CacheMB != 2000 || p.Synology.Quality != PhotoQualityOriginal {
		t.Errorf("defaults = %+v", p)
	}

	cfg, err = load(t, `
[photos]
source = "synology"
interval = "45s"

[photos.synology]
url = "https://nas.example.com:5001/"
username = "pidash"
password = "s3cret"
insecure_tls = true
quality = "preview"
`)
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Photos.Synology
	if s.URL != "https://nas.example.com:5001" || !s.InsecureTLS || s.Quality != PhotoQualityPreview || cfg.Photos.Interval.Duration.Seconds() != 45 {
		t.Errorf("synology = %+v", cfg.Photos)
	}
	// The password must never be sent to the browser.
	if b, _ := json.Marshal(cfg.ForClient()); strings.Contains(string(b), "s3cret") {
		t.Error("client config leaks the NAS password")
	}

	if _, err := load(t, "[photos]\nsource = \"folder\"\n[photos.folder]\npath = \"/srv/photos\"\n"); err != nil {
		t.Errorf("folder source: %v", err)
	}
}

func TestWeatherSettings(t *testing.T) {
	cfg, _ := Load("")
	if cfg.Weather.Enabled() || cfg.Weather.Units != "metric" || cfg.Weather.Interval.Duration.Minutes() != 15 {
		t.Errorf("defaults = %+v", cfg.Weather)
	}
	cfg, err := load(t, "[weather]\nlocation = \" Zurich \"\n")
	if err != nil || !cfg.Weather.Enabled() || cfg.Weather.Location != "Zurich" {
		t.Errorf("by name: %+v, err %v", cfg.Weather, err)
	}
	cfg, err = load(t, "[weather]\nlatitude = 0.0\nlongitude = 0.0\nunits = \"imperial\"\nname = \"Null Island\"\n")
	if err != nil || !cfg.Weather.Enabled() || *cfg.Weather.Latitude != 0 {
		t.Errorf("by coordinates (0,0 is a valid place): %+v, err %v", cfg.Weather, err)
	}
}

func TestRemoteTVAndExitKeySettings(t *testing.T) {
	cfg, _ := Load("")
	if cfg.Remote.MouseKeyboard || cfg.Remote.PIN != "" || cfg.TV.Off != "" {
		t.Errorf("the phone mouse and the TV schedule must be off by default: %+v %+v", cfg.Remote, cfg.TV)
	}
	// The built-in exit keys live in the input package; the settings only add to them.
	if len(cfg.Input.AppExitKeys) != 0 || cfg.Input.AppExitHold.Duration.Seconds() != 2 {
		t.Errorf("exit key defaults = %+v", cfg.Input)
	}
	cfg, err := load(t, `
[remote]
mouse_keyboard = true
pin = "4321"

[tv]
off = "23:30"
on = "6:45"

[input]
app_exit_keys = ["KEY_RED"]
app_exit_hold = "0s"

[input.remote_keys]
KEY_GREEN = "weather"
`)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Remote.MouseKeyboard || cfg.TV.On != "6:45" || len(cfg.Input.AppExitKeys) != 1 || cfg.Input.AppExitHold.Duration != 0 {
		t.Errorf("cfg = %+v %+v %+v", cfg.Remote, cfg.TV, cfg.Input.AppExitKeys)
	}
	if cfg.Input.RemoteKeys["KEY_GREEN"] != "weather" {
		t.Errorf("remote keys = %v", cfg.Input.RemoteKeys)
	}
	// Setting exit keys must not wipe the default key map.
	if cfg.Input.Keys["Enter"] != "select" {
		t.Error("the dashboard key map was lost")
	}
	// The PIN is never sent to the browser.
	if b, _ := json.Marshal(cfg.ForClient()); strings.Contains(string(b), "4321") {
		t.Error("client config leaks the PIN")
	}
}
