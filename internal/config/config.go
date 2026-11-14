// Package config loads and validates the PiDash configuration file.
package config

import (
	"fmt"
	"github.com/DarioRomano/PiDash/internal/maps"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the top-level configuration, mirrored by config.example.toml.
type Config struct {
	Server  Server  `toml:"server"`
	Display Display `toml:"display"`
	Input   Input   `toml:"input"`
	System  System  `toml:"system"`
	Kiosk   Kiosk   `toml:"kiosk"`
	Apps    []App   `toml:"apps"`
	Photos  Photos  `toml:"photos"`
	Weather Weather `toml:"weather"`
	Remote  Remote  `toml:"remote"`
	TV      TV      `toml:"tv"`
	Sound   Sound   `toml:"sound"`
	Wifi    Wifi    `toml:"guest_wifi"`
	Games   Games   `toml:"games"`
	// Calendar shows the next events from calendars published as links.
	Calendar Calendar `toml:"calendar"`
	// Home shows and switches devices of a Home Assistant installation.
	Home Home `toml:"homeassistant"`
	// Update controls automatic updates.
	Update Update `toml:"update"`
	// Radio lists internet radio stations, shown as tiles in the app row.
	Radio []RadioStation `toml:"radio"`
}

// RadioStation is one internet radio station.
type RadioStation struct {
	Name string `toml:"name" json:"name"`
	// URL is the address of the stream (MP3, AAC or Ogg).
	URL string `toml:"url" json:"url"`
}

// Calendar lists calendar links.
type Calendar struct {
	// URLs are iCalendar links (https:// or webcal://), as calendar programs
	// offer under "share" or "secret address".
	URLs []string `toml:"urls"`
	// Days is how far ahead events are shown.
	Days int `toml:"days"`
}

// Home is the connection to Home Assistant.
type Home struct {
	// URL is the address of Home Assistant, e.g. "http://homeassistant.local:8123".
	URL string `toml:"url"`
	// Token is a long-lived access token, created on the profile page there.
	Token string `toml:"token"`
	// Entities are the ids of the things to show as tiles, e.g. "light.sofa".
	Entities []string `toml:"entities"`
}

// Enabled reports whether Home Assistant is set up.
func (h Home) Enabled() bool { return h.URL != "" && h.Token != "" }

// Update controls automatic updates.
type Update struct {
	// Auto installs new releases by itself, at night, when no app is open.
	Auto bool `toml:"auto"`
}

var entityPattern = regexp.MustCompile(`^[a-z_]+\.[a-z0-9_]+$`)

func (c *Config) validateExtras() error {
	for i, u := range c.Calendar.URLs {
		u = strings.TrimSpace(u)
		c.Calendar.URLs[i] = u
		parsed, err := url.Parse(u)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "webcal") {
			return fmt.Errorf("calendar link %d must start with https:// or webcal://", i+1)
		}
	}
	if len(c.Calendar.URLs) > 10 {
		return fmt.Errorf("at most 10 calendars can be shown")
	}
	if c.Calendar.Days == 0 {
		c.Calendar.Days = 14
	}
	if c.Calendar.Days < 1 || c.Calendar.Days > 60 {
		return fmt.Errorf("calendar.days must be between 1 and 60, got %d", c.Calendar.Days)
	}
	if len(c.Radio) > 12 {
		return fmt.Errorf("at most 12 radio stations can be shown")
	}
	for i := range c.Radio {
		st := &c.Radio[i]
		st.Name, st.URL = strings.TrimSpace(st.Name), strings.TrimSpace(st.URL)
		parsed, err := url.Parse(st.URL)
		if st.Name == "" || err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("radio station %d needs a name and the http:// or https:// address of its stream", i+1)
		}
	}
	c.Home.URL = strings.TrimRight(strings.TrimSpace(c.Home.URL), "/")
	if c.Home.URL != "" {
		parsed, err := url.Parse(c.Home.URL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("the Home Assistant address must be a full http:// or https:// address, got %q", c.Home.URL)
		}
	}
	if len(c.Home.Entities) > 16 {
		return fmt.Errorf("at most 16 Home Assistant tiles can be shown")
	}
	for _, e := range c.Home.Entities {
		if !entityPattern.MatchString(e) {
			return fmt.Errorf("%q is not a Home Assistant entity id like light.sofa", e)
		}
	}
	return nil
}

