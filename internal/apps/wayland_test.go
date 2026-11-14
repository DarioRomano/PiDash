package apps

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWaylandSocket(t *testing.T) {
	for _, c := range []struct {
		env  []string
		want string
	}{
		{[]string{"XDG_RUNTIME_DIR=/run/user/1000", "WAYLAND_DISPLAY=wayland-0"}, "/run/user/1000/wayland-0"},
		{[]string{"WAYLAND_DISPLAY=/tmp/desk/wl"}, "/tmp/desk/wl"},
		{[]string{"XDG_RUNTIME_DIR=/run/user/1000", "WAYLAND_DISPLAY="}, ""},
		{[]string{"WAYLAND_DISPLAY=wayland-0"}, ""},
		{nil, ""},
	} {
		got, ok := waylandSocket(c.env)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%v: %q, %v; want %q", c.env, got, ok, c.want)
		}
	}
}

func TestWaylandTexts(t *testing.T) {
	for _, s := range []string{"", "a", "abc", "abcd", "zwlr_foreign_toplevel_manager_v1"} {
		raw := wlString(s)
		if len(raw)%4 != 0 {
			t.Errorf("%q takes %d bytes, not a multiple of four", s, len(raw))
		}
		got, rest, ok := readString(append(raw, 1, 2, 3, 4))
		if !ok || got != s || len(rest) != 4 {
			t.Errorf("%q came back as %q, %d bytes left, ok=%v", s, got, len(rest), ok)
		}
	}
	// Nonsense is refused rather than read past its end.
	for _, bad := range [][]byte{nil, {1, 0}, wlUint(0), wlUint(400), append(wlUint(9), 'a', 'b', 0, 0)} {
		if _, _, ok := readString(bad); ok {
			t.Errorf("%v was read as a text", bad)
		}
	}
}

// On a real desktop: run with PIDASH_TEST_WINDOWS=a,b naming two open
// windows, in a session whose WAYLAND_DISPLAY is that desktop.
func TestOnARealWaylandDesktop(t *testing.T) {
	names := strings.Split(os.Getenv("PIDASH_TEST_WINDOWS"), ",")
	if len(names) != 2 {
		t.Skip("needs a Wayland desktop with two windows: set PIDASH_TEST_WINDOWS=first,second")
	}
	ctx := context.Background()
	f := &Focuser{Log: quiet}
	if !f.Available() {
		t.Fatal("this desktop should let windows be chosen")
	}
	for round := 0; round < 3; round++ {
		for i, name := range names {
			other := names[1-i]
			if !f.Ensure(ctx, name) {
				t.Fatalf("no window called %s", name)
			}
			deadline := time.Now().Add(2 * time.Second)
			for !f.Focused(ctx, name) && time.Now().Before(deadline) {
				time.Sleep(50 * time.Millisecond)
			}
			if !f.Focused(ctx, name) || f.Focused(ctx, other) {
				t.Fatalf("round %d: after giving %s the keyboard: %s has it: %v, %s has it: %v", round, name, name, f.Focused(ctx, name), other, f.Focused(ctx, other))
			}
			if exists, hadIt := f.ensure(ctx, name); !exists || !hadIt {
				t.Errorf("a window that has the keyboard: exists=%v hadIt=%v", exists, hadIt)
			}
		}
	}
	if f.Ensure(ctx, "no-such-window") || f.Focused(ctx, "no-such-window") {
		t.Error("a window that is not there was found")
	}
}

// fakeCompositor is a desktop as far as wlWindow can tell: it offers a seat
// and, unless told otherwise, the list of windows.
type fakeCompositor struct {
	path      string
	windows   []string // app ids
	activated int      // which window has the keyboard; -1 for none
	noManager bool
	asked     chan int // the windows told to take the keyboard
}

