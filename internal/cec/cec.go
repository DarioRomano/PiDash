// Package cec talks to the TV over HDMI-CEC using the cec-ctl tool from
// v4l-utils: it registers the Pi so the TV forwards remote-control keys, and
// switches the TV on and to standby.
package cec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Power is the TV's power state as it reports it.
type Power string

// Power states.
const (
	PowerOn      Power = "on"
	PowerStandby Power = "standby"
	PowerUnknown Power = "unknown"
)

// Runner runs cec-ctl with the given arguments and returns its output.
type Runner func(ctx context.Context, args ...string) (string, error)

// Controller sends CEC commands through the adapter that has a TV attached.
type Controller struct {
	// Run executes cec-ctl; Devices are the adapters to try. A Pi 5 has one
	// per HDMI port.
	Run     Runner
	Devices []string
	// OSDName is how the Pi appears in the TV's input list.
	OSDName string

	// OnTVStandby is called when the TV announces that it is switching off,
	// OnTVActive when it shows a sign of being on. Set before Follow starts.
	OnTVStandby func()
	OnTVActive  func()

	// active is true while the TV shows the Pi's input, as far as PiDash knows.
	active atomic.Bool
}

// New returns a controller that uses the system's cec-ctl.
func New() *Controller {
	return &Controller{
		Run: func(ctx context.Context, args ...string) (string, error) {
			out, err := exec.CommandContext(ctx, "cec-ctl", args...).CombinedOutput()
			return string(out), err
		},
		Devices: []string{"/dev/cec0", "/dev/cec1"},
		OSDName: "PiDash",
	}
}

// Available reports whether CEC can be used at all on this machine.
func (c *Controller) Available() bool {
	if _, err := exec.LookPath("cec-ctl"); err != nil {
		return false
	}
	for _, d := range c.Devices {
		if _, err := os.Stat(d); err == nil {
			return true
		}
	}
	return false
}

// ErrNoTV is returned when no adapter has a TV connected.
var ErrNoTV = errors.New("no TV found over HDMI-CEC (is the TV on, connected, and CEC enabled in its settings?)")

var (
	physRe  = regexp.MustCompile(`Physical Address\s*:\s*([0-9a-fA-F]\.[0-9a-fA-F]\.[0-9a-fA-F]\.[0-9a-fA-F])`)
	maskRe  = regexp.MustCompile(`Logical Address Mask\s*:\s*0x([0-9a-fA-F]+)`)
	powerRe = regexp.MustCompile(`pwr-state:\s*([a-z-]+)`)
)

type adapter struct {
	dev, phys string
	// registered is true once the Pi has claimed a CEC address on this adapter.
	registered bool
}

// find returns the adapter with a TV attached: the one that has been given a
// valid physical address by the TV.
func (c *Controller) find(ctx context.Context) (adapter, error) {
	for _, dev := range c.Devices {
		if _, err := os.Stat(dev); err != nil && c.usesRealDevices() {
			continue
		}
		// Without other options cec-ctl prints the adapter's state.
		out, err := c.run(ctx, "-d", dev)
		if err != nil {
			continue
		}
		m := physRe.FindStringSubmatch(out)
		if m == nil || strings.EqualFold(m[1], "f.f.f.f") {
			continue
		}
		a := adapter{dev: dev, phys: strings.ToLower(m[1])}
		if mm := maskRe.FindStringSubmatch(out); mm != nil && strings.Trim(mm[1], "0") != "" {
			a.registered = true
		}
		return a, nil
	}
	return adapter{}, ErrNoTV
}

// usesRealDevices is false in tests, where the device paths do not exist.
func (c *Controller) usesRealDevices() bool {
	for _, d := range c.Devices {
		if strings.HasPrefix(d, "/dev/") {
			return true
		}
	}
	return false
}

