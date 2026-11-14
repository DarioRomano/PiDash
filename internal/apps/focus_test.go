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

func musicManager(t *testing.T) (*Manager, *fakeDesktop, *Mock) {
	t.Helper()
	list := []config.App{
		{ID: "spotify", Name: "Spotify", URL: "https://open.spotify.com/", Background: true},
		{ID: "netflix", Name: "Netflix", URL: "https://www.netflix.com/"},
	}
	backend := &Mock{}
	m := NewManager(list, backend, quiet)
	d := &fakeDesktop{windows: map[string]bool{"pidash": true, "pidash-spotify": true, "pidash-netflix": true}, focused: "pidash"}
	m.Focus = newFocuser(d)
	return m, d, backend
}

func (d *fakeDesktop) has() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.focused
}

func settle() { time.Sleep(60 * time.Millisecond) } // the window calls made in the background

func TestAMusicAppKeepsPlayingBehindTheDashboard(t *testing.T) {
	m, d, backend := musicManager(t)
	ctx := context.Background()
	if err := m.Start(ctx, "spotify"); err != nil {
		t.Fatal(err)
	}
	settle() // its window opened and was given the keyboard
	if d.has() != "pidash-spotify" {
		t.Fatalf("after starting Spotify the keyboard is with %q", d.has())
	}

	// Back held: the dashboard comes to the front, Spotify keeps running.
	if err := m.Leave(ctx); err != nil {
		t.Fatal(err)
	}
	st := m.Status()
	if st.Running != "" || st.Background != "spotify" {
		t.Fatalf("after leaving: running %q, background %q; want the dashboard in front and Spotify behind", st.Running, st.Background)
	}
	if !backend.Active(ctx, config.App{ID: "spotify"}) {
		t.Fatal("Spotify was closed instead of being put behind the dashboard")
	}
	if d.has() != "pidash" {
		t.Errorf("the keyboard is with %q, want the dashboard", d.has())
	}
	// The remote now belongs to the dashboard: nothing gives the keyboard
	// back to Spotify, and it does not count as a website that is open.
	m.EnsureFocus()
	settle()
	if d.has() != "pidash" || m.RunningWebsite() {
		t.Errorf("Spotify took the keyboard back while behind the dashboard (keyboard: %s)", d.has())
	}

	// OK on its tile brings it back, without starting it a second time.
	if err := m.Start(ctx, "spotify"); err != nil {
		t.Fatal(err)
	}
	settle()
	st = m.Status()
	if st.Running != "spotify" || st.Background != "" || d.has() != "pidash-spotify" {
		t.Errorf("after opening it again: running %q, background %q, keyboard %q", st.Running, st.Background, d.has())
	}

	// Behind the dashboard again, then another app is opened: the music ends.
	m.Leave(ctx)
	if err := m.Start(ctx, "netflix"); err != nil {
		t.Fatal(err)
	}
	st = m.Status()
	if st.Running != "netflix" || st.Background != "" || backend.Active(ctx, config.App{ID: "spotify"}) {
		t.Errorf("after opening Netflix: %+v, Spotify still running: %v", st, backend.Active(ctx, config.App{ID: "spotify"}))
	}
	// Netflix is no music app: leaving it closes it.
	m.Leave(ctx)
	if st := m.Status(); st.Running != "" || st.Background != "" || backend.Active(ctx, config.App{ID: "netflix"}) {
		t.Errorf("after leaving Netflix: %+v", st)
	}
}

func TestStopEndsAMusicAppThatPlaysBehindTheDashboard(t *testing.T) {
	m, _, backend := musicManager(t)
	ctx := context.Background()
	m.Start(ctx, "spotify")
	m.Leave(ctx)
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if st := m.Status(); st.Background != "" || st.Running != "" || backend.Active(ctx, config.App{ID: "spotify"}) {
		t.Errorf("after Stop: %+v", st)
	}
}

func TestWithoutAWayToMoveWindowsAMusicAppIsSimplyClosed(t *testing.T) {
	m, _, backend := musicManager(t)
	m.Focus = nil // no wlrctl on this system
	ctx := context.Background()
	m.Start(ctx, "spotify")
	m.Leave(ctx)
	if st := m.Status(); st.Background != "" || backend.Active(ctx, config.App{ID: "spotify"}) {
		t.Errorf("it cannot be put behind the dashboard here, so it must be closed: %+v", st)
	}

	// The same when the dashboard's window is not there to come forward.
	m, d, backend := musicManager(t)
	delete(d.windows, "pidash")
	m.Start(ctx, "spotify")
	m.Leave(ctx)
	if st := m.Status(); st.Background != "" || backend.Active(ctx, config.App{ID: "spotify"}) {
		t.Errorf("without a dashboard window: %+v", st)
	}
}

func TestAfterARestartPiDashKnowsThatTheMusicIsBehindTheDashboard(t *testing.T) {
	// Saving settings restarts PiDash; Spotify plays on. The new PiDash finds
	// it running and has to tell whether it is in front or behind.
	for _, c := range []struct {
		focused         string
		running, behind string
	}{{"pidash", "", "spotify"}, {"pidash-spotify", "spotify", ""}} {
		m, d, backend := musicManager(t)
		backend.Start(context.Background(), config.App{ID: "spotify"})
		d.mu.Lock()
		d.focused = c.focused
		d.mu.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		go m.Run(ctx)
		settle()
		if st := m.Status(); st.Running != c.running || st.Background != c.behind {
			t.Errorf("keyboard with %s: running %q, background %q; want %q and %q", c.focused, st.Running, st.Background, c.running, c.behind)
		}
		cancel()
	}
}