func startFakeCompositor(t *testing.T, windows []string, activated int, noManager bool) *fakeCompositor {
	t.Helper()
	f := &fakeCompositor{path: filepath.Join(t.TempDir(), "wayland-9"), windows: windows, activated: activated, noManager: noManager, asked: make(chan int, 8)}
	l, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeCompositor) serve(c net.Conn) {
	defer c.Close()
	w := &wlConn{c: c}
	const handles = 0xff000000
	var registry uint32
	for {
		m, err := w.read()
		if err != nil {
			return
		}
		switch {
		case m.object == wlDisplay && m.opcode == 1: // get_registry
			registry = wlOrder.Uint32(m.body)
			_ = w.send(registry, 0, wlUint(1), wlString("wl_compositor"), wlUint(5))
			_ = w.send(registry, 0, wlUint(2), wlString(wlSeatName), wlUint(8))
			if !f.noManager {
				_ = w.send(registry, 0, wlUint(3), wlString(wlManagerName), wlUint(3))
			}
		case m.object == wlDisplay && m.opcode == 0: // sync
			_ = w.send(wlOrder.Uint32(m.body), 0, wlUint(1))
		case m.object == registry && m.opcode == 0: // bind
			iface, rest, _ := readString(m.body[4:])
			id := wlOrder.Uint32(rest[4:])
			if iface == wlSeatName {
				_ = w.send(id, 0, wlUint(3)) // capabilities: nothing wlWindow cares about
			}
			if iface != wlManagerName {
				continue
			}
			for i, app := range f.windows {
				h := uint32(handles + i)
				_ = w.send(id, 0, wlUint(h))
				_ = w.send(h, 0, wlString("Window "+app)) // title
				_ = w.send(h, 1, wlString(app))
				state := wlUint(4)
				state = append(state, wlUint(3)...) // fullscreen
				if i == f.activated {
					state = append(wlUint(8), append(wlUint(3), wlUint(wlStateActivated)...)...)
				}
				_ = w.send(h, 4, state)
				_ = w.send(h, 5) // done
			}
		case m.object >= handles && m.opcode == 4: // activate
			f.asked <- int(m.object - handles)
		}
	}
}

func TestWindowsOnAFakeDesktop(t *testing.T) {
	ctx := context.Background()
	desk := startFakeCompositor(t, []string{"pidash", "pidash-netflix", "foot"}, 0, false)
	env := []string{"WAYLAND_DISPLAY=" + desk.path}

	// Netflix is open but the dashboard has the keyboard: Netflix is told to take it.
	exists, hadIt, err := wlWindow(ctx, env, "pidash-netflix", true)
	if err != nil || !exists || hadIt {
		t.Fatalf("exists=%v hadIt=%v err=%v", exists, hadIt, err)
	}
	select {
	case got := <-desk.asked:
		if got != 1 {
			t.Errorf("window %d was told to take the keyboard, want 1", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no window was told to take the keyboard")
	}

	// The one that has it is left alone; looking changes nothing; and a
	// window that is not there is not found.
	if exists, hadIt, err := wlWindow(ctx, env, "pidash", true); err != nil || !exists || !hadIt {
		t.Errorf("the dashboard: exists=%v hadIt=%v err=%v", exists, hadIt, err)
	}
	if exists, hadIt, err := wlWindow(ctx, env, "foot", false); err != nil || !exists || hadIt {
		t.Errorf("only looking: exists=%v hadIt=%v err=%v", exists, hadIt, err)
	}
	if exists, _, err := wlWindow(ctx, env, "pidash-n", true); err != nil || exists {
		t.Errorf("a window that is not there: exists=%v err=%v", exists, err)
	}
	select {
	case got := <-desk.asked:
		t.Errorf("window %d was told to take the keyboard for no reason", got)
	case <-time.After(100 * time.Millisecond):
	}

	f := &Focuser{Log: quiet, Env: func() []string { return env }}
	if !f.Available() || !f.Focused(ctx, "pidash") || f.Focused(ctx, "foot") || !f.Ensure(ctx, "foot") {
		t.Error("the focuser does not see the desktop as it is")
	}

	// A desktop that does not let programs choose windows says so, and
	// nothing is promised on the status page.
	plain := startFakeCompositor(t, nil, -1, true)
	plainEnv := []string{"WAYLAND_DISPLAY=" + plain.path}
	if _, _, err := wlWindow(ctx, plainEnv, "pidash", true); !errors.Is(err, errNoWindowControl) {
		t.Errorf("err = %v, want errNoWindowControl", err)
	}
	if (&Focuser{Log: quiet, Env: func() []string { return plainEnv }}).Available() {
		t.Error("available on a desktop without window control")
	}
	// No desktop at all: an error, soon.
	if _, _, err := wlWindow(ctx, []string{"WAYLAND_DISPLAY=" + filepath.Join(t.TempDir(), "none")}, "pidash", true); err == nil {
		t.Error("no desktop, but no error")
	}
}
