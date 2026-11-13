// Package settings holds what is changed on the settings page.
//
// Those values are stored in settings.json next to config.toml and laid over
// it: built-in defaults, then config.toml, then settings.json. The page
// therefore never rewrites the file a person edits by hand, and removing
// settings.json returns to exactly what config.toml says.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/desktop"
)

// File is the content of settings.json. A section that is absent leaves
// config.toml in charge of it.
type File struct {
	// Port remembers the port PiDash moved to because the configured one was
	// taken, so that happens once instead of on every start.
	Port    *Port    `json:"port,omitempty"`
	Weather *Weather `json:"weather,omitempty"`
	Photos  *Photos  `json:"photos,omitempty"`
	Apps    *[]App   `json:"apps,omitempty"`
}

// Port records a port fallback. It applies only while config.toml still asks
// for the port that was taken.
type Port struct {
	Configured string `json:"configured"`
	Using      string `json:"using"`
}

// Weather settings.
type Weather struct {
	Location string `json:"location"`
	Name     string `json:"name"`
	Units    string `json:"units"`
}

// Photos settings.
type Photos struct {
	// Source is "", "synology" or "folder". The folder's path stays in config.toml.
	Source          string `json:"source"`
	IntervalSeconds int    `json:"intervalSeconds"`
	// Motion is "off", "whole" or "fill"; empty leaves config.toml in charge.
	Motion   string   `json:"motion,omitempty"`
	Synology Synology `json:"synology"`
}