func (c *Controller) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := c.Run(ctx, args...)
	if err != nil {
		return out, fmt.Errorf("cec-ctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(lastLine(out)))
	}
	return out, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// ready finds the adapter and makes sure the Pi is registered on it as a
// playback device, which is required before it can send anything.
func (c *Controller) ready(ctx context.Context) (adapter, error) {
	a, err := c.find(ctx)
	if err != nil {
		return a, err
	}
	if !a.registered {
		if _, err := c.run(ctx, "-d", a.dev, "--playback", "--osd-name", c.OSDName); err != nil {
			return a, err
		}
	}
	return a, nil
}

// Setup registers the Pi with the TV and makes it the active source, so the TV
// shows the Pi's input and sends it the remote-control keys.
func (c *Controller) Setup(ctx context.Context) error {
	a, err := c.ready(ctx)
	if err != nil {
		return err
	}
	return c.activate(ctx, a)
}

func (c *Controller) activate(ctx context.Context, a adapter) error {
	// "Image view on" wakes the TV; "active source" is a broadcast (address 15)
	// telling every device which input to show.
	if _, err := c.run(ctx, "-d", a.dev, "--to", "0", "--image-view-on"); err != nil {
		return err
	}
	_, err := c.run(ctx, "-d", a.dev, "--to", "15", "--active-source", "phys-addr="+a.phys)
	if err == nil {
		c.active.Store(true)
	}
	return err
}

// Announce registers the Pi with the TV, so that the TV's remote reaches it,
// and has a TV that is on show the Pi's input. A TV in standby is left in
// standby: this is for when the dashboard starts, which says nothing about
// anyone wanting to watch. It reports whether the TV was told to show the Pi.
func (c *Controller) Announce(ctx context.Context) (bool, error) {
	a, err := c.ready(ctx)
	if err != nil {
		return false, err
	}
	if power, _ := c.PowerStatus(ctx); power == PowerStandby {
		return false, nil
	}
	// "Active source" on its own: unlike "image view on" it wakes no TV.
	if _, err := c.run(ctx, "-d", a.dev, "--to", "15", "--active-source", "phys-addr="+a.phys); err != nil {
		return false, err
	}
	c.active.Store(true)
	return true, nil
}

// Wake switches the TV on and to the Pi's input.
func (c *Controller) Wake(ctx context.Context) error { return c.Setup(ctx) }

// Standby switches the TV off.
func (c *Controller) Standby(ctx context.Context) error {
	a, err := c.ready(ctx)
	if err != nil {
		return err
	}
	_, err = c.run(ctx, "-d", a.dev, "--to", "0", "--standby")
	return err
}

// Volume presses a volume button of the TV's remote on its behalf: "up",
// "down" or "mute". to is the device that sets the volume: 0 for the TV,
// 5 for a soundbar or receiver that has taken over the sound.
func (c *Controller) Volume(ctx context.Context, button string, to int) error {
	if button != "up" && button != "down" && button != "mute" {
		return fmt.Errorf("unknown volume button %q", button)
	}
	a, err := c.ready(ctx)
	if err != nil {
		return err
	}
	cmd := "volume-" + button
	if button == "mute" {
		cmd = "mute"
	}
	// Pressed and let go, as a finger would.
	_, err = c.run(ctx, "-d", a.dev, "--to", strconv.Itoa(to), "--user-control-pressed", "ui-cmd="+cmd, "--user-control-released")
	return err
}

// PowerStatus asks the TV whether it is on. TVs that do not answer, or answer
// with a transitional state, yield PowerUnknown.
func (c *Controller) PowerStatus(ctx context.Context) (Power, error) {
	a, err := c.ready(ctx)
	if err != nil {
		return PowerUnknown, err
	}
	out, err := c.run(ctx, "-d", a.dev, "--to", "0", "--give-device-power-status")
	if err != nil {
		return PowerUnknown, err
	}
	if m := powerRe.FindStringSubmatch(out); m != nil {
		switch m[1] {
		case "on":
			return PowerOn, nil
		case "standby":
			return PowerStandby, nil
		}
	}
	return PowerUnknown, nil
}
