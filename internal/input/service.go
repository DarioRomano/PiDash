package input

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/DarioRomano/PiDash/internal/apps"
)

var errNotARemote = errors.New("not a remote control")

// ErrDisabled is returned when the phone's mouse and keyboard are switched off
// in the config.
var ErrDisabled = errors.New("the phone's mouse and keyboard are switched off (set remote.mouse_keyboard = true)")

// Status tells the phone remote what it can offer.
type Status struct {
	// Enabled mirrors remote.mouse_keyboard in the config.
	Enabled bool `json:"enabled"`
	// Available is true when the virtual mouse can actually be created.
	Available bool `json:"available"`
	// Problem says what is missing when it is enabled but not available.
	Problem string `json:"problem,omitempty"`
	// Typing is true when text can be typed into apps.
	Typing bool `json:"typing"`
	// PINRequired asks the phone to send the PIN with input requests.
	PINRequired bool `json:"pinRequired"`
}

// Service is the phone's virtual mouse and keyboard.
type Service struct {
	enabled bool
	log     *slog.Logger

	mu  sync.Mutex
	dev *virtualDevice
}

// NewService returns the phone input service. When enabled is false every
// method refuses to act.
func NewService(enabled bool, log *slog.Logger) *Service {
	return &Service{enabled: enabled, log: log}
}

// Status reports whether the phone can act as mouse and keyboard.
func (s *Service) Status() Status {
	st := Status{Enabled: s.enabled}
	if !s.enabled {
		return st
	}
	if err := uinputProblem(); err != nil {
		st.Problem = err.Error()
		return st
	}
	st.Available = true
	st.Typing = typeTool() != ""
	return st
}

// device is for requests from the phone, which need the feature switched on.
func (s *Service) device() (*virtualDevice, error) {
	if !s.enabled {
		return nil, ErrDisabled
	}
	return s.open()
}

// Tap presses and releases one key on the virtual keyboard. It is used to pass
// the TV remote's buttons on to an app, which works whether or not the phone's
// mouse and keyboard are switched on: the key comes from the remote in the
// room, not from the network.
func (s *Service) Tap(code uint16) error {
	d, err := s.open()
	if err != nil {
		return err
	}
	if err := d.key(code, true); err != nil {
		return err
	}
	return d.key(code, false)
}

// KeyboardProblem says why the virtual keyboard cannot be created, or "".
func (s *Service) KeyboardProblem() string {
	if err := uinputProblem(); err != nil {
		return err.Error()
	}
	return ""
}

// open creates the virtual mouse and keyboard on first use.
func (s *Service) open() (*virtualDevice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dev == nil {
		d, err := openVirtualDevice()
		if err != nil {
			return nil, err
		}
		s.dev = d
		s.log.Info("phone remote: virtual mouse and keyboard ready")
	}
	return s.dev, nil
}

// Close removes the virtual device.
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dev != nil {
		s.dev.close()
		s.dev = nil
	}
}

// Move moves the pointer by a number of pixels.
func (s *Service) Move(dx, dy int) error {
	d, err := s.device()
	if err != nil {
		return err
	}
	return d.move(clamp(dx, 400), clamp(dy, 400))
}

// Scroll turns the wheel by a number of notches; positive is up / right.
func (s *Service) Scroll(dx, dy int) error {
	d, err := s.device()
	if err != nil {
		return err
	}
	return d.scroll(clamp(dx, 20), clamp(dy, 20))
}

func clamp(v, limit int) int {
	return max(-limit, min(limit, v))
}

// Click presses and releases a mouse button: "left" or "right".
func (s *Service) Click(button string) error {
	code, ok := map[string]uint16{"left": btnLeft, "right": btnRight, "middle": btnMiddle}[button]
	if !ok {
		return fmt.Errorf("unknown button %q", button)
	}
	return s.tap(code)
}

// Button presses or releases a mouse button, for dragging.
func (s *Service) Button(button string, down bool) error {
	code, ok := map[string]uint16{"left": btnLeft, "right": btnRight}[button]
	if !ok {
		return fmt.Errorf("unknown button %q", button)
	}
	d, err := s.device()
	if err != nil {
		return err
	}
	return d.key(code, down)
}

// Key presses one of the named keys next to the touchpad, e.g. "enter".
func (s *Service) Key(name string) error {
	code, ok := namedKeys[name]
	if !ok {
		return fmt.Errorf("unknown key %q", name)
	}
	return s.tap(code)
}

// Action presses the key for a dashboard action (up, select, back, …), so the
// phone's direction pad works in whatever app has the screen. It reports
// false when the action has no key or the virtual keyboard is unavailable.
func (s *Service) Action(action string) bool {
	code, ok := actionKeys[action]
	if !ok || !s.enabled {
		return false
	}
	if err := s.tap(code); err != nil {
		s.log.Debug("phone remote: key not sent", "action", action, "err", err)
		return false
	}
	return true
}

func (s *Service) tap(code uint16) error {
	d, err := s.device()
	if err != nil {
		return err
	}
	if err := d.key(code, true); err != nil {
		return err
	}
	return d.key(code, false)
}

// typeTool returns the program used to type text into the desktop: wtype on
// Wayland, xdotool on X11. Typing goes through these rather than the virtual
// keyboard so that it produces the right characters whatever keyboard layout
// the desktop is set to.
func typeTool() string {
	env := apps.DesktopEnv(os.Environ())
	wayland := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "WAYLAND_DISPLAY=") && kv != "WAYLAND_DISPLAY=" {
			wayland = true
		}
	}
	name := "xdotool"
	if wayland {
		name = "wtype"
	}
	if _, err := exec.LookPath(name); err != nil {
		return ""
	}
	return name
}

// Type types text into the app that has the keyboard focus.
func (s *Service) Type(ctx context.Context, text string) error {
	if !s.enabled {
		return ErrDisabled
	}
	if text == "" {
		return nil
	}
	if len(text) > 500 {
		return errors.New("text is too long")
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return errors.New("text must not contain line breaks or control characters")
		}
	}
	tool := typeTool()
	var cmd *exec.Cmd
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	switch tool {
	case "wtype":
		cmd = exec.CommandContext(ctx, "wtype", "--", text)
	case "xdotool":
		cmd = exec.CommandContext(ctx, "xdotool", "type", "--clearmodifiers", "--delay", "12", "--", text)
	default:
		return errors.New("typing needs the wtype package (or xdotool on X11): run the installer with --phone-input")
	}
	cmd.Env = apps.DesktopEnv(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w: %s", tool, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// readEvents reads key events from a device until it fails or is closed.
// onAxis, if not nil, gets the positions of sticks and direction pads.
func readEvents(r io.Reader, onKey func(code uint16, value int32), onAxis func(axis uint16, value int32)) {
	buf := make([]byte, eventSize*32)
	for {
		n, err := r.Read(buf)
		for off := 0; off+eventSize <= n; off += eventSize {
			switch typ, code, value := decodeEvent(buf[off : off+eventSize]); {
			case typ == evKey:
				onKey(code, value)
			case typ == evAbs && onAxis != nil:
				onAxis(code, value)
			}
		}
		if err != nil {
			return
		}
	}
}
