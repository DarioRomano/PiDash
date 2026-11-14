package apps

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

// KioskWindow is the name of the dashboard's own browser window.
const KioskWindow = "pidash"

// WindowOf returns the name by which an app's window can be found, or ""
// when it is not known. Websites are opened in a browser window PiDash names
// itself; a program's window is known when its menu entry says so.
func WindowOf(app config.App) string {
	switch {
	case app.Window != "":
		return app.Window
	case app.URL != "":
		return "pidash-" + app.ID
	}
	return ""
}

// Focuser gives a window the keyboard.
//
// Keys from a TV remote or a keyboard go to the one window that has the
// keyboard. A window normally gets it when it opens, but it can lose it:
// another window opening, or the TV being switched off and on, which
// rearranges the screen. An app without the keyboard looks as if the remote
// had stopped working. Giving it back is all that is needed.
type Focuser struct {
	// Env supplies the desktop session's environment (WAYLAND_DISPLAY, …).
	Env func() []string
	Log *slog.Logger
	// Run executes a tool; nil means the real one. For tests.
	Run func(ctx context.Context, env []string, name string, args ...string) error

	mu   sync.Mutex
	next time.Time // EnsureSoon does nothing before this
}

// How long EnsureSoon rests after a check. A window that had the keyboard is
// unlikely to lose it while buttons keep being pressed, and a game played
// with a controller presses them all the time: it is left alone for longer.
const (
	restAfterFix = 1500 * time.Millisecond
	restWhenFine = 8 * time.Second
)

func (f *Focuser) run(ctx context.Context, name string, args ...string) error {
	env := os.Environ()
	if f.Env != nil {
		env = f.Env()
	}
	if f.Run != nil {
		return f.Run(ctx, env, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	return cmd.Run()
}

// tool reports which helper fits the session: wlrctl on a Wayland desktop
// such as Raspberry Pi OS's, xdotool on X11.
func (f *Focuser) tool() string {
	env := os.Environ()
	if f.Env != nil {
		env = f.Env()
	}
	wayland := false
	for _, kv := range env {
		if strings.HasPrefix(kv, "WAYLAND_DISPLAY=") && len(kv) > len("WAYLAND_DISPLAY=") {
			wayland = true
		}
	}
	name := "xdotool"
	if wayland {
		name = "wlrctl"
	}
	if f.Run == nil {
		if _, err := exec.LookPath(name); err != nil {
			return ""
		}
	}
	return name
}

// Available reports whether windows can be given the keyboard here.
func (f *Focuser) Available() bool { return f != nil && f.tool() != "" }

// Focused reports whether the named window has the keyboard right now.
func (f *Focuser) Focused(ctx context.Context, window string) bool {
	if f == nil || window == "" || f.tool() != "wlrctl" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return f.run(ctx, "wlrctl", "toplevel", "find", "app_id:"+window, "state:focused") == nil
}

// Ensure gives the named window the keyboard unless it has it already. It
// reports whether the window exists.
func (f *Focuser) Ensure(ctx context.Context, window string) bool {
	exists, _ := f.ensure(ctx, window)
	return exists
}

// ensure also reports whether the window had the keyboard already.
func (f *Focuser) ensure(ctx context.Context, window string) (exists, hadIt bool) {
	if f == nil || window == "" {
		return false, false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	switch f.tool() {
	case "wlrctl":
		if f.run(ctx, "wlrctl", "toplevel", "find", "app_id:"+window, "state:focused") == nil {
			return true, true
		}
		if err := f.run(ctx, "wlrctl", "toplevel", "focus", "app_id:"+window); err != nil {
			return false, false
		}
		f.Log.Debug("gave the keyboard to a window", "window", window)
		return true, false
	case "xdotool":
		return f.run(ctx, "xdotool", "search", "--limit", "1", "--class", "^"+window+"$", "windowactivate") == nil, false
	}
	return false, false
}

// Await waits for a window that is about to open, for up to wait, and gives
// it the keyboard.
func (f *Focuser) Await(ctx context.Context, window string, wait time.Duration) {
	if f == nil || window == "" || f.tool() == "" {
		return
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if f.Ensure(ctx, window) {
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(700 * time.Millisecond):
		}
	}
}

// EnsureSoon is Ensure for use on every key press: it does its work in the
// background and at most once in a while.
func (f *Focuser) EnsureSoon(window string) {
	if f == nil || window == "" {
		return
	}
	f.mu.Lock()
	if time.Now().Before(f.next) {
		f.mu.Unlock()
		return
	}
	f.next = time.Now().Add(restAfterFix)
	f.mu.Unlock()
	go func() {
		if _, hadIt := f.ensure(context.Background(), window); hadIt {
			f.mu.Lock()
			f.next = time.Now().Add(restWhenFine)
			f.mu.Unlock()
		}
	}()
}

// PretendFocuser returns a focuser for development (-mock): every window
// exists and takes the keyboard when asked.
func PretendFocuser(log *slog.Logger) *Focuser {
	var mu sync.Mutex
	focused := ""
	return &Focuser{Log: log, Env: func() []string { return []string{"WAYLAND_DISPLAY=pretend"} },
		Run: func(_ context.Context, _ []string, _ string, args ...string) error {
			mu.Lock()
			defer mu.Unlock()
			if len(args) < 3 {
				return nil
			}
			window := strings.TrimPrefix(args[2], "app_id:")
			if args[1] == "find" && focused != window {
				return errors.New("not focused")
			}
			focused = window
			return nil
		}}
}
