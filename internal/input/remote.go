package input

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	// OnRepeat delivers a direction again and again while its button is held
	// on the dashboard, so that a long list can be run through. Nil means
	// held buttons do nothing more.
	OnRepeat func(action string)
	// PadAsKeys reports whether the open app wants a game controller's
	// buttons as keyboard keys: true for websites, which know arrows and
	// Enter but no controllers. Games read the controller themselves.
	PadAsKeys func() bool
	// RepeatAfter and RepeatEvery set how a held direction repeats; zero
	// means the usual 650 and 140 ms.
	RepeatAfter, RepeatEvery time.Duration
	// OnVolume makes the TV louder (+1) or quieter (-1). It is called when a
	// direction is pressed on a game controller while its logo button is
	// held, anywhere: on the dashboard, in a website, in a game. Nil
	// switches this off.
	OnVolume func(step int)
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

	// A held direction, per device: stop ends its repeating.
	repeatMu sync.Mutex
	repeats  map[string]func()

	// For the status page: what is being listened to and what was heard last.
	seenMu  sync.Mutex
	devices map[string]string // path → name
	pads    map[string]bool   // path → is a game controller
	logo    map[string]bool   // path → the controller's logo button is down
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
	if code == btnMode && value != 2 {
		r.seenMu.Lock()
		if r.logo == nil {
			r.logo = map[string]bool{}
		}
		r.logo[device] = value == 1
		r.seenMu.Unlock()
	}
	if value != 1 {
		if value == 0 {
			r.stopRepeat(device)
		}
		return
	}
	r.stopRepeat(device) // another button: the held one no longer counts
	r.seenMu.Lock()
	name, pad, logo := r.devices[device], r.pads[device], r.logo[device]
	if name == "" {
		name = filepath.Base(device)
	}
	r.last = Heard{Device: name, Key: KeyName(code), Action: r.Actions[code], At: time.Now()}
	r.seenMu.Unlock()

	// Logo button held, and up or down: the TV's volume. The logo button was
	// not being held to leave the app after all.
	if step := volumeStep(code); pad && logo && step != 0 && r.OnVolume != nil {
		r.hold.defuse(device, btnMode)
		r.OnVolume(step)
		r.startRepeat(device, func() { r.OnVolume(step) })
		return
	}
	if pad && code == btnMode && r.OnPadWake != nil {
		r.OnPadWake(name)
	}
	r.press(code, pad, false)
	// A direction that is held keeps going: on the dashboard, and from a
	// controller inside a website. (A keyboard's or remote's own arrows
	// repeat inside apps by themselves; they never pass through here.)
	if isArrowAction(r.Actions[code]) || (pad && isArrow(padKeys[code])) {
		r.startRepeat(device, func() { r.press(code, pad, true) })
	}
}

// press acts on a button: once when it is pressed, and for a held direction
// again each time it repeats.
func (r *Remote) press(code uint16, pad, repeat bool) {
	if r.AppRunning() {
		if !repeat && r.OnAppKey != nil {
			r.OnAppKey()
		}
		to, ok := appKeys[code]
		if pad {
			to, ok = padKeys[code]
			ok = ok && r.PadAsKeys != nil && r.PadAsKeys()
		}
		if !ok || r.Tap == nil || (repeat && !(pad && isArrow(to))) {
			return
		}
		if err := r.Tap(to); err != nil {
			r.tapWarnOnce.Do(func() {
				r.Log.Warn("cannot pass the TV remote's buttons on to apps", "err", err)
			})
		}
		return
	}
	action, ok := r.Actions[code]
	switch {
	case !ok:
	case repeat:
		if isArrowAction(action) && r.OnRepeat != nil {
			r.OnRepeat(action)
		}
	case pad && r.OnPadAction != nil:
		r.OnPadAction(action)
	default:
		r.OnAction(action)
	}
}

// startRepeat calls fire again and again while a button of device is held:
// first after RepeatAfter, then every RepeatEvery.
//
// The first wait is longer than it would need to be for a keyboard. Some TVs
// never say that a button of their remote was let go; Linux then assumes so
// 550 ms after the press. Repeating sooner than that would turn every single
// press on such a TV into two.
func (r *Remote) startRepeat(device string, fire func()) {
	after, every := r.RepeatAfter, r.RepeatEvery
	if after <= 0 {
		after = 650 * time.Millisecond
	}
	if every <= 0 {
		every = 140 * time.Millisecond
	}
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	r.repeatMu.Lock()
	if r.repeats == nil {
		r.repeats = map[string]func(){}
	}
	r.repeats[device] = stop
	r.repeatMu.Unlock()
	go func() {
		wait := time.NewTimer(after)
		defer wait.Stop()
		// Nobody holds a button for half a minute; a release that was lost
		// (a controller that went out of range) must not repeat for ever.
		for i := 0; i < 200; i++ {
			select {
			case <-done:
				return
			case <-wait.C:
			}
			fire()
			wait.Reset(every)
		}
	}()
}

func (r *Remote) stopRepeat(device string) {
	r.repeatMu.Lock()
	stop := r.repeats[device]
	delete(r.repeats, device)
	r.repeatMu.Unlock()
	if stop != nil {
		stop()
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
			// A controller made by a program (a game stream's stand-in, a
			// remote desktop's) is not somebody picking one up.
			made := dir == "/dev/input" && madeByAProgram("/sys/class/input", p)
			if isPad && started && !made && r.OnPadWake != nil {
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
				r.stopRepeat(p)
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

// madeByAProgram reports whether an input device was created by a program
// (through uinput) rather than plugged in or connected over Bluetooth. The
// kernel files those under "virtual/input"; a Bluetooth device is "virtual"
// too, but hangs off the Bluetooth service's own node.
func madeByAProgram(sysInput, device string) bool {
	real, err := filepath.EvalSymlinks(filepath.Join(sysInput, filepath.Base(device)))
	return err == nil && strings.Contains(real, "/devices/virtual/input/")
}
