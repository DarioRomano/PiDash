package config

import (
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
		"duplicate id":       "[[apps]]\nid = \"a\"\ncommand = \"x\"\n[[apps]]\nid = \"a\"\ncommand = \"y\"\n",
	}
	for name, body := range cases {
		if _, err := load(t, body); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestKeyMapInFileReplacesDefaults(t *testing.T) {
	cfg, err := load(t, "[input.keys]\nKeyA = \"select\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Input.Keys) != 1 || cfg.Input.Keys["KeyA"] != "select" {
		t.Errorf("keys = %v", cfg.Input.Keys)
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