// Server holds HTTP settings.
type Server struct {
	Listen string `toml:"listen"`
}

// Display holds settings that are forwarded to the browser.
type Display struct {
	// SafeArea is the inset applied on every edge as a fraction of the screen,
	// for TVs that overscan. 0.03 means 3%.
	SafeArea float64 `toml:"safe_area" json:"safe_area"`
	// DebugKeys shows the last raw key code and mapped action on screen.
	// Useful while mapping a new TV remote over CEC.
	DebugKeys bool `toml:"debug_keys" json:"debug_keys"`
	// PixelShift nudges the whole UI by a few pixels periodically to avoid
	// burn-in on OLED and plasma panels. 0 disables it.
	PixelShift int `toml:"pixel_shift" json:"pixel_shift"`
	// AppRowTimeout closes the app row after this many seconds without a
	// key press or pointer movement, returning to the clock. 0 keeps it open.
	AppRowTimeout int `toml:"app_row_timeout" json:"app_row_timeout"`
	// NightStart and NightEnd ("HH:MM") bound the hours in which the
	// dashboard shows only a dim clock on black. Both empty switches it off.
	NightStart string `toml:"night_start" json:"night_start"`
	NightEnd   string `toml:"night_end" json:"night_end"`
	// EveningDim darkens the picture as the evening comes, from sunset on:
	// "off" (default), "light" or "strong".
	EveningDim string `toml:"evening_dim" json:"evening_dim"`
	// Language of everything on screen: "en", "de", "fr" or "it".
	Language string `toml:"language" json:"language"`
	// ClockPosition is the corner the clock sits in: "bottom-left" (default),
	// "bottom-right", "top-left" or "top-right".
	ClockPosition string `toml:"clock_position" json:"clock_position"`
	// ClockSize is "large" (default), "medium" or "small".
	ClockSize string `toml:"clock_size" json:"clock_size"`
	// ClockStyle is "bold" (default), "slim" or "analog", a clock with hands.
	ClockStyle string `toml:"clock_style" json:"clock_style"`
	// View is what the dashboard shows when it starts: "clock" (default),
	// "details" (also the weather and the photo's caption) or "photo" (only
	// the photo). Up and Down switch between them at any time.
	View string `toml:"view" json:"view"`
	// Background is how the background without photos behaves: "moving"
	// (default) drifts and reacts to the remote; "still" is painted once and
	// costs the Pi no work.
	Background string `toml:"background" json:"background"`
	// Performance trades smoothness for the work the Pi has to do, and so
	// for the power it draws and the heat it makes: "smooth", "balanced"
	// (default) or "saver".
	Performance string `toml:"performance" json:"performance"`
	// BackgroundFPS sets how many pictures a second the moving background
	// is drawn with, from 1 to 60; 0 leaves it to Performance.
	BackgroundFPS int `toml:"background_fps" json:"background_fps"`
	// Sounds plays a small sound when the remote moves the highlight or
	// chooses something.
	Sounds bool `toml:"sounds" json:"sounds"`
	// Use 24-hour time.
	Clock24h bool `toml:"clock_24h" json:"clock_24h"`
	// Locale used for date formatting, e.g. "en-GB". Empty uses the browser default.
	Locale string `toml:"locale" json:"locale"`
}

