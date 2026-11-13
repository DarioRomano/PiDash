package input

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
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
	// Tap presses a key in the running app; nil when that is not possible.
	Tap func(code uint16) error
	Log *slog.Logger
	// Dir is the directory with the event devices; /dev/input by default.
	Dir string

	hold        *holdDetector
	tapWarnOnce sync.Once
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
	if r.AppRunning() {
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
		r.OnAction(action)
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
			r.Log.Debug("listening to input device", "path", p)
			go func(p string, f *os.File) {
				readEvents(f, func(code uint16, value int32) { r.key(p, code, value) })
				f.Close()
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
