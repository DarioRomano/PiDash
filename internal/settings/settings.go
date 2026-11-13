// Package settings holds what is changed on the settings page.
//
// Those values are stored in settings.json next to config.toml and laid over
// it: built-in defaults, then config.toml, then settings.json. The page
// therefore never rewrites the file a person edits by hand, and removing
// settings.json returns to exactly what config.toml says.
package settings

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
	Screen  *Screen  `json:"screen,omitempty"`
	Apps    *[]App   `json:"apps,omitempty"`
	// Calendar, Home and System follow the same rule: absent leaves config.toml in charge.
	Calendar *Calendar `json:"calendar,omitempty"`
	Home     *Home     `json:"home,omitempty"`
	System   *System   `json:"system,omitempty"`
	// Lock holds the PIN that must be typed before settings can be changed.
	Lock *Lock `json:"lock,omitempty"`
	// Radio are the stations in the app row.
	Radio *[]config.RadioStation `json:"radio,omitempty"`
}

// Lock is the settings lock. The PIN is write-only: the page learns only
// whether one is set.
type Lock struct {
	PIN string `json:"pin,omitempty"`
	// Set is for the page: a PIN exists. Remove, from the page, deletes it.
	Set    bool `json:"set,omitempty"`
	Remove bool `json:"remove,omitempty"`
}

// Calendar settings: the calendar links.
type Calendar struct {
	Links []Link `json:"links"`
}

// Link is one calendar link. Such a link is a secret: whoever has it can read
// the calendar. So the page is only told where it leads (Host), and names a
// link it wants to keep by its Key; URL is filled in only for a new one.
type Link struct {
	Key  string `json:"key,omitempty"`
	URL  string `json:"url,omitempty"`
	Host string `json:"host,omitempty"`
}

// Home is the connection to Home Assistant.
type Home struct {
	URL string `json:"url"`
	// Token is write-only, like the NAS password: empty keeps the stored one.
	Token    string   `json:"token,omitempty"`
	TokenSet bool     `json:"tokenSet,omitempty"`
	Entities []string `json:"entities"`
}

// System settings.
type System struct {
	AutoUpdate bool `json:"autoUpdate"`
	// Cooling stops the moving background and the movement of photos while
	// the Pi is hot. Absent (settings saved by an older version) leaves
	// config.toml in charge.
	Cooling *bool `json:"cooling,omitempty"`
}

// defaultCoolAbove is the limit used when cooling is switched on from the
// settings page and config.toml has it switched off.
const defaultCoolAbove = 75

func linkKey(u string) string {
	sum := sha1.Sum([]byte(u))
	return hex.EncodeToString(sum[:6])
}

func linkHost(u string) string {
	if parsed, err := url.Parse(u); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return "calendar"
}

// Screen settings.
type Screen struct {
	// NightStart and NightEnd ("HH:MM") bound the night mode; both empty is off.
	NightStart string `json:"nightStart"`
	NightEnd   string `json:"nightEnd"`
	// Language, ClockPosition, ClockSize and View: empty leaves config.toml in charge.
	Language      string `json:"language,omitempty"`
	ClockPosition string `json:"clockPosition,omitempty"`
	ClockSize     string `json:"clockSize,omitempty"`
	View          string `json:"view,omitempty"`
	Background    string `json:"background,omitempty"`
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
	Motion string `json:"motion,omitempty"`
	// PairPortraits shows two upright photos side by side; absent leaves
	// config.toml in charge.
	PairPortraits *bool `json:"pairPortraits,omitempty"`
	// OnThisDay prefers photos taken on today's date in earlier years.
	OnThisDay *bool `json:"onThisDay,omitempty"`
	// Videos also shows short video clips.
	Videos   *bool    `json:"videos,omitempty"`
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
	URL     string `json:"url,omitempty"`
	Browser string `json:"browser,omitempty"`
	// Zoom enlarges a website; 0 leaves it at its normal size.
	Zoom      float64 `json:"zoom,omitempty"`
	AdBlock   bool    `json:"adblock,omitempty"`
	UserAgent string  `json:"userAgent,omitempty"`
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
	Console bool `json:"console"`
	// Web is true for websites, which can be enlarged.
	Web bool       `json:"web"`
	App config.App `json:"-"`
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
	{ID: "spotify", Name: "Spotify", Description: "The Spotify web player. Works best with a mouse; the remote's OK and play button work on what is selected",
		App: config.App{URL: "https://open.spotify.com/", Browser: config.BrowserChromium}},
	{ID: "plex", Name: "Plex", Description: "The Plex web player",
		App: config.App{URL: "https://app.plex.tv/desktop", Browser: config.BrowserChromium}},
	{ID: "moonlight", Name: "Moonlight", Description: "Game streaming from your PC. Needs Moonlight installed", Console: true,
		App: config.App{Command: "moonlight-qt", Mode: config.AppModeConsole}},
	{ID: "kodi", Name: "Kodi", Description: "Media centre. Needs Kodi installed", Console: true,
		App: config.App{Command: "kodi-standalone", Mode: config.AppModeConsole}},
	{ID: "steamlink", Name: "Steam Link", Description: "Game streaming from Steam. Needs Steam Link installed",
		App: config.App{Command: "steamlink", Mode: config.AppModeDesktop}},
}