// Input maps browser KeyboardEvent.code values to dashboard actions, and
// names the remote-control keys that close an app.
type Input struct {
	Keys map[string]string `toml:"keys"`
	// AppExitKeys are Linux key names (KEY_EXIT, KEY_BACK, …) that return from
	// an app to the dashboard when held for AppExitHold. They are read straight
	// from the remote, so they work while the app has the screen.
	AppExitKeys []string `toml:"app_exit_keys"`
	// AppExitHold is how long the key must be held; "0s" acts on the first press.
	AppExitHold Duration `toml:"app_exit_hold"`
	// RemoteKeys maps extra Linux key names from the TV remote (KEY_RED, …) to
	// dashboard actions, on top of the built-in ones for OK, Back and so on.
	RemoteKeys map[string]string `toml:"remote_keys"`
	// PadInWebsites makes a game controller press keyboard keys inside
	// website apps (arrows, Enter, Escape), which know no controllers.
	PadInWebsites bool `toml:"controller_in_websites"`
}

// Remote configures the phone remote.
type Remote struct {
	// MouseKeyboard lets the phone act as a mouse and keyboard for whatever is
	// on screen. Off by default: it gives every device on the home network
	// control of the desktop session.
	MouseKeyboard bool `toml:"mouse_keyboard"`
	// PIN, when set, must be entered on the phone before it can act as mouse
	// and keyboard.
	PIN string `toml:"pin"`
}

// TV is the daily schedule for switching the TV off and on over HDMI-CEC.
// Games configures the tiles PiDash makes for games.
type Games struct {
	// Moonlight shows the games of the PCs Moonlight is paired with as
	// tiles of their own, next to the apps.
	Moonlight bool `toml:"moonlight"`
	// RetroArch shows the games marked as favourites in RetroArch, and the
	// ones played there last, as tiles of their own.
	RetroArch bool `toml:"retroarch"`
}

// Wifi is the network visitors may use. Its name and password are shown on
// the TV as a code a phone can scan.
type Wifi struct {
	Name     string `toml:"name"`
	Password string `toml:"password"`
	// Hidden marks a network that does not announce its name.
	Hidden bool `toml:"hidden"`
}

// Sound configures where the Pi's sound goes.
type Sound struct {
	// FollowBluetooth moves the sound to Bluetooth headphones or a speaker
	// when they connect, and back when they disconnect.
	FollowBluetooth bool `toml:"follow_bluetooth"`
}

// TV configures the daily power schedule over HDMI-CEC.
type TV struct {
	// Off and On are times of day like "23:30"; empty means "do not".
	Off string `toml:"off"`
	On  string `toml:"on"`
	// WakeOnStart switches the TV on whenever the dashboard starts: when
	// the Pi is switched on, but also when the dashboard is started again
	// after a fault or from afar. Off by default: a TV in standby stays so.
	WakeOnStart bool `toml:"wake_on_start"`
	// WakeOnController switches the TV on, and to the Pi, when a game
	// controller is switched on while the TV is in standby.
	WakeOnController bool `toml:"wake_on_controller"`
	// ControllerVolume lets a game controller change the volume: its logo
	// button held, and up or down.
	ControllerVolume bool `toml:"controller_volume"`
	// VolumeTo is who sets the volume: "tv", or "audio" for a soundbar or
	// receiver connected by HDMI.
	VolumeTo string `toml:"volume_to"`
}

var clockPattern = regexp.MustCompile(`^([01]?\d|2[0-3]):[0-5]\d$`)

// sunPattern matches a time that follows the sun: "sunset", "sunrise", and
// either moved by minutes, as in "sunset+90" or "sunrise-30".
var sunPattern = regexp.MustCompile(`^(sunrise|sunset)([+-]\d{1,3})?$`)

// System configures the system-stats provider.
type System struct {
	Enabled  bool     `toml:"enabled"`
	Interval Duration `toml:"interval"`
	// CoolAbove is the temperature in °C above which the dashboard stops
	// its moving background and the movement of photos until the Pi has
	// cooled down again. 0 never does.
	CoolAbove float64 `toml:"cool_above"`
}

// Kiosk configures how the fullscreen browser on the TV is started and stopped.
type Kiosk struct {
	// Unit is the systemd user unit that runs the browser. Empty disables the
	// "Show on TV" / "Exit to desktop" controls.
	Unit string `toml:"unit"`
	// Watchdog restarts the browser when the dashboard in it has stopped
	// answering for a few minutes.
	Watchdog bool `toml:"watchdog"`
}