// Synology is the connection to Synology Photos.
type Synology struct {
	URL         string `json:"url"`
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`
	InsecureTLS bool   `json:"insecureTls"`
	Quality     string `json:"quality"`
}

// App kinds.
const (
	KindPreset  = "preset"  // one of Presets
	KindWeb     = "web"     // a website given by address
	KindFile    = "file"    // a program defined in config.toml, kept as it is
	KindDesktop = "desktop" // a program installed on the Pi, named by its menu entry
)

// App is one entry of the app row as chosen on the settings page.
//
// Programs can only be added from the built-in presets and from what is
// installed on the Pi. The page names an installed program by its menu entry;
// the command is always read from that entry on the Pi, never taken from the
// page. An arbitrary command still has to be written into config.toml: the
// settings page is open to the home network, and it must not become a way to
// run commands on the Pi.
type App struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
	// Preset names the entry of Presets, for KindPreset.
	Preset string `json:"preset,omitempty"`
	// Desktop is the id of the installed program's menu entry, for KindDesktop.
	Desktop string `json:"desktop,omitempty"`
	// Missing is true for a KindDesktop entry whose program is not installed
	// any more. It is for display and not stored.
	Missing bool `json:"missing,omitempty"`
	// URL is the address for KindWeb, and for presets that need one.
	URL       string `json:"url,omitempty"`
	Browser   string `json:"browser,omitempty"`
	AdBlock   bool   `json:"adblock,omitempty"`
	UserAgent string `json:"userAgent,omitempty"`
	// Detail describes a KindFile entry for display; it is not stored.
	Detail string `json:"detail,omitempty"`
}

// Preset is a ready-made app.
type Preset struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// NeedsURL asks for an address, with URLHint as the example.
	NeedsURL bool   `json:"needsUrl"`
	URLHint  string `json:"urlHint,omitempty"`
	// Console is true for programs that run outside the desktop.
	Console bool       `json:"console"`
	App     config.App `json:"-"`
}

// youTubeTV makes YouTube serve its TV interface, which works with a remote.
const youTubeTV = "Mozilla/5.0 (PS4; Leanback Shell) Gecko/20100101 Firefox/65.0 LeanbackShell/01.00.01.75 Sony PS4/ (PS4, , no, CH)"

// Presets are the apps offered on the settings page.
var Presets = []Preset{
	{ID: "youtube", Name: "YouTube", Description: "The TV interface, in Firefox with uBlock Origin",
		App: config.App{URL: "https://www.youtube.com/tv", Browser: config.BrowserFirefox, AdBlock: true, UserAgent: youTubeTV}},
	{ID: "netflix", Name: "Netflix", Description: "In Chromium, which has the DRM module Netflix needs",
		App: config.App{URL: "https://www.netflix.com/browse", Browser: config.BrowserChromium}},
	{ID: "synology-photos", Name: "Synology Photos", Description: "Browse the photo app of your NAS",
		NeedsURL: true, URLHint: "http://192.168.1.20:5000/?launchApp=SYNO.Foto.AppInstance",
		App: config.App{Browser: config.BrowserChromium}},
	{ID: "plex", Name: "Plex", Description: "The Plex web player",
		App: config.App{URL: "https://app.plex.tv/desktop", Browser: config.BrowserChromium}},
	{ID: "moonlight", Name: "Moonlight", Description: "Game streaming from your PC. Needs Moonlight installed", Console: true,
		App: config.App{Command: "moonlight-qt", Mode: config.AppModeConsole}},
	{ID: "kodi", Name: "Kodi", Description: "Media centre. Needs Kodi installed", Console: true,
		App: config.App{Command: "kodi-standalone", Mode: config.AppModeConsole}},
	{ID: "steamlink", Name: "Steam Link", Description: "Game streaming from Steam. Needs Steam Link installed",
		App: config.App{Command: "steamlink", Mode: config.AppModeDesktop}},
}

func preset(id string) (Preset, bool) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// Path returns where settings.json lives for a given config.toml.
func Path(configPath string) string {
	if configPath != "" {
		return filepath.Join(filepath.Dir(configPath), "settings.json")
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "pidash", "settings.json")
	}
	return ""
}

// Store keeps settings.json and the config.toml values it is laid over.
type Store struct {
	path string
	base config.Config
	// installed lists the programs installed on the Pi.
	installed func() []desktop.Entry

	mu   sync.Mutex
	file File
}

// Open reads settings.json. A missing file is an empty one. An unreadable
// file is reported and ignored, so a damaged file never keeps PiDash down.
func Open(path string, base config.Config) (*Store, error) {
	s := &Store{path: path, base: base, installed: func() []desktop.Entry { return desktop.List(desktop.DataDirs()) }}
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s.file); err != nil {
		s.file = File{}
		return s, fmt.Errorf("%s is damaged and was ignored: %w", path, err)
	}
	return s, nil
}

// Effective returns config.toml with the saved settings laid over it.
func (s *Store) Effective() (config.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return apply(s.base, s.file, s.installed())
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(name string) string {
	id := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(id) > 28 {
		id = strings.Trim(id[:28], "-")
	}
	if id == "" {
		id = "app"
	}
	return id
}

// errNotInstalled marks an app whose program was removed from the Pi.
var errNotInstalled = errors.New("it is not installed any more")

// apply lays f over base and validates the result. installed is what the Pi
// has installed, for the apps chosen from that list.
func apply(base config.Config, f File, installed []desktop.Entry) (config.Config, error) {
	cfg := base
	cfg.Apps = append([]config.App{}, base.Apps...)

	if f.Port != nil && f.Port.Configured == base.Server.Listen && f.Port.Using != "" {
		cfg.Server.Listen = f.Port.Using
	}

	if w := f.Weather; w != nil {
		cfg.Weather.Location = strings.TrimSpace(w.Location)
		cfg.Weather.Name = strings.TrimSpace(w.Name)
		if w.Units != "" {
			cfg.Weather.Units = w.Units
		}
		// A place chosen on the settings page replaces coordinates from the file.
		cfg.Weather.Latitude, cfg.Weather.Longitude = nil, nil
	}

	if p := f.Photos; p != nil {
		cfg.Photos.Source = p.Source
		if p.IntervalSeconds > 0 {
			cfg.Photos.Interval = config.Duration{Duration: time.Duration(p.IntervalSeconds) * time.Second}
		}
		if p.Motion != "" {
			cfg.Photos.Motion = p.Motion
		}
		if p.Source == config.PhotoSourceSynology {
			cfg.Photos.Synology.URL = p.Synology.URL
			cfg.Photos.Synology.Username = p.Synology.Username
			cfg.Photos.Synology.Password = p.Synology.Password
			cfg.Photos.Synology.InsecureTLS = p.Synology.InsecureTLS
			if p.Synology.Quality != "" {
				cfg.Photos.Synology.Quality = p.Synology.Quality
			}
		}
	}

	if f.Apps != nil {
		cfg.Apps = cfg.Apps[:0]
		used := map[string]bool{}
		for i, a := range *f.Apps {
			app, err := resolve(a, base, installed)
			if errors.Is(err, errNotInstalled) {
				// Uninstalling a program must not undo every other setting: its
				// tile just disappears, and comes back if it is installed again.
				continue
			}
			if err != nil {
				return cfg, fmt.Errorf("app %d (%s): %w", i+1, a.Name, err)
			}
			// Keep ids unique, whatever was typed as names.
			id := app.ID
			for n := 2; used[id]; n++ {
				id = fmt.Sprintf("%s-%d", app.ID, n)
			}
			app.ID = id
			used[id] = true
			cfg.Apps = append(cfg.Apps, app)
		}
	}

	if err := cfg.Finish(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// resolve turns a settings-page entry into a launcher app.
func resolve(a App, base config.Config, installed []desktop.Entry) (config.App, error) {
	switch a.Kind {
	case KindDesktop:
		for _, e := range installed {
			if e.ID != a.Desktop {
				continue
			}
			app := config.App{ID: slug(e.ID), Name: e.Name, Command: e.Command(), Icon: e.Icon, Mode: config.AppModeDesktop}
			if strings.TrimSpace(a.Name) != "" {
				app.Name = strings.TrimSpace(a.Name)
			}
			if app.Command == "" {
				return config.App{}, errNotInstalled
			}
			return app, nil
		}
		return config.App{}, errNotInstalled
	case KindFile:
		for _, b := range base.Apps {
			if b.ID == a.ID {
				return b, nil
			}
		}
		return config.App{}, errors.New("it is no longer defined in config.toml; remove it from the list")
	case KindPreset:
		p, ok := preset(a.Preset)
		if !ok {
			return config.App{}, fmt.Errorf("unknown preset %q", a.Preset)
		}
		app := p.App
		app.ID = p.ID
		app.Name = p.Name
		if strings.TrimSpace(a.Name) != "" {
			app.Name = strings.TrimSpace(a.Name)
		}
		if p.NeedsURL {
			app.URL = strings.TrimSpace(a.URL)
			if app.URL == "" {
				return config.App{}, errors.New("an address is needed")
			}
		}
		return app, nil
	case KindWeb:
		name := strings.TrimSpace(a.Name)
		if name == "" {
			return config.App{}, errors.New("a name is needed")
		}
		app := config.App{ID: slug(name), Name: name, URL: strings.TrimSpace(a.URL), Browser: a.Browser,
			AdBlock: a.AdBlock, UserAgent: strings.TrimSpace(a.UserAgent)}
		if app.AdBlock {
			app.Browser = config.BrowserFirefox // the ad blocker only exists for Firefox
		}
		return app, nil
	}
	return config.App{}, fmt.Errorf("unknown kind %q", a.Kind)
}

// describe turns an app from config.toml into its settings-page entry.
func describe(app config.App) App {
	for _, p := range Presets {
		same := app.ID == p.ID && app.Command == p.App.Command && (p.NeedsURL || app.URL == p.App.URL)
		if same && (p.App.Command != "" || app.URL != "") {
			out := App{Kind: KindPreset, ID: app.ID, Name: app.Name, Preset: p.ID}
			if p.NeedsURL {
				out.URL = app.URL
			}
			return out
		}
	}
	if app.URL != "" {
		return App{Kind: KindWeb, ID: app.ID, Name: app.Name, URL: app.URL, Browser: app.Browser, AdBlock: app.AdBlock, UserAgent: app.UserAgent}
	}
	return App{Kind: KindFile, ID: app.ID, Name: app.Name, Detail: app.Command}
}

// View is what the settings page shows. It never contains a password.
type View struct {
	Weather WeatherView `json:"weather"`
	Photos  PhotosView  `json:"photos"`
	Apps    []App       `json:"apps"`
	Presets []Preset    `json:"presets"`
	// Installed are the programs on the Pi that can be added to the app row.
	Installed []desktop.Entry `json:"installed"`
}

// WeatherView is the weather section.
type WeatherView struct {
	Location string `json:"location"`
	Name     string `json:"name"`
	Units    string `json:"units"`
	// Coordinates is true when config.toml pins the place by latitude and longitude.
	Coordinates bool `json:"coordinates"`
}

// PhotosView is the background photos section.
type PhotosView struct {
	Source          string       `json:"source"`
	IntervalSeconds int          `json:"intervalSeconds"`
	Motion          string       `json:"motion"`
	Synology        SynologyView `json:"synology"`
	// FolderPath is the folder configured in config.toml, if any.
	FolderPath string `json:"folderPath"`
}

// SynologyView is the NAS connection without its password.
type SynologyView struct {
	URL         string `json:"url"`
	Username    string `json:"username"`
	PasswordSet bool   `json:"passwordSet"`
	InsecureTLS bool   `json:"insecureTls"`
	Quality     string `json:"quality"`
}

// View returns the current settings for the page.
func (s *Store) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	installed := s.installed()
	if installed == nil {
		installed = []desktop.Entry{}
	}
	isInstalled := map[string]bool{}
	for _, e := range installed {
		isInstalled[e.ID] = true
	}
	cfg, err := apply(s.base, s.file, installed)
	if err != nil {
		cfg = s.base
	}
	v := View{
		Installed: installed,
		Weather: WeatherView{Location: cfg.Weather.Location, Name: cfg.Weather.Name, Units: cfg.Weather.Units,
			Coordinates: cfg.Weather.Latitude != nil},
		Photos: PhotosView{
			Source: cfg.Photos.Source, IntervalSeconds: int(cfg.Photos.Interval.Duration / time.Second),
			Motion: cfg.Photos.Motion,
			Synology: SynologyView{URL: cfg.Photos.Synology.URL, Username: cfg.Photos.Synology.Username,
				PasswordSet: cfg.Photos.Synology.Password != "", InsecureTLS: cfg.Photos.Synology.InsecureTLS,
				Quality: cfg.Photos.Synology.Quality},
		},
		Apps:    []App{},
		Presets: Presets,
	}
	if strings.HasPrefix(s.base.Photos.Folder.Path, "/") && s.base.Photos.Source == config.PhotoSourceFolder {
		v.Photos.FolderPath = s.base.Photos.Folder.Path
	}
	if s.file.Apps != nil {
		for _, a := range *s.file.Apps {
			if a.Kind == KindFile {
				for _, b := range s.base.Apps {
					if b.ID == a.ID {
						a.Name, a.Detail = b.Name, b.Command
					}
				}
			}
			a.Missing = a.Kind == KindDesktop && !isInstalled[a.Desktop]
			v.Apps = append(v.Apps, a)
		}
	} else {
		for _, a := range s.base.Apps {
			v.Apps = append(v.Apps, describe(a))
		}
	}
	return v
}

// Update is what the settings page sends. Absent sections stay as they are.
type Update struct {
	Weather *Weather `json:"weather"`
	Photos  *Photos  `json:"photos"`
	Apps    *[]App   `json:"apps"`
}

// SynologyPassword returns the stored NAS password, for "test connection"
// when the page leaves the password field empty to mean "unchanged".
func (s *Store) SynologyPassword() string {
	cfg, err := s.Effective()
	if err != nil {
		return ""
	}
	return cfg.Photos.Synology.Password
}

// Save validates an update against config.toml and writes settings.json.
// Nothing is written when the result would not be a valid configuration.
func (s *Store) Save(u Update) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.file
	if u.Weather != nil {
		w := *u.Weather
		next.Weather = &w
	}
	if u.Photos != nil {
		p := *u.Photos
		// An empty password means "keep the one already stored".
		if p.Source == config.PhotoSourceSynology && p.Synology.Password == "" {
			if cur, err := apply(s.base, s.file, s.installed()); err == nil {
				p.Synology.Password = cur.Photos.Synology.Password
			} else {
				p.Synology.Password = s.base.Photos.Synology.Password
			}
		}
		next.Photos = &p
	}
	if u.Apps != nil {
		apps := make([]App, 0, len(*u.Apps))
		for _, a := range *u.Apps {
			a.Detail, a.Missing = "", false
			apps = append(apps, a)
		}
		next.Apps = &apps
	}
	if _, err := apply(s.base, next, s.installed()); err != nil {
		return err
	}
	if err := s.write(next); err != nil {
		return err
	}
	s.file = next
	return nil
}

// RememberPort records that PiDash moved from the configured port to another.
func (s *Store) RememberPort(configured, using string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.file
	next.Port = &Port{Configured: configured, Using: using}
	if err := s.write(next); err != nil {
		return err
	}
	s.file = next
	return nil
}

func (s *Store) write(f File) error {
	if s.path == "" {
		return errors.New("there is no place to store settings")
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	// The file can hold the NAS password: readable by the owner only.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