// Emulator is a game emulator that runs well on a Raspberry Pi 5 and is
// worth offering by name. It is installed like any other program; PiDash
// finds it through its entry in the Pi's application menu, and adds what it
// takes to start it filling the screen.
type Emulator struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Systems says in a few words what it plays.
	Systems     string `json:"systems"`
	Description string `json:"description"`
	// Install is the command that installs it on Raspberry Pi OS.
	Install string `json:"install"`
	// Desktop is the id of its menu entry when it is installed, else empty.
	Desktop string `json:"desktop,omitempty"`
	// Entries are the names its menu entry goes by, depending on where it
	// was installed from; Args make it start fullscreen.
	Entries []string `json:"-"`
	Args    string   `json:"-"`
}

// Emulators are the emulators offered on the settings page.
var Emulators = []Emulator{
	{ID: "retroarch", Name: "RetroArch", Systems: "classic consoles",
		Description: "One program for many classic consoles, from the NES and Mega Drive to the first PlayStation. Made for a game controller.",
		Install:     "sudo apt install retroarch",
		Entries:     []string{"retroarch", "com.libretro.RetroArch", "org.libretro.RetroArch"}, Args: "-f"},
	{ID: "ppsspp", Name: "PPSSPP", Systems: "PlayStation Portable",
		Description: "PlayStation Portable games, which a Pi 5 plays at full speed. Made for a game controller.",
		Install:     "flatpak install flathub org.ppsspp.PPSSPP",
		Entries:     []string{"org.ppsspp.PPSSPP", "PPSSPPSDL", "ppsspp", "PPSSPPQt"}, Args: "--fullscreen"},
	{ID: "mupen64plus", Name: "Mupen64Plus", Systems: "Nintendo 64",
		Description: "Nintendo 64 games. Choosing a game needs a mouse or the phone's touchpad; playing it, a game controller.",
		Install:     "sudo apt install mupen64plus-qt",
		Entries:     []string{"mupen64plus-qt"}},
	{ID: "dolphin", Name: "Dolphin", Systems: "GameCube and Wii",
		Description: "GameCube and Wii games. Only the lighter ones run at full speed on a Pi 5. Made for a game controller.",
		Install:     "flatpak install flathub org.DolphinEmu.dolphin-emu",
		Entries:     []string{"dolphin-emu", "org.DolphinEmu.dolphin-emu"}},
	{ID: "scummvm", Name: "ScummVM", Systems: "adventure games",
		Description: "Classic point-and-click adventures. They are played with a mouse: the phone remote has a touchpad.",
		Install:     "sudo apt install scummvm",
		Entries:     []string{"org.scummvm.scummvm", "scummvm"}, Args: "-f"},
	{ID: "dosbox", Name: "DOSBox", Systems: "DOS games",
		Description: "Games and programs written for DOS. It starts at a command line, so it needs a keyboard.",
		Install:     "sudo apt install dosbox",
		Entries:     []string{"dosbox", "com.dosbox_x.DOSBox-X", "dosbox-x", "dosbox-staging"}, Args: "-fullscreen"},
	{ID: "mame", Name: "MAME", Systems: "arcade machines",
		Description: "Arcade machines. Made for a game controller or a keyboard.",
		Install:     "sudo apt install mame",
		Entries:     []string{"mame", "org.mamedev.MAME"}},
}

// emulatorOf returns the emulator a menu entry belongs to.
func emulatorOf(entryID string) (Emulator, bool) {
	for _, em := range Emulators {
		for _, name := range em.Entries {
			if strings.EqualFold(name, entryID) {
				return em, true
			}
		}
	}
	return Emulator{}, false
}

