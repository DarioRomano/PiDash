package input

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Remote listens to the TV remote (and keyboards) directly, below the desktop,
// and makes its buttons work everywhere:
//
//   - On the dashboard, buttons a browser does not understand, such as the
//     remote's OK and Back, are turned into dashboard actions.
//   - Inside an app, those buttons are passed on as the keyboard keys apps
//     expect: OK as Enter, Back as Escape.
//   - Holding Back (or Esc) leaves the app; on the dashboard it returns to
//     the desktop.
//
// It reads the input devices themselves, so it keeps working while an app,
// even one running outside the desktop, has the screen.
type Remote struct {
	// Actions maps key codes to dashboard actions.
	Actions map[uint16]string
	// ExitKeys leave an app when held for Hold.
	ExitKeys []uint16
	Hold     time.Duration
	// AppRunning reports whether an app has the screen.
	AppRunning func() bool
	// OnAction delivers an action to the dashboard.
	OnAction func(action string)
	// OnAppExit closes the running app.
	OnAppExit func()
	// OnPadAction, if set, gets the actions of game controllers instead of
	// OnAction. A press on the TV's remote proves the TV is on; a press on a
	// controller proves nothing of the kind.
	OnPadAction func(action string)
	// OnPadWake is called when a game controller is switched on, that is,
	// when it appears after start-up, and when its logo button is pressed:
	// the two things people do to say "I want to play".
	OnPadWake func(name string)
	// OnAppKey is called for every button pressed while an app is open, so
	// that the app's window can be given the keyboard if it lost it.
	OnAppKey func()
	// Tap presses a key in the running app; nil when that is not possible.
	Tap func(code uint16) error
	Log *slog.Logger
	// Dir is the directory with the event devices; /dev/input by default.
	Dir string

	hold        *holdDetector
	tapWarnOnce sync.Once

	// For the status page: what is being listened to and what was heard last.
	seenMu  sync.Mutex
	devices map[string]string // path → name
	pads    map[string]bool   // path → is a game controller
	last    Heard
}

// Heard is a button press as the backend read it from an input device.
type Heard struct {
	// Device is the name of the remote, keyboard or controller.
	Device string `json:"device"`
	// Key is the kernel's name for the button, such as KEY_OK.
	Key string `json:"key"`
	// Action is what the dashboard does on it; empty when nothing.
	Action string    `json:"action,omitempty"`
	At     time.Time `json:"at"`
}

// Devices names the remotes, keyboards and controllers being listened to.
func (r *Remote) Devices() []string {
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	out := make([]string, 0, len(r.devices))
	for _, name := range r.devices {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Last returns the most recent button press, if there was one.
func (r *Remote) Last() (Heard, bool) {
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	return r.last, !r.last.At.IsZero()
}

func (r *Remote) listening(path, name string, pad, on bool) {
	r.seenMu.Lock()
	defer r.seenMu.Unlock()
	if r.devices == nil {
		r.devices, r.pads = map[string]string{}, map[string]bool{}
	}
	if on {
		r.devices[path], r.pads[path] = name, pad
	} else {
		delete(r.devices, path)
		delete(r.pads, path)
	}
}

func (r *Remote) init() {
	r.hold = newHoldDetector(r.ExitKeys, r.Hold, func() {
		if r.AppRunning() {
			r.OnAppExit()
			return
		}
		// On the dashboard a long Back means "return to the desktop".
		r.OnAction("exit")
	})
}

// wanted lists every key the remote component cares about, which decides
// which input devices are worth opening.
func (r *Remote) wanted() []uint16 {
	seen := map[uint16]bool{}
	var out []uint16
	add := func(c uint16) {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for c := range r.Actions {
		add(c)
	}
	for c := range appKeys {
		add(c)
	}
	for _, c := range r.ExitKeys {
		add(c)
	}
	return out
}

// key handles one key event from a device: value 1 is press, 0 release, 2 repeat.
func (r *Remote) key(device string, code uint16, value int32) {
	r.hold.event(device, code, value)
	if value != 1 {
		return
	}
	r.seenMu.Lock()
	name, pad := r.devices[device], r.pads[device]
	if name == "" {
		name = filepath.Base(device)
	}
	r.last = Heard{Device: name, Key: KeyName(code), Action: r.Actions[code], At: time.Now()}
	r.seenMu.Unlock()
	if pad && code == btnMode && r.OnPadWake != nil {
		r.OnPadWake(name)
	}
	if r.AppRunning() {
		if r.OnAppKey != nil {
			r.OnAppKey()
		}
		to, ok := appKeys[code]
		if !ok || r.Tap == nil {
			return
		}
		if err := r.Tap(to); err != nil {
			r.tapWarnOnce.Do(func() {
				r.Log.Warn("cannot pass the TV remote's buttons on to apps", "err", err)
			})
		}
		return
	}
	if action, ok := r.Actions[code]; ok {
		if pad && r.OnPadAction != nil {
			r.OnPadAction(action)
		} else {
			r.OnAction(action)
		}
	}
}

// Run watches until ctx ends, picking up devices as they appear.
func (r *Remote) Run(ctx context.Context) {
	r.init()
	dir := r.Dir
	if dir == "" {
		dir = "/dev/input"
	}
	wanted := r.wanted()

	var mu sync.Mutex
	open := map[string]*os.File{}
	skipped := map[string]bool{}
	warned := false
	// Controllers that are there when PiDash starts were not just switched on.
	started := false

	scan := func() {
		paths, _ := filepath.Glob(filepath.Join(dir, "event*"))
		for _, p := range paths {
			mu.Lock()
			_, have := open[p]
			skip := skipped[p]
			mu.Unlock()
			if have || skip {
				continue
			}
			f, err := openRemote(p, wanted)
			if err != nil {
				if errors.Is(err, os.ErrPermission) && !warned {
					warned = true
					r.Log.Warn("cannot read input devices, so the TV remote's OK and Back buttons will not work; add your user to the 'input' group", "err", err)
				}
				if errors.Is(err, errNotARemote) {
					mu.Lock()
					skipped[p] = true
					mu.Unlock()
				}
				continue
			}
			mu.Lock()
			open[p] = f
			mu.Unlock()
			name := deviceLabel(f)
			if name == "" {
				name = filepath.Base(p)
			}
			axes, isPad := padAxes(f)
			r.listening(p, name, isPad, true)
			r.Log.Debug("listening to input device", "path", p, "name", name, "controller", isPad)
			if isPad && started && r.OnPadWake != nil {
				r.Log.Info("a game controller was switched on", "name", name)
				r.OnPadWake(name)
			}
			go func(p string, f *os.File) {
				// A game controller's stick and direction pad become arrow keys.
				pad := newStick(axes)
				readEvents(f, func(code uint16, value int32) { r.key(p, code, value) }, func(axis uint16, value int32) {
					release, press := pad.move(axis, value)
					if release != 0 {
						r.key(p, release, 0)
					}
					if press != 0 {
						r.key(p, press, 1)
					}
				})
				f.Close()
				r.listening(p, "", false, false)
				r.hold.forget(p)
				mu.Lock()
				delete(open, p)
				mu.Unlock()
			}(p, f)
		}
		// Device numbers are reused after unplugging: forget skips for paths that are gone.
		mu.Lock()
		for p := range skipped {
			if _, err := os.Stat(p); err != nil {
				delete(skipped, p)
			}
		}
		mu.Unlock()
	}

	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		scan()
		started = true
		select {
		case <-ctx.Done():
			mu.Lock()
			for _, f := range open {
				f.Close() // unblocks the readers
			}
			mu.Unlock()
			return
		case <-t.C:
		}
	}
}
