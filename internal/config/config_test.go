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

func TestNightBySunDimmingAndMap(t *testing.T) {
	cfg, err := load(t, "[display]\nnight_start = \"sunset+90\"\nnight_end = \"sunrise-30\"\nevening_dim = \"strong\"\n[photos]\nmap_tiles = \"https://tiles.example.org/{z}/{x}/{y}.png\"\n[games]\nretroarch = false\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Display.NightStart != "sunset+90" || cfg.Display.NightEnd != "sunrise-30" || cfg.Display.EveningDim != "strong" {
		t.Errorf("display = %+v", cfg.Display)
	}
	if !cfg.Photos.Map || !cfg.ForClient().PhotoMap || cfg.Games.RetroArch || !cfg.Games.Moonlight {
		t.Errorf("photos.map = %v, games = %+v", cfg.Photos.Map, cfg.Games)
	}
	// A clock time and a sun time mix; the map can be switched off, and then
	// its address does not matter.
	if _, err := load(t, "[display]\nnight_start = \"sunset\"\nnight_end = \"07:00\"\n[photos]\nmap = false\nmap_tiles = \"\"\n"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{
		"[display]\nnight_start = \"dusk\"\nnight_end = \"07:00\"\n",
		"[display]\nnight_start = \"sunset+1000\"\nnight_end = \"07:00\"\n",
		"[display]\nnight_start = \"sunset\"\n",
		"[display]\nevening_dim = \"dark\"\n",
		"[photos]\nmap_tiles = \"https://tiles.example.org/{z}/{x}.png\"\n",
	} {
		if _, err := load(t, bad); err == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
}

// A setting this version does not know does not make the file unusable: it
// is left aside and named.
func TestSettingsThisVersionDoesNotKnowAreNamedAndIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	text := "[display]\nbogus = 1\nclock_24h = false\n[update]\nauto = true\nsomething_newer = \"x\"\n[nowhere]\nkey = 2\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Unknown, " "); got != "display.bogus update.something_newer nowhere nowhere.key" && got != "display.bogus update.something_newer nowhere.key" {
		t.Errorf("unknown = %q", got)
	}
	// What it does know still counts.
	if cfg.Display.Clock24h || !cfg.Update.Auto {
		t.Errorf("the known settings were lost: %+v %+v", cfg.Display.Clock24h, cfg.Update.Auto)
	}
	if clean, _ := Load(""); len(clean.Unknown) != 0 {
		t.Errorf("the built-in settings have unknown keys: %v", clean.Unknown)
	}
}

// The ad blocker is an add-on for Firefox. Asked for in another browser it
// has nothing to act on, and is left aside rather than refused.
func TestAdBlockerOutsideFirefoxIsLeftAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	text := "[[apps]]\nid = \"a\"\nurl = \"https://example.com\"\nadblock = true\n[[apps]]\nid = \"b\"\nurl = \"https://example.org\"\nbrowser = \"firefox\"\nadblock = true\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Apps[0].AdBlock || cfg.Apps[0].Browser != BrowserChromium || !cfg.Apps[1].AdBlock {
		t.Errorf("apps = %+v", cfg.Apps)
	}
}

func TestOnlyTheDashboardLeavesOutWhatIsOfTheMachine(t *testing.T) {
	cfg, err := load(t, `
[weather]
location = "Zurich"

[photos]
source = "folder"
[photos.folder]
path = "/photos"

[[apps]]
id = "youtube"
url = "https://www.youtube.com/tv"

[[radio]]
name = "A station"
url = "https://example.org/stream"

[tv]
off = "23:00"
idle_off = "30m"
[[tv.inputs]]
name = "Console"
port = 2

[videos]
source = "folder"
folders = ["/films"]

[games]
moonlight = true

[update]
auto = true
`)
	if err != nil {
		t.Fatal(err)
	}
	cfg.OnlyTheDashboard()
	if len(cfg.Apps) != 0 || cfg.TV.Off != "" || cfg.TV.IdleOff.Duration != 0 || len(cfg.TV.Inputs) != 0 ||
		cfg.Videos.Source != VideoSourceNone || cfg.Games.Moonlight || cfg.Update.Auto || cfg.Kiosk.Unit != "" || cfg.Kiosk.Watchdog {
		t.Errorf("something of the machine is left: %+v", cfg)
	}
	// What a browser anywhere can show stays.
	if cfg.Weather.Location != "Zurich" || cfg.Photos.Folder.Path != "/photos" || len(cfg.Radio) != 1 {
		t.Errorf("the dashboard lost something: %+v", cfg)
	}
	client := cfg.ForClient()
	if client.Videos || len(client.TVInputs) != 0 {
		t.Errorf("the page is told of videos or other devices: %+v", client)
	}
}

// The settings file the installer wrote stays on the Pi through every
// update: what a version installed, every later version must accept. The
// files in testdata are config.example.toml as earlier versions shipped it;
// they are never edited, and each release adds its own.
func TestSettingsFilesInstalledByEarlierVersionsStillLoad(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "installed-by-*.toml"))
	if len(files) == 0 {
		t.Fatal("no installed settings files to try")
	}
	for _, file := range files {
		cfg, err := Load(file)
		if err != nil {
			t.Errorf("%s is refused by this version: %v", filepath.Base(file), err)
			continue
		}
		// A setting that was since taken away is named and ignored, never an
		// error; the apps that file set up are still there.
		if len(cfg.Apps) == 0 {
			t.Errorf("%s: its apps are gone", filepath.Base(file))
		}
	}
	// The file this version installs has nothing in it that it does not know.
	now, err := Load(filepath.Join("..", "..", "config.example.toml"))
	if err != nil || len(now.Unknown) != 0 {
		t.Errorf("config.example.toml: %v, unknown settings %v", err, now.Unknown)
	}
}
