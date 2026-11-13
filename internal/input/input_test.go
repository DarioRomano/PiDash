package input

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestParseKey(t *testing.T) {
	for in, want := range map[string]uint16{"KEY_EXIT": 174, "key_back": 158, " KEY_RED ": 398, "174": 174, "KEY_ESC": 1} {
		if got, err := ParseKey(in); err != nil || got != want {
			t.Errorf("ParseKey(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "EXIT", "KEY_NOPE", "0", "-3", "99999"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) should fail", bad)
		}
	}
}

func TestHoldingBackLongEnoughFires(t *testing.T) {
	var fired atomic.Int32
	h := newHoldDetector([]uint16{keyExit, keyBack}, 90*time.Millisecond, func() { fired.Add(1) })

	// A normal short press does nothing.
	h.event("remote", keyExit, 1)
	time.Sleep(15 * time.Millisecond)
	h.event("remote", keyExit, 0)
	time.Sleep(140 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatal("a short press must not close the app")
	}

	// Held, with the auto-repeat events a remote sends while held.
	h.event("remote", keyExit, 1)
	for i := 0; i < 4; i++ {
		time.Sleep(20 * time.Millisecond)
		h.event("remote", keyExit, 2)
	}
	time.Sleep(60 * time.Millisecond)
	if fired.Load() != 1 {
		t.Fatalf("held key fired %d times, want 1", fired.Load())
	}
	// Keeping it down longer does not fire again.
	time.Sleep(120 * time.Millisecond)
	if fired.Load() != 1 {
		t.Fatalf("one long hold fired %d times", fired.Load())
	}
	h.event("remote", keyExit, 0)
	time.Sleep(60 * time.Millisecond)

	// Other keys are ignored however long they are held.
	h.event("remote", keyEnter, 1)
	time.Sleep(140 * time.Millisecond)
	if fired.Load() != 1 {
		t.Error("an unrelated key fired")
	}

	// Releasing the same key on another device does not cancel this one.
	h.event("remote", keyBack, 1)
	h.event("keyboard", keyBack, 0)
	time.Sleep(140 * time.Millisecond)
	if fired.Load() != 2 {
		t.Errorf("fired = %d, want 2", fired.Load())
	}
	h.event("remote", keyBack, 0)
	time.Sleep(60 * time.Millisecond)

	// A device that disappears takes its pending hold with it.
	h.event("remote", keyBack, 1)
	h.forget("remote")
	time.Sleep(140 * time.Millisecond)
	if fired.Load() != 2 {
		t.Errorf("a removed device still fired: %d", fired.Load())
	}
}

func TestHoldReportedAsRapidPressesStillCounts(t *testing.T) {
	// Some TVs send a held button as press, release, press, release … with
	// short gaps. That must count as one hold and fire exactly once.
	var fired atomic.Int32
	h := newHoldDetector([]uint16{keyExit}, 120*time.Millisecond, func() { fired.Add(1) })
	for i := 0; i < 12; i++ {
		h.event("remote", keyExit, 1)
		time.Sleep(10 * time.Millisecond)
		h.event("remote", keyExit, 0)
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("a hold sent as rapid presses fired %d times, want 1", fired.Load())
	}
	// After a real pause the next hold works again.
	time.Sleep(80 * time.Millisecond)
	h.event("remote", keyExit, 1)
	time.Sleep(160 * time.Millisecond)
	if fired.Load() != 2 {
		t.Errorf("second hold: fired = %d, want 2", fired.Load())
	}

	// Two separate quick taps with a pause between them are not a hold.
	var taps atomic.Int32
	h2 := newHoldDetector([]uint16{keyExit}, 120*time.Millisecond, func() { taps.Add(1) })
	for i := 0; i < 2; i++ {
		h2.event("remote", keyExit, 1)
		time.Sleep(10 * time.Millisecond)
		h2.event("remote", keyExit, 0)
		time.Sleep(90 * time.Millisecond)
	}
	if taps.Load() != 0 {
		t.Errorf("two separate taps were taken for a hold")
	}
}

