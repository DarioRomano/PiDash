// Package config loads and validates the PiDash configuration file.
package config

import (
	"fmt"
	"os"
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
	// Use 24-hour time.
	Clock24h bool `toml:"clock_24h" json:"clock_24h"`
	// Locale used for date formatting, e.g. "en-GB". Empty uses the browser default.
	Locale string `toml:"locale" json:"locale"`
}

// Input maps browser KeyboardEvent.code values to dashboard actions.
type Input struct {
	Keys map[string]string `toml:"keys"`
}

// System configures the system-stats provider.
type System struct {
	Enabled  bool     `toml:"enabled"`
	Interval Duration `toml:"interval"`
}

// Kiosk configures how the fullscreen browser on the TV is started and stopped.
type Kiosk struct {
	// Unit is the systemd user unit that runs the browser. Empty disables the
	// "Show on TV" / "Exit to desktop" controls.
	Unit string `toml:"unit"`
}

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
		Display: Display{SafeArea: 0.03, DebugKeys: true, PixelShift: 4, Clock24h: true},
		Input: Input{Keys: map[string]string{
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
			"MediaPlay":      "playpause",
			"MediaPause":     "playpause",
			"ContextMenu":    "menu",
			"Home":           "home",
			"F10":            "exit",
			"Digit0":         "0", "Digit1": "1", "Digit2": "2", "Digit3": "3", "Digit4": "4",
			"Digit5": "5", "Digit6": "6", "Digit7": "7", "Digit8": "8", "Digit9": "9",
		}},
		System: System{Enabled: true, Interval: Duration{10 * time.Second}},
		Kiosk:  Kiosk{Unit: "pidash-kiosk.service"},
	}
}

// Load reads path (if non-empty) over the defaults and validates the result.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, cfg.validate()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	// A user-supplied key map replaces the default one entirely so that
	// unwanted defaults can be removed; start from empty if the file has one.
	var probe struct {
		Input struct {
			Keys map[string]string `toml:"keys"`
		} `toml:"input"`
	}
	if err := toml.Unmarshal(data, &probe); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if probe.Input.Keys != nil {
		cfg.Input.Keys = map[string]string{}
	}
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return cfg, fmt.Errorf("%s: unknown key(s): %v", path, undecoded)
	}
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	if c.Display.SafeArea < 0 || c.Display.SafeArea > 0.2 {
		return fmt.Errorf("display.safe_area must be between 0 and 0.2, got %v", c.Display.SafeArea)
	}
	if c.System.Interval.Duration < time.Second {
		return fmt.Errorf("system.interval must be at least 1s")
	}
	return nil
}

// Client is the subset of the configuration sent to the browser.
type Client struct {
	Display Display           `json:"display"`
	Keys    map[string]string `json:"keys"`
}

// ForClient returns the browser-facing view of the configuration.
func (c Config) ForClient() Client {
	return Client{Display: c.Display, Keys: c.Input.Keys}
}