// Weather configures the forecast from Open-Meteo. It is on when a location
// or coordinates are set.
type Weather struct {
	// Location is a place name, like "Zurich" or "Portland, Oregon".
	Location string `toml:"location"`
	// Latitude and Longitude pin the exact spot instead of a place name.
	Latitude  *float64 `toml:"latitude"`
	Longitude *float64 `toml:"longitude"`
	// Name overrides the label shown on the TV.
	Name string `toml:"name"`
	// Units is "metric" (°C, km/h, mm) or "imperial" (°F, mph, inch).
	Units    string   `toml:"units"`
	Interval Duration `toml:"interval"`
}

// Enabled reports whether a place has been configured.
func (w Weather) Enabled() bool {
	return strings.TrimSpace(w.Location) != "" || (w.Latitude != nil && w.Longitude != nil)
}

func (w *Weather) validate() error {
	w.Location = strings.TrimSpace(w.Location)
	if (w.Latitude == nil) != (w.Longitude == nil) {
		return fmt.Errorf("weather: set both latitude and longitude, or neither")
	}
	if w.Latitude != nil && (*w.Latitude < -90 || *w.Latitude > 90 || *w.Longitude < -180 || *w.Longitude > 180) {
		return fmt.Errorf("weather: latitude must be between -90 and 90, longitude between -180 and 180")
	}
	if w.Units != "metric" && w.Units != "imperial" {
		return fmt.Errorf("weather.units must be \"metric\" or \"imperial\", got %q", w.Units)
	}
	if w.Interval.Duration < 5*time.Minute {
		return fmt.Errorf("weather.interval must be at least 5m")
	}
	return nil
}

// Photo sources for the background slideshow.
const (
	PhotoSourceNone     = ""
	PhotoSourceSynology = "synology"
	PhotoSourceFolder   = "folder"
)

// Photos configures the background slideshow.
type Photos struct {
	// Source is "synology", "folder", or "" for no photos.
	Source string `toml:"source"`
	// Interval is how long each photo stays on screen.
	Interval Duration `toml:"interval"`
	// Refresh is how often the chosen folders are re-read for new photos.
	Refresh Duration `toml:"refresh"`
	// Motion animates the photos with slow pans and zooms: "off" (default),
	// "whole" (the whole photo always stays on screen) or "fill" (the photo
	// fills the screen and its edges may drift out of view).
	Motion string `toml:"motion"`
	// PairPortraits shows two upright photos side by side.
	PairPortraits bool `toml:"pair_portraits"`
	// OnThisDay shows photos taken on today's date in earlier years first.
	OnThisDay bool `toml:"on_this_day"`
	// Videos also shows short video clips, without sound.
	Videos bool `toml:"videos"`
	// Map shows a map beside a photo that knows where it was taken. The map
	// comes from MapTiles, a web address with {z}, {x} and {y} in it.
	Map      bool   `toml:"map"`
	MapTiles string `toml:"map_tiles"`
	// CacheMB limits the disk space used for rendered photos.
	CacheMB  int            `toml:"cache_mb"`
	Synology SynologyPhotos `toml:"synology"`
	Folder   PhotoFolder    `toml:"folder"`
}

// SynologyPhotos is the connection to Synology Photos on a NAS.
type SynologyPhotos struct {
	// URL is the address of DSM, e.g. "http://192.168.1.20:5000".
	URL      string `toml:"url"`
	Username string `toml:"username"`
	Password string `toml:"password"`
	// InsecureTLS accepts a self-signed HTTPS certificate.
	InsecureTLS bool `toml:"insecure_tls"`
	// Quality is "original" (render from the full file, sharpest) or
	// "preview" (use the NAS's ready-made 1280-pixel preview, fastest).
	Quality string `toml:"quality"`
}

// PhotoFolder is a folder of photos on the Pi or a mounted share.
type PhotoFolder struct {
	Path string `toml:"path"`
}