func TestZeroHoldActsOnThePress(t *testing.T) {
	fired := make(chan struct{}, 1)
	h := newHoldDetector([]uint16{398}, 0, func() { fired <- struct{}{} })
	h.event("remote", 398, 1)
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("a dedicated exit key should act at once")
	}
}

// remoteHarness is a Remote wired to recorders instead of a TV and apps.
type remoteHarness struct {
	*Remote
	mu         sync.Mutex
	appRunning bool
	actions    []string
	taps       []uint16
	exits      int
}

func newRemoteHarness(hold time.Duration) *remoteHarness {
	h := &remoteHarness{}
	h.Remote = &Remote{
		Actions:  DefaultRemoteActions(),
		ExitKeys: DefaultExitKeys(),
		Hold:     hold,
		Log:      quiet,
		AppRunning: func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.appRunning
		},
		OnAction: func(a string) { h.mu.Lock(); h.actions = append(h.actions, a); h.mu.Unlock() },
		OnAppExit: func() {
			h.mu.Lock()
			h.exits++
			h.appRunning = false
			h.mu.Unlock()
		},
		Tap: func(c uint16) error { h.mu.Lock(); h.taps = append(h.taps, c); h.mu.Unlock(); return nil },
	}
	h.init()
	return h
}

func (h *remoteHarness) press(code uint16) {
	h.key("cec", code, 1)
	h.key("cec", code, 0)
}

func (h *remoteHarness) snapshot() (actions []string, taps []uint16, exits int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.actions...), append([]uint16{}, h.taps...), h.exits
}

func TestRemoteOnTheDashboard(t *testing.T) {
	h := newRemoteHarness(time.Hour)
	// What a TV remote sends over HDMI-CEC for OK, Back, a number and Play.
	h.press(keyOK)
	h.press(keyExit)
	h.press(keyNumeric0 + 3)
	h.press(keyPlayCD)
	// Arrows and letters are left to the browser, which understands them.
	h.press(keyUp)
	h.press(30) // KEY_A

	actions, taps, exits := h.snapshot()
	want := []string{"select", "back", "3", "playpause"}
	if strings.Join(actions, ",") != strings.Join(want, ",") {
		t.Errorf("dashboard actions = %v, want %v", actions, want)
	}
	if len(taps) != 0 || exits != 0 {
		t.Errorf("nothing should be typed or closed on the dashboard: taps=%v exits=%d", taps, exits)
	}
}

func TestRemoteInsideAnApp(t *testing.T) {
	h := newRemoteHarness(time.Hour)
	h.appRunning = true
	h.press(keyOK)           // must reach the app as Enter
	h.press(keyExit)         // … as Escape
	h.press(keyNumeric0 + 7) // … as the 7 key
	h.press(keyEnter)        // a real Enter reaches the app by itself: nothing to add
	h.press(keyEsc)
	h.press(keyUp)

	actions, taps, _ := h.snapshot()
	wantTaps := []uint16{keyEnter, keyEsc, 8}
	if len(taps) != len(wantTaps) {
		t.Fatalf("keys passed to the app = %v, want %v", taps, wantTaps)
	}
	for i := range wantTaps {
		if taps[i] != wantTaps[i] {
			t.Errorf("tap %d = %d, want %d", i, taps[i], wantTaps[i])
		}
	}
	if len(actions) != 0 {
		t.Errorf("the dashboard must not react while an app is open: %v", actions)
	}
}

