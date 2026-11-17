package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

// What a version wrote to disk, every later version must read. The files in
// testdata are settings as they were saved from the settings page by the
// version in their name, with everything set; they are never edited. A new
// version that changes how something is stored gets a new file, and the old
// ones stay, so that an update is tried against each of them.
func TestSettingsSavedByEarlierVersionsStillLoad(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "saved-by-*.json"))
	if len(files) == 0 {
		t.Fatal("no saved settings to try")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			base, err := config.Load("")
			if err != nil {
				t.Fatal(err)
			}
			s, err := Open(path, base)
			if err != nil {
				t.Fatalf("the file is not read: %v", err)
			}
			if s.Fresh() {
				t.Error("a home with saved settings is taken for a first start")
			}
			cfg, err := s.Effective()
			if err != nil {
				t.Fatalf("the saved settings no longer make sense to this version: %v", err)
			}

			// The weather, the photos and where they come from.
			if cfg.Weather.Location != "Zurich" || cfg.Weather.Name != "Home" || !cfg.Weather.Radar {
				t.Errorf("weather = %+v", cfg.Weather)
			}
			p := cfg.Photos
			if p.Source != config.PhotoSourceImmich || p.Immich.URL != "http://192.168.1.21:2283" || p.Immich.APIKey != "immich-key" || p.Immich.Quality != config.PhotoQualityPreview {
				t.Errorf("photos from Immich = %+v", p.Immich)
			}
			if p.Interval.Duration != 45*time.Second || p.Motion != "whole" || !p.PairPortraits || !p.OnThisDay || !p.Map || !p.SkipOdd || !p.Art {
				t.Errorf("how the photos are shown = %+v", p)
			}
			// The screen.
			d := cfg.Display
			if d.Language != "de" || d.NightStart != "23:00" || d.NightEnd != "06:30" || d.ClockPosition != "bottom-left" || d.View != "details" {
				t.Errorf("screen = %+v", d)
			}
			// The apps, in their order, each as it was set up.
			var ids []string
			for _, a := range cfg.Apps {
				ids = append(ids, a.ID)
			}
			if got := strings.Join(ids, " "); got != "youtube spotify jellyfin a-shop" {
				t.Fatalf("apps = %s", got)
			}
			if a := cfg.Apps[1]; a.Zoom != 1.5 || !a.Background || a.URL != "https://open.spotify.com/" {
				t.Errorf("spotify = %+v", a)
			}
			if a := cfg.Apps[2]; a.URL != "http://192.168.1.22:8096" {
				t.Errorf("jellyfin = %+v", a)
			}
			if a := cfg.Apps[3]; a.Name != "A shop" || !a.Arrows {
				t.Errorf("the website = %+v", a)
			}
			// Everything else that has a card on the settings page.
			if cfg.Departures.Stop != "Zürich, Bellevue" || cfg.Departures.Walk != 3 {
				t.Errorf("departures = %+v", cfg.Departures)
			}
			if !cfg.Sound.SpotifyConnect || cfg.Sound.AirPlay || cfg.Sound.SpeakerName != "Living room" || !cfg.Sound.FollowBluetooth {
				t.Errorf("sound = %+v", cfg.Sound)
			}
			if cfg.TV.IdleOff.Duration != 30*time.Minute || len(cfg.TV.Inputs) != 1 || cfg.TV.Inputs[0].Name != "Console" {
				t.Errorf("tv = %+v", cfg.TV)
			}
			if !cfg.Update.Auto || cfg.Kiosk.Resolution != "1080p" || !cfg.Games.Page || !cfg.Games.OpenOnController || !cfg.Bluetooth.DisconnectWithTV {
				t.Errorf("system: update %+v, kiosk %+v, games %+v", cfg.Update, cfg.Kiosk, cfg.Games)
			}
			if len(cfg.Radio) != 1 || cfg.Radio[0].Name != "A station" {
				t.Errorf("radio = %+v", cfg.Radio)
			}
			if cfg.Wifi.Name != "Guests" || cfg.Wifi.Password != "guest-secret" {
				t.Errorf("wifi = %+v", cfg.Wifi)
			}
			if cfg.Remote.PIN != "4711" {
				t.Errorf("the PIN is %q", cfg.Remote.PIN)
			}
			if got := strings.Join(cfg.Display.HiddenTiles, " "); got != "wifi desktop" {
				t.Errorf("hidden tiles = %q", got)
			}

			// And the page is shown them without the secrets: that there is
			// a key for the source in use, and a PIN, not what they are.
			v := s.View()
			if !v.Photos.Immich.KeySet || !v.Lock.Set || v.Lock.PIN != "" {
				t.Errorf("what the page is told of the secrets: %+v %+v", v.Photos.Immich, v.Lock)
			}
			// Saving something else keeps all of it.
			stop := Departures{Stop: "Bern, Bahnhof", Walk: 5}
			if err := s.Save(Update{Departures: &stop}); err != nil {
				t.Fatalf("saving on top of it: %v", err)
			}
			again, err := s.Effective()
			if err != nil || again.Departures.Stop != "Bern, Bahnhof" || again.Photos.Immich.APIKey != "immich-key" || len(again.Apps) != 4 || again.Display.Language != "de" {
				t.Errorf("after saving something else: %v, %+v", err, again.Departures)
			}
		})
	}
}
