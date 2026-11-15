// Package screen can have the Pi send the TV a 1920 x 1080 picture while the
// dashboard runs, and the picture the desktop is set to otherwise.
//
// A 4K picture has four times the pixels of a 1080p one, and everything the
// Pi draws costs four times as much. A TV enlarges a 1080p picture to its
// panel by itself, and well. So where the Pi struggles at 4K, the largest
// saving there is costs nothing but some sharpness of small lettering.
//
// The screens are asked and set through wlr-randr, the tool Raspberry Pi OS's
// own screen settings use on its Wayland desktop.
package screen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Mode is one picture size and rate a screen offers.
type Mode struct {
	Width, Height int
	// Hz is the refresh rate as the screen states it; 0 when it states none.
	Hz      float64
	Current bool
}

// arg is the mode as wlr-randr wants to be told it.
func (m Mode) arg() string {
	if m.Hz == 0 {
		return fmt.Sprintf("%dx%d", m.Width, m.Height)
	}
	return fmt.Sprintf("%dx%d@%.6fHz", m.Width, m.Height, m.Hz)
}

// Output is a screen connected to the Pi.
type Output struct {
	Name    string
	Enabled bool
	Modes   []Mode
}

// Current returns the mode in use.
func (o Output) Current() (Mode, bool) {
	for _, m := range o.Modes {
		if m.Current {
			return m, true
		}
	}
	return Mode{}, false
}

var (
	outputLine = regexp.MustCompile(`^(\S+)(?: "|$)`)
	modeLine   = regexp.MustCompile(`^\s+(\d+)x(\d+) px(?:, ([0-9.]+) Hz)?(?: \(([^)]*)\))?`)
)

// Parse reads what wlr-randr prints about the screens.
func Parse(text string) []Output {
	var out []Output
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			if m := outputLine.FindStringSubmatch(line); m != nil {
				out = append(out, Output{Name: m[1]})
			}
			continue
		}
		if len(out) == 0 {
			continue
		}
		o := &out[len(out)-1]
		trimmed := strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(trimmed, "Enabled:"); ok {
			o.Enabled = strings.TrimSpace(v) == "yes"
			continue
		}
		if m := modeLine.FindStringSubmatch(line); m != nil {
			w, _ := strconv.Atoi(m[1])
			h, _ := strconv.Atoi(m[2])
			hz, _ := strconv.ParseFloat(m[3], 64)
			o.Modes = append(o.Modes, Mode{Width: w, Height: h, Hz: hz, Current: strings.Contains(m[4], "current")})
		}
	}
	return out
}

// smaller picks the 1920 x 1080 mode to use instead of a larger one: at the
// rate in use if the screen offers that, otherwise the fastest up to 60.
func smaller(o Output, now Mode) (Mode, bool) {
	var best Mode
	found := false
	for _, m := range o.Modes {
		if m.Width != 1920 || m.Height != 1080 || m.Hz > 60.5 {
			continue
		}
		same := func(x Mode) bool { return x.Hz > now.Hz-0.5 && x.Hz < now.Hz+0.5 }
		if !found || (same(m) && !same(best)) || (same(m) == same(best) && m.Hz > best.Hz) {
			best, found = m, true
		}
	}
	return best, found
}

// Control asks and sets the screens.
type Control struct {
	// Run executes wlr-randr; nil means the real one. For tests.
	Run func(ctx context.Context, args ...string) ([]byte, error)
	// StatePath is the file that remembers what the screens were set to.
	StatePath string
}

// ErrNoTool is returned where wlr-randr is not installed, or the desktop is
// not one it can talk to.
var ErrNoTool = errors.New("the screen cannot be set here: this needs the tool wlr-randr and a Wayland desktop")

func (c *Control) run(ctx context.Context, args ...string) ([]byte, error) {
	if c.Run != nil {
		return c.Run(ctx, args...)
	}
	if _, err := exec.LookPath("wlr-randr"); err != nil {
		return nil, ErrNoTool
	}
	return exec.CommandContext(ctx, "wlr-randr", args...).CombinedOutput()
}

// Lower switches every screen that shows more than 1920 x 1080 to that size,
// if it offers it, and remembers what it was. It returns what it changed, in
// words. Screens already lowered by an earlier call are left alone.
func (c *Control) Lower(ctx context.Context) ([]string, error) {
	text, err := c.run(ctx)
	if err != nil {
		return nil, err
	}
	was := c.remembered()
	var changed []string
	for _, o := range Parse(string(text)) {
		now, ok := o.Current()
		if !o.Enabled || !ok || now.Width*now.Height <= 1920*1080 {
			continue
		}
		to, ok := smaller(o, now)
		if !ok {
			continue
		}
		if out, err := c.run(ctx, "--output", o.Name, "--mode", to.arg()); err != nil {
			return changed, fmt.Errorf("%s: %w: %s", o.Name, err, strings.TrimSpace(string(out)))
		}
		// The first size it had is the one to go back to.
		if _, known := was[o.Name]; !known {
			was[o.Name] = now.arg()
		}
		changed = append(changed, fmt.Sprintf("%s: %dx%d instead of %dx%d", o.Name, to.Width, to.Height, now.Width, now.Height))
	}
	if len(changed) > 0 {
		if err := c.remember(was); err != nil {
			return changed, err
		}
	}
	return changed, nil
}

// Restore gives every screen that Lower changed its own size back.
func (c *Control) Restore(ctx context.Context) ([]string, error) {
	was := c.remembered()
	if len(was) == 0 {
		return nil, nil
	}
	var restored []string
	var firstErr error
	for name, mode := range was {
		if out, err := c.run(ctx, "--output", name, "--mode", mode); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(out)))
			}
			continue
		}
		restored = append(restored, name+": "+mode)
	}
	// Forgotten either way: a screen that is gone cannot be set back, and
	// the desktop sets its own size again when it is next started.
	_ = os.Remove(c.StatePath)
	return restored, firstErr
}

func (c *Control) remembered() map[string]string {
	was := map[string]string{}
	if data, err := os.ReadFile(c.StatePath); err == nil {
		_ = json.Unmarshal(data, &was)
	}
	return was
}

func (c *Control) remember(was map[string]string) error {
	data, err := json.Marshal(was)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.StatePath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(c.StatePath, data, 0o600)
}