// Photo quality settings.
const (
	PhotoQualityOriginal = "original"
	PhotoQualityPreview  = "preview"

	PhotoMotionOff   = "off"
	PhotoMotionWhole = "whole"
	PhotoMotionFill  = "fill"
)

// App launch modes.
const (
	AppModeDesktop = "desktop"
	AppModeConsole = "console"
)

// Browsers a web app can run in.
const (
	BrowserChromium = "chromium"
	BrowserFirefox  = "firefox"
)

// App is something that can be launched from the dashboard: either a program
// (Command) or a website shown fullscreen in its own browser profile (URL).
type App struct {
	// ID is a short unique name used in URLs and unit names: a-z, 0-9 and "-".
	ID string `toml:"id"`
	// Name is shown on the TV. Defaults to ID.
	Name string `toml:"name"`
	// Command is run with `sh -c` as the desktop user. Use this or URL, not both.
	Command string `toml:"command"`
	// URL makes this a web app: the address is opened fullscreen in Browser.
	URL string `toml:"url"`
	// Browser for a web app: "chromium" (default) or "firefox".
	Browser string `toml:"browser"`
	// AdBlock installs uBlock Origin into the app's browser profile (Firefox only).
	AdBlock bool `toml:"adblock"`
	// UserAgent overrides the browser's user agent for a web app, for sites
	// that only show their TV interface to TVs.
	UserAgent string `toml:"user_agent"`
	// Mode is "desktop" (default) or "console" (own virtual terminal).
	Mode string `toml:"mode"`
	// Icon is optional: a path to an SVG or PNG, or an icon name. When empty
	// the icon of the installed program or a built-in one is used.
	Icon string `toml:"icon"`
	// Window is the name of the program's window (its app id), by which
	// PiDash can give it the keyboard. Filled in from the menu entry for
	// installed programs; rarely needed by hand.
	Window string `toml:"window"`
	// Background keeps the app running when it is left: it is only put
	// behind the dashboard, so that its music carries on. For websites
	// that play music, such as Spotify.
	Background bool `toml:"background"`
	// Zoom enlarges a web app, for sites drawn too small on a 4K screen:
	// 1.5 shows everything half as large again. 0 or 1 leaves it alone.
	Zoom float64 `toml:"zoom"`
}

var appIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Duration is a time.Duration that decodes from TOML strings like "30s".
type Duration struct{ time.Duration }

// UnmarshalText implements encoding.TextUnmarshaler.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (d Duration) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// Default returns the built-in configuration. Values from the file override it.
func Default() Config {
	return Config{
		Server:  Server{Listen: ":8080"},
		Display: Display{Sounds: true, SafeArea: 0.03, DebugKeys: true, PixelShift: 4, AppRowTimeout: 30, Clock24h: true, Performance: "balanced"},
		Input: Input{PadInWebsites: true, AppExitKeys: []string{}, AppExitHold: Duration{2 * time.Second}, Keys: map[string]string{
			"ArrowUp":        "up",
			"ArrowDown":      "down",
			"ArrowLeft":      "left",
			"ArrowRight":     "right",
			"Enter":          "select",
			"NumpadEnter":    "select",
			"Space":          "select",
			"Escape":         "back",
			"Backspace":      "back",
			"BrowserBack":    "back",
			"MediaPlayPause": "playpause",
			"MediaTrackNext": "next", "MediaTrackPrevious": "previous",
			"MediaPlay":   "playpause",
			"MediaPause":  "playpause",
			"ContextMenu": "menu",
			"Home":        "home",
			"F10":         "exit",
			// Keyboard shortcuts.
			"KeyA":     "menu",
			"KeyW":     "weather",
			"KeyS":     "settings",
			"KeyP":     "playpause",
			"KeyH":     "help",
			"F1":       "help",
			"Slash":    "help",
			"PageUp":   "left",
			"PageDown": "right",
			"Digit0":   "0", "Digit1": "1", "Digit2": "2", "Digit3": "3", "Digit4": "4",
			"Digit5": "5", "Digit6": "6", "Digit7": "7", "Digit8": "8", "Digit9": "9",
		}},
		System:   System{Enabled: true, Interval: Duration{10 * time.Second}, CoolAbove: 75},
		Kiosk:    Kiosk{Unit: "pidash-kiosk.service", Watchdog: true},
		TV:       TV{WakeOnController: true, ControllerVolume: true, VolumeTo: "tv"},
		Sound:    Sound{FollowBluetooth: true},
		Games:    Games{Moonlight: true, RetroArch: true},
		Weather:  Weather{Units: "metric", Interval: Duration{15 * time.Minute}},
		Calendar: Calendar{Days: 14},
		Photos: Photos{
			Interval: Duration{30 * time.Second},
			Refresh:  Duration{6 * time.Hour},
			CacheMB:  2000,
			Map:      true,
			MapTiles: maps.DefaultURL,
			Synology: SynologyPhotos{Quality: PhotoQualityOriginal},
		},
	}
}