func TestHoldingBackLeavesTheAppThenTheDashboard(t *testing.T) {
	h := newRemoteHarness(80 * time.Millisecond)
	h.appRunning = true

	// Hold the remote's Back inside an app: the app closes.
	h.key("cec", keyExit, 1)
	time.Sleep(140 * time.Millisecond)
	_, _, exits := h.snapshot()
	if exits != 1 {
		t.Fatalf("holding Back in an app: exits = %d, want 1", exits)
	}
	// Still holding: it must not go on to close the dashboard as well.
	time.Sleep(140 * time.Millisecond)
	actions, _, _ := h.snapshot()
	if len(actions) != 0 {
		t.Fatalf("one hold did two things: %v", actions)
	}
	h.key("cec", keyExit, 0)
	time.Sleep(60 * time.Millisecond)

	// A keyboard's Esc held in an app closes it too.
	h.mu.Lock()
	h.appRunning = true
	h.mu.Unlock()
	h.key("keyboard", keyEsc, 1)
	time.Sleep(140 * time.Millisecond)
	h.key("keyboard", keyEsc, 0)
	if _, _, exits := h.snapshot(); exits != 2 {
		t.Errorf("holding Esc in an app: exits = %d, want 2", exits)
	}
	time.Sleep(60 * time.Millisecond)

	// Back on the dashboard, a new hold means "return to the desktop".
	h.key("cec", keyExit, 1)
	time.Sleep(140 * time.Millisecond)
	h.key("cec", keyExit, 0)
	actions, _, _ = h.snapshot()
	if len(actions) != 2 || actions[0] != "back" || actions[1] != "exit" {
		t.Errorf("dashboard actions = %v, want [back exit]", actions)
	}
}

func TestRemoteWithoutVirtualKeyboardDoesNotCrash(t *testing.T) {
	h := newRemoteHarness(time.Hour)
	h.appRunning = true
	h.Tap = func(uint16) error { return errors.New("no /dev/uinput") }
	h.press(keyOK)
	h.press(keyOK) // the warning is logged once, not per press
	h.Tap = nil
	h.press(keyOK)
}

func TestDisabledServiceRefusesEverything(t *testing.T) {
	s := NewService(false, quiet)
	if st := s.Status(); st.Enabled || st.Available || st.Typing {
		t.Errorf("status = %+v", st)
	}
	if err := s.Move(1, 1); !errors.Is(err, ErrDisabled) {
		t.Errorf("Move: %v", err)
	}
	if err := s.Click("left"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Click: %v", err)
	}
	if err := s.Type(context.Background(), "hello"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Type: %v", err)
	}
	if s.Action("up") {
		t.Error("Action must not press keys when disabled")
	}
	// Passing on the TV remote's own buttons is not a network feature, so it
	// is not refused for being "disabled"; here it can only fail for lack of
	// /dev/uinput.
	if err := s.Tap(keyEnter); errors.Is(err, ErrDisabled) {
		t.Error("Tap must not depend on the phone's mouse and keyboard being switched on")
	}
}

func TestEnabledServiceExplainsWhatIsMissing(t *testing.T) {
	// This test machine has no /dev/uinput, like a Pi before the one-time setup.
	if uinputProblem() == nil {
		t.Skip("/dev/uinput is usable here")
	}
	s := NewService(true, quiet)
	st := s.Status()
	if !st.Enabled || st.Available || st.Problem == "" {
		t.Errorf("status = %+v", st)
	}
	if err := s.Move(3, 4); err == nil {
		t.Error("Move should fail without /dev/uinput")
	}
	if s.Action("up") {
		t.Error("Action should report that the key was not sent")
	}
}

func TestTypeRejectsControlCharacters(t *testing.T) {
	s := NewService(true, quiet)
	for _, bad := range []string{"line\nbreak", "tab\tbed", "esc\x1b[2J", string(make([]byte, 501))} {
		if err := s.Type(context.Background(), bad); err == nil {
			t.Errorf("Type(%q) should be refused", bad)
		}
	}
	if err := s.Click("turbo"); err == nil {
		t.Error("unknown button should be refused")
	}
	if err := s.Key("f13"); err == nil {
		t.Error("unknown key should be refused")
	}
}