// emulatorsForPage says which emulators are installed, and as what.
func emulatorsForPage(installed []desktop.Entry) []Emulator {
	out := make([]Emulator, len(Emulators))
	for i, em := range Emulators {
		// In the order of Entries: the system's own copy before a Flatpak one.
	find:
		for _, name := range em.Entries {
			for _, e := range installed {
				if strings.EqualFold(e.ID, name) && e.Command() != "" {
					em.Desktop = e.ID
					break find
				}
			}
		}
		out[i] = em
	}
	return out
}

// withArgs adds arguments to a command unless it has them already.
func withArgs(command, args string) string {
	have := strings.Fields(command)
	for _, arg := range strings.Fields(args) {
		if !slices.Contains(have, arg) {
			have = append(have, arg)
		}
	}
	return strings.Join(have, " ")
}

// presetsForPage marks which presets are websites.
func presetsForPage() []Preset {
	out := make([]Preset, len(Presets))
	for i, p := range Presets {
		p.Web = p.App.URL != "" || p.NeedsURL
		out[i] = p
	}
	return out
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
		if p.PairPortraits != nil {
			cfg.Photos.PairPortraits = *p.PairPortraits
		}
		if p.OnThisDay != nil {
			cfg.Photos.OnThisDay = *p.OnThisDay
		}
		if p.Videos != nil {
			cfg.Photos.Videos = *p.Videos
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

	if c := f.Calendar; c != nil {
		cfg.Calendar.URLs = nil
		for _, l := range c.Links {
			if l.URL != "" {
				cfg.Calendar.URLs = append(cfg.Calendar.URLs, l.URL)
			}
		}
	}
	if h := f.Home; h != nil {
		cfg.Home.URL, cfg.Home.Token, cfg.Home.Entities = h.URL, h.Token, append([]string{}, h.Entities...)
	}
	if sys := f.System; sys != nil {
		cfg.Update.Auto = sys.AutoUpdate
		if sys.Cooling != nil {
			switch {
			case !*sys.Cooling:
				cfg.System.CoolAbove = 0
			case cfg.System.CoolAbove == 0:
				cfg.System.CoolAbove = defaultCoolAbove
			}
		}
	}
	if l := f.Lock; l != nil {
		cfg.Remote.PIN = l.PIN
	}
	if f.Radio != nil {
		cfg.Radio = append([]config.RadioStation{}, *f.Radio...)
	}
	if sc := f.Screen; sc != nil {
		cfg.Display.NightStart, cfg.Display.NightEnd = strings.TrimSpace(sc.NightStart), strings.TrimSpace(sc.NightEnd)
		for _, choice := range []struct {
			from string
			to   *string
		}{
			{sc.Language, &cfg.Display.Language}, {sc.ClockPosition, &cfg.Display.ClockPosition},
			{sc.ClockSize, &cfg.Display.ClockSize}, {sc.View, &cfg.Display.View}, {sc.Background, &cfg.Display.Background},
		} {
			if choice.from != "" {
				*choice.to = choice.from
			}
		}
	}

	if f.Apps != nil {
		apps, _, err := resolveAll(*f.Apps, base, installed)
		if err != nil {
			return cfg, err
		}
		cfg.Apps = apps
	}

	if err := cfg.Finish(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// resolveAll turns the entries of the settings page into launcher apps.
// from[i] is the position in entries that apps[i] came from: the two lists
// differ in length when a program is not installed any more.
func resolveAll(entries []App, base config.Config, installed []desktop.Entry) (apps []config.App, from []int, err error) {
	apps = []config.App{}
	used := map[string]bool{}
	for i, a := range entries {
		app, err := resolve(a, base, installed)
		if errors.Is(err, errNotInstalled) {
			// Uninstalling a program must not undo every other setting: its
			// tile just disappears, and comes back if it is installed again.
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("app %d (%s): %w", i+1, a.Name, err)
		}
		// Keep ids unique, whatever was typed as names.
		id := app.ID
		for n := 2; used[id]; n++ {
			id = fmt.Sprintf("%s-%d", app.ID, n)
		}
		app.ID = id
		used[id] = true
		apps = append(apps, app)
		from = append(from, i)
	}
	return apps, from, nil
}

// ErrOrder is returned by ReorderApps for an order that does not name
// exactly the apps of the app row.
var ErrOrder = errors.New("the order does not match the apps in the app row")

// ReorderApps puts the apps of the app row in the order of ids, the ids the
// launcher knows them by, and saves that. It is what moving a tile with the
// TV remote does; nothing else about the apps changes.
func (s *Store) ReorderApps(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The entries as the settings page has them. Before the first save they
	// are still the ones from config.toml.
	var entries []App
	if s.file.Apps != nil {
		entries = append(entries, *s.file.Apps...)
	} else {
		for _, a := range s.base.Apps {
			e := describe(a)
			e.Detail = ""
			entries = append(entries, e)
		}
	}
	installed := s.installed()
	apps, from, err := resolveAll(entries, s.base, installed)
	if err != nil {
		return err
	}
	if len(ids) != len(apps) {
		return ErrOrder
	}
	entryOf := map[string]int{}
	for i, a := range apps {
		entryOf[a.ID] = from[i]
	}
	next := make([]App, 0, len(entries))
	taken := map[int]bool{}
	for _, id := range ids {
		i, ok := entryOf[id]
		if !ok || taken[i] {
			return ErrOrder
		}
		taken[i] = true
		next = append(next, entries[i])
	}
	// Programs that are not installed right now keep their place at the end.
	for i, e := range entries {
		if !taken[i] {
			next = append(next, e)
		}
	}
	// Two apps with the same name are told apart by a number that follows
	// their order. Swapping those would swap what they are known as, and with
	// it their browser profiles: such a move is refused.
	after, _, err := resolveAll(next, s.base, installed)
	if err != nil {
		return err
	}
	for i, a := range after {
		if a.ID != ids[i] {
			return fmt.Errorf("%s and an app of the same name cannot change places", a.Name)
		}
	}
	f := s.file
	f.Apps = &next
	if _, err := apply(s.base, f, installed); err != nil {
		return err
	}
	if err := s.write(f); err != nil {
		return err
	}
	s.file = f
	return nil
}

// resolve turns a settings-page entry into a launcher app.
func resolve(a App, base config.Config, installed []desktop.Entry) (config.App, error) {
	switch a.Kind {
	case KindDesktop:
		for _, e := range installed {
			if e.ID != a.Desktop {
				continue
			}
			app := config.App{ID: slug(e.ID), Name: e.Name, Command: e.Command(), Icon: e.Icon, Mode: config.AppModeDesktop, Window: e.Window}
			if strings.TrimSpace(a.Name) != "" {
				app.Name = strings.TrimSpace(a.Name)
			}
			// A known emulator gets its proper name and starts fullscreen.
			if em, ok := emulatorOf(e.ID); ok {
				if strings.TrimSpace(a.Name) == "" {
					app.Name = em.Name
				}
				if app.Command != "" {
					app.Command = withArgs(app.Command, em.Args)
				}
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
		if app.URL != "" {
			app.Zoom = a.Zoom
		}
		return app, nil
	case KindWeb:
		name := strings.TrimSpace(a.Name)
		if name == "" {
			return config.App{}, errors.New("a name is needed")
		}
		app := config.App{ID: slug(name), Name: name, URL: strings.TrimSpace(a.URL), Browser: a.Browser,
			AdBlock: a.AdBlock, UserAgent: strings.TrimSpace(a.UserAgent), Zoom: a.Zoom}
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
			out := App{Kind: KindPreset, ID: app.ID, Name: app.Name, Preset: p.ID, Zoom: app.Zoom}
			if p.NeedsURL {
				out.URL = app.URL
			}
			return out
		}
	}
	if app.URL != "" {
		return App{Kind: KindWeb, ID: app.ID, Name: app.Name, URL: app.URL, Browser: app.Browser, AdBlock: app.AdBlock, UserAgent: app.UserAgent, Zoom: app.Zoom}
	}
	return App{Kind: KindFile, ID: app.ID, Name: app.Name, Detail: app.Command}
}

// View is what the settings page shows. It never contains a password.
type View struct {
	Weather  WeatherView           `json:"weather"`
	Photos   PhotosView            `json:"photos"`
	Screen   Screen                `json:"screen"`
	Calendar Calendar              `json:"calendar"`
	Home     Home                  `json:"home"`
	System   System                `json:"system"`
	Lock     Lock                  `json:"lock"`
	Radio    []config.RadioStation `json:"radio"`
	Apps     []App                 `json:"apps"`
	Presets  []Preset              `json:"presets"`
	// Installed are the programs on the Pi that can be added to the app row.
	Installed []desktop.Entry `json:"installed"`
	// Emulators are the game emulators worth knowing about, installed or not.
	Emulators []Emulator `json:"emulators"`
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
	PairPortraits   bool         `json:"pairPortraits"`
	OnThisDay       bool         `json:"onThisDay"`
	Videos          bool         `json:"videos"`
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
		Emulators: emulatorsForPage(installed),
		Weather: WeatherView{Location: cfg.Weather.Location, Name: cfg.Weather.Name, Units: cfg.Weather.Units,
			Coordinates: cfg.Weather.Latitude != nil},
		Photos: PhotosView{
			Source: cfg.Photos.Source, IntervalSeconds: int(cfg.Photos.Interval.Duration / time.Second),
			Motion: cfg.Photos.Motion, PairPortraits: cfg.Photos.PairPortraits, OnThisDay: cfg.Photos.OnThisDay, Videos: cfg.Photos.Videos,
			Synology: SynologyView{URL: cfg.Photos.Synology.URL, Username: cfg.Photos.Synology.Username,
				PasswordSet: cfg.Photos.Synology.Password != "", InsecureTLS: cfg.Photos.Synology.InsecureTLS,
				Quality: cfg.Photos.Synology.Quality},
		},
		Screen: Screen{NightStart: cfg.Display.NightStart, NightEnd: cfg.Display.NightEnd, Language: cfg.Display.Language,
			ClockPosition: cfg.Display.ClockPosition, ClockSize: cfg.Display.ClockSize, View: cfg.Display.View, Background: cfg.Display.Background},
		Calendar: Calendar{Links: []Link{}},
		Home:     Home{URL: cfg.Home.URL, TokenSet: cfg.Home.Token != "", Entities: append([]string{}, cfg.Home.Entities...)},
		System:   System{AutoUpdate: cfg.Update.Auto, Cooling: boolPtr(cfg.System.CoolAbove > 0)},
		Lock:     Lock{Set: cfg.Remote.PIN != ""},
		Radio:    append([]config.RadioStation{}, cfg.Radio...),
		Apps:     []App{},
		Presets:  presetsForPage(),
	}
	for _, u := range cfg.Calendar.URLs {
		v.Calendar.Links = append(v.Calendar.Links, Link{Key: linkKey(u), Host: linkHost(u)})
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
	Weather  *Weather               `json:"weather"`
	Photos   *Photos                `json:"photos"`
	Screen   *Screen                `json:"screen"`
	Apps     *[]App                 `json:"apps"`
	Calendar *Calendar              `json:"calendar"`
	Home     *Home                  `json:"home"`
	System   *System                `json:"system"`
	Lock     *Lock                  `json:"lock"`
	Radio    *[]config.RadioStation `json:"radio"`
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
	if u.Screen != nil {
		sc := *u.Screen
		next.Screen = &sc
	}
	if u.Calendar != nil || u.Home != nil {
		cur, err := apply(s.base, s.file, s.installed())
		if err != nil {
			cur = s.base
		}
		if u.Calendar != nil {
			// A link the page kept is named by its key; look its address up.
			known := map[string]string{}
			for _, link := range cur.Calendar.URLs {
				known[linkKey(link)] = link
			}
			cal := Calendar{Links: []Link{}}
			for _, l := range u.Calendar.Links {
				link := strings.TrimSpace(l.URL)
				if link == "" {
					link = known[l.Key]
				}
				if link != "" {
					cal.Links = append(cal.Links, Link{URL: link})
				}
			}
			next.Calendar = &cal
		}
		if u.Home != nil {
			h := Home{URL: strings.TrimSpace(u.Home.URL), Token: strings.TrimSpace(u.Home.Token), Entities: u.Home.Entities}
			if h.Token == "" {
				h.Token = cur.Home.Token // unchanged
			}
			if h.URL == "" {
				h.Token, h.Entities = "", nil
			}
			next.Home = &h
		}
	}
	if u.System != nil {
		sys := *u.System
		next.System = &sys
	}
	if u.Radio != nil {
		stations := append([]config.RadioStation{}, *u.Radio...)
		next.Radio = &stations
	}
	if l := u.Lock; l != nil {
		pin := strings.TrimSpace(l.PIN)
		switch {
		case l.Remove:
			next.Lock = &Lock{}
		case pin != "":
			if len(pin) < 4 || len(pin) > 12 || strings.Trim(pin, "0123456789") != "" {
				return errors.New("the PIN must be 4 to 12 digits")
			}
			next.Lock = &Lock{PIN: pin}
		}
		// Neither: the PIN stays as it is.
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

// Export returns everything saved on the settings page, secrets included,
// for a backup file.
func (s *Store) Export() File {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.file
	f.Port = nil // belongs to this Pi, not to the settings
	return f
}

// Import replaces the saved settings with those of a backup. Nothing is
// written unless they make a valid configuration on this Pi.
func (s *Store) Import(f File) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.Port = s.file.Port
	if _, err := apply(s.base, f, s.installed()); err != nil {
		return err
	}
	if err := s.write(f); err != nil {
		return err
	}
	s.file = f
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

func boolPtr(b bool) *bool { return &b }