// Load reads path (if non-empty) over the defaults and validates the result.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, cfg.Finish()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	// Keys in the file are added to the built-in key map, so shortcuts that
	// arrive with an update work without editing the settings. Mapping a key
	// to "none" switches a built-in one off.
	var file struct {
		Input struct {
			Keys map[string]string `toml:"keys"`
		} `toml:"input"`
	}
	if err := toml.Unmarshal(data, &file); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	keys := cfg.Input.Keys
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	for k, v := range file.Input.Keys {
		keys[k] = v
	}
	for k, v := range keys {
		if v = strings.ToLower(strings.TrimSpace(v)); v == "" || v == "none" {
			delete(keys, k)
		} else {
			keys[k] = v
		}
	}
	cfg.Input.Keys = keys
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return cfg, fmt.Errorf("%s: unknown key(s): %v", path, undecoded)
	}
	return cfg, cfg.Finish()
}

// Finish fills in per-entry defaults and validates the configuration.
func (c *Config) Finish() error {
	seen := map[string]bool{}
	for i := range c.Apps {
		a := &c.Apps[i]
		a.ID = strings.TrimSpace(a.ID)
		if !appIDPattern.MatchString(a.ID) {
			return fmt.Errorf("apps[%d].id %q must be 1-32 characters of a-z, 0-9 or '-', starting with a letter or digit", i, a.ID)
		}
		if seen[a.ID] {
			return fmt.Errorf("apps: duplicate id %q", a.ID)
		}
		seen[a.ID] = true
		if strings.TrimSpace(a.Name) == "" {
			a.Name = a.ID
		}
		a.Command, a.URL = strings.TrimSpace(a.Command), strings.TrimSpace(a.URL)
		switch {
		case a.Command == "" && a.URL == "":
			return fmt.Errorf("apps[%d] (%s): set either command (a program) or url (a website)", i, a.ID)
		case a.Command != "" && a.URL != "":
			return fmt.Errorf("apps[%d] (%s): set either command or url, not both", i, a.ID)
		}
		switch a.Mode {
		case "":
			a.Mode = AppModeDesktop
		case AppModeDesktop, AppModeConsole:
		default:
			return fmt.Errorf("apps[%d] (%s): mode must be %q or %q, got %q", i, a.ID, AppModeDesktop, AppModeConsole, a.Mode)
		}
		if a.URL != "" {
			u, err := url.Parse(a.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("apps[%d] (%s): url must be a full http:// or https:// address, got %q", i, a.ID, a.URL)
			}
			if a.Mode != AppModeDesktop {
				return fmt.Errorf("apps[%d] (%s): a web app runs in a browser and needs mode = %q", i, a.ID, AppModeDesktop)
			}
			switch a.Browser {
			case "":
				a.Browser = BrowserChromium
			case BrowserChromium, BrowserFirefox:
			default:
				return fmt.Errorf("apps[%d] (%s): browser must be %q or %q, got %q", i, a.ID, BrowserChromium, BrowserFirefox, a.Browser)
			}
			if a.AdBlock && a.Browser != BrowserFirefox {
				return fmt.Errorf("apps[%d] (%s): adblock = true needs browser = %q", i, a.ID, BrowserFirefox)
			}
			if a.Zoom != 0 && (a.Zoom < 0.5 || a.Zoom > 4) {
				return fmt.Errorf("apps[%d] (%s): zoom must be between 0.5 and 4, got %v", i, a.ID, a.Zoom)
			}
			if strings.ContainsAny(a.UserAgent, "\r\n") {
				return fmt.Errorf("apps[%d] (%s): user_agent must be a single line", i, a.ID)
			}
		} else if a.Browser != "" || a.AdBlock || a.UserAgent != "" || a.Zoom != 0 {
			return fmt.Errorf("apps[%d] (%s): browser, adblock, user_agent and zoom only apply to web apps (url)", i, a.ID)
		}
	}
	if err := c.Photos.validate(); err != nil {
		return err
	}
	if err := c.Weather.validate(); err != nil {
		return err
	}
	for name, v := range map[string]string{"tv.off": c.TV.Off, "tv.on": c.TV.On} {
		if v != "" && !clockPattern.MatchString(v) {
			return fmt.Errorf("%s must be a time of day like \"23:30\", got %q", name, v)
		}
	}
	if c.TV.Off != "" && c.TV.Off == c.TV.On {
		return fmt.Errorf("tv.off and tv.on must be different times")
	}
	if n := len(c.Remote.PIN); n != 0 && (n < 4 || n > 32) {
		return fmt.Errorf("remote.pin must be 4 to 32 characters")
	}
	if c.Input.AppExitHold.Duration < 0 || c.Input.AppExitHold.Duration > 10*time.Second {
		return fmt.Errorf("input.app_exit_hold must be between 0s and 10s")
	}
	// Tidies addresses and fills in defaults, so it must work on c itself.
	if err := c.validateExtras(); err != nil {
		return err
	}
	return c.validate()
}

