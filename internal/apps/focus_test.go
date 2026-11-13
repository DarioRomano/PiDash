package apps

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

// fakeDesktop imitates wlrctl on a desktop with some windows open.
type fakeDesktop struct {
	mu      sync.Mutex
	windows map[string]bool // name -> exists
	focused string
	calls   []string
}

func (d *fakeDesktop) run(_ context.Context, _ []string, name string, args ...string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	line := name + " " + strings.Join(args, " ")
	d.calls = append(d.calls, line)
	if name != "wlrctl" || len(args) < 3 {
		return errors.New("unexpected: " + line)
	}
	window := strings.TrimPrefix(args[2], "app_id:")
	switch args[1] {
	case "find":
		if d.windows[window] && d.focused == window {
			return nil
		}
		return errors.New("no match")
	case "focus":
		if !d.windows[window] {
			return errors.New("no match")
		}
		d.focused = window
		return nil
	}
	return errors.New("unexpected: " + line)
}

func newFocuser(d *fakeDesktop) *Focuser {
	return &Focuser{Env: func() []string { return []string{"WAYLAND_DISPLAY=wayland-0"} }, Log: quiet, Run: d.run}
}

func TestTheOpenAppKeepsTheKeyboard(t *testing.T) {
	d := &fakeDesktop{windows: map[string]bool{KioskWindow: true, "pidash-netflix": true}, focused: KioskWindow}
	f := newFocuser(d)
	netflix := config.App{ID: "netflix", URL: "https://www.netflix.com/browse"}
	if WindowOf(netflix) != "pidash-netflix" {
		t.Fatalf("window of a website = %q", WindowOf(netflix))
	}

	// The dashboard has the keyboard although Netflix is open: hand it over.
	if !f.Ensure(context.Background(), WindowOf(netflix)) || d.focused != "pidash-netflix" {
		t.Fatalf("focused = %q after Ensure", d.focused)
	}
	// It has it already: nothing is changed, only looked at.
	before := len(d.calls)
	f.Ensure(context.Background(), WindowOf(netflix))
	if len(d.calls) != before+1 || !strings.Contains(d.calls[before], "find") {
		t.Errorf("a window that has the keyboard was focused again: %v", d.calls[before:])
	}
	// A window that is not there, and an app whose window is not known.
	if f.Ensure(context.Background(), "pidash-gone") {
		t.Error("Ensure reported a window that does not exist")
	}
	if WindowOf(config.App{ID: "retro", Command: "retroarch"}) != "" || f.Ensure(context.Background(), "") {
		t.Error("a program without a known window must be left alone")
	}
	if got := WindowOf(config.App{ID: "x", Command: "kodi", Window: "Kodi"}); got != "Kodi" {
		t.Errorf("window from the menu entry = %q", got)
	}
}

func TestAWindowThatOpensLaterGetsTheKeyboard(t *testing.T) {
	d := &fakeDesktop{windows: map[string]bool{KioskWindow: true}, focused: KioskWindow}
	f := newFocuser(d)
	go func() {
		time.Sleep(900 * time.Millisecond) // the browser takes a moment to open its window
		d.mu.Lock()
		d.windows["pidash-youtube"] = true
		d.mu.Unlock()
	}()
	f.Await(context.Background(), "pidash-youtube", 10*time.Second)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.focused != "pidash-youtube" {
		t.Errorf("focused = %q", d.focused)
	}
}

func TestButtonPressesDoNotFloodTheDesktop(t *testing.T) {
	d := &fakeDesktop{windows: map[string]bool{"pidash-netflix": true}, focused: "pidash-netflix"}
	f := newFocuser(d)
	for i := 0; i < 20; i++ {
		f.EnsureSoon("pidash-netflix")
	}
	time.Sleep(200 * time.Millisecond)
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) != 1 {
		t.Errorf("20 button presses led to %d checks, want 1", len(d.calls))
	}
	var nobody *Focuser
	nobody.EnsureSoon("x") // without a focuser nothing happens, and nothing breaks
	nobody.Await(context.Background(), "x", time.Second)
}

func TestAWindowThatHasTheKeyboardIsLeftAloneForLonger(t *testing.T) {
	d := &fakeDesktop{windows: map[string]bool{"retroarch": true}, focused: "retroarch"}
	f := newFocuser(d)
	f.EnsureSoon("retroarch")
	time.Sleep(150 * time.Millisecond)
	f.mu.Lock()
	rest := time.Until(f.next)
	f.mu.Unlock()
	if rest < restWhenFine-time.Second {
		t.Errorf("after finding the keyboard in place the next check is in %v, want about %v", rest, restWhenFine)
	}

	// A window that had lost the keyboard is looked at again soon.
	d.mu.Lock()
	d.focused = "pidash"
	d.mu.Unlock()
	f.mu.Lock()
	f.next = time.Time{}
	f.mu.Unlock()
	f.EnsureSoon("retroarch")
	time.Sleep(150 * time.Millisecond)
	f.mu.Lock()
	rest = time.Until(f.next)
	f.mu.Unlock()
	if rest > restAfterFix {
		t.Errorf("after giving the keyboard back the next check is in %v, want within %v", rest, restAfterFix)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.focused != "retroarch" {
		t.Errorf("the keyboard is with %q, want retroarch", d.focused)
	}
}