func (p *Photos) validate() error {
	switch p.Motion {
	case "":
		p.Motion = PhotoMotionOff
	case PhotoMotionOff, PhotoMotionWhole, PhotoMotionFill:
	default:
		return fmt.Errorf("photos.motion must be %q, %q or %q, got %q", PhotoMotionOff, PhotoMotionWhole, PhotoMotionFill, p.Motion)
	}
	if p.Interval.Duration < 5*time.Second {
		return fmt.Errorf("photos.interval must be at least 5s")
	}
	if p.Refresh.Duration < time.Minute {
		return fmt.Errorf("photos.refresh must be at least 1m")
	}
	if err := maps.CheckURL(p.MapTiles); p.Map && err != nil {
		return fmt.Errorf("photos.map_tiles %v", err)
	}
	if p.CacheMB < 50 {
		return fmt.Errorf("photos.cache_mb must be at least 50")
	}
	switch p.Source {
	case PhotoSourceNone:
	case PhotoSourceSynology:
		s := &p.Synology
		s.URL = strings.TrimRight(strings.TrimSpace(s.URL), "/")
		u, err := url.Parse(s.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("photos.synology.url must be the address of your NAS, like http://192.168.1.20:5000, got %q", s.URL)
		}
		if s.Username == "" || s.Password == "" {
			return fmt.Errorf("photos.synology needs username and password")
		}
		if s.Quality != PhotoQualityOriginal && s.Quality != PhotoQualityPreview {
			return fmt.Errorf("photos.synology.quality must be %q or %q, got %q", PhotoQualityOriginal, PhotoQualityPreview, s.Quality)
		}
	case PhotoSourceFolder:
		if !strings.HasPrefix(p.Folder.Path, "/") {
			return fmt.Errorf("photos.folder.path must be a full path like /home/pi/Pictures, got %q", p.Folder.Path)
		}
	default:
		return fmt.Errorf("photos.source must be %q, %q or empty, got %q", PhotoSourceSynology, PhotoSourceFolder, p.Source)
	}
	return nil
}

func (c Config) validate() error {
	for _, choice := range []struct {
		name  string
		value *string
		of    []string
	}{
		{"language", &c.Display.Language, []string{"en", "de", "fr", "it"}},
		{"clock_position", &c.Display.ClockPosition, []string{"bottom-left", "bottom-right", "top-left", "top-right"}},
		{"clock_size", &c.Display.ClockSize, []string{"large", "medium", "small"}},
		{"clock_style", &c.Display.ClockStyle, []string{"bold", "slim", "analog"}},
		{"evening_dim", &c.Display.EveningDim, []string{"off", "light", "strong"}},
		{"view", &c.Display.View, []string{"clock", "details", "photo"}},
		{"background", &c.Display.Background, []string{"moving", "still"}},
		{"performance", &c.Display.Performance, []string{"smooth", "balanced", "saver"}},
	} {
		if *choice.value != "" && !slices.Contains(choice.of, *choice.value) {
			return fmt.Errorf("display.%s must be one of %s, got %q", choice.name, strings.Join(choice.of, ", "), *choice.value)
		}
	}
	if c.Display.BackgroundFPS < 0 || c.Display.BackgroundFPS > 60 {
		return fmt.Errorf("display.background_fps must be between 1 and 60, or 0 to leave it to display.performance; got %d", c.Display.BackgroundFPS)
	}
	if v := c.TV.VolumeTo; v != "" && v != "tv" && v != "audio" {
		return fmt.Errorf("tv.volume_to must be \"tv\" or \"audio\", got %q", v)
	}
	if (c.Display.NightStart == "") != (c.Display.NightEnd == "") {
		return fmt.Errorf("display.night_start and display.night_end must both be set, or both be empty")
	}
	for name, v := range map[string]string{"night_start": c.Display.NightStart, "night_end": c.Display.NightEnd} {
		if v != "" && !clockPattern.MatchString(v) && !sunPattern.MatchString(v) {
			return fmt.Errorf("display.%s must be a time like 22:30, or follow the sun like \"sunset\" or \"sunset+60\"; got %q", name, v)
		}
	}
	if c.Display.AppRowTimeout < 0 || c.Display.AppRowTimeout > 3600 {
		return fmt.Errorf("display.app_row_timeout must be between 0 and 3600 seconds, got %d", c.Display.AppRowTimeout)
	}
	if c.Display.SafeArea < 0 || c.Display.SafeArea > 0.2 {
		return fmt.Errorf("display.safe_area must be between 0 and 0.2, got %v", c.Display.SafeArea)
	}
	if c.System.Interval.Duration < time.Second {
		return fmt.Errorf("system.interval must be at least 1s")
	}
	if t := c.System.CoolAbove; t != 0 && (t < 40 || t > 100) {
		return fmt.Errorf("system.cool_above must be 0 (never) or between 40 and 100 °C, got %v", t)
	}
	return nil
}

// Client is the subset of the configuration sent to the browser.
type Client struct {
	Display Display           `json:"display"`
	Keys    map[string]string `json:"keys"`
	// Version is the running PiDash version. The dashboard reloads itself
	// when it changes, so an update reaches the TV without a restart.
	Version string `json:"version"`
	// Notices are problems the person at the TV should know about, such as a
	// mistake in the settings file or a port that was already taken.
	Notices []string `json:"notices"`
	// Started identifies this run of the backend. The dashboard reloads when
	// it changes, which is how saved settings reach the TV.
	Started int64 `json:"started"`
	// Radio are the stations in the app row.
	Radio []RadioStation `json:"radio"`
	// GuestWifi is the name of the network for visitors; empty when none is
	// set. Its password is fetched only when the code is shown.
	GuestWifi string `json:"guest_wifi"`
	// PhotoMap is whether a map is shown beside a photo's details.
	PhotoMap bool `json:"photo_map"`
}

// ForClient returns the browser-facing view of the configuration.
func (c Config) ForClient() Client {
	stations := c.Radio
	if stations == nil {
		stations = []RadioStation{}
	}
	return Client{Display: c.Display, Keys: c.Input.Keys, Radio: stations, GuestWifi: c.Wifi.Name, PhotoMap: c.Photos.Map}
}
