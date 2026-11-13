package input

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
	h := newHoldDetector([]uint16{keyExit, keyBack}, 60*time.Millisecond, func() { fired.Add(1) })

	// A normal short press does nothing.
	h.event("remote", keyExit, 1)
	time.Sleep(15 * time.Millisecond)
	h.event("remote", keyExit, 0)
	time.Sleep(90 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatal("a short press must not close the app")
	}

	// Held, with the auto-repeat events a remote sends while held.
	h.event("remote", keyExit, 1)
	for i := 0; i < 4; i++ {
		time.Sleep(20 * time.Millisecond)
		h.event("remote", keyExit, 2)
	}
	time.Sleep(30 * time.Millisecond)
	if fired.Load() != 1 {
		t.Fatalf("held key fired %d times, want 1", fired.Load())
	}
	h.event("remote", keyExit, 0)

	// Other keys are ignored however long they are held.
	h.event("remote", keyEnter, 1)
	time.Sleep(90 * time.Millisecond)
	if fired.Load() != 1 {
		t.Error("an unrelated key fired")
	}

	// Releasing the same key on another device does not cancel this one.
	h.event("remote", keyBack, 1)
	h.event("keyboard", keyBack, 0)
	time.Sleep(90 * time.Millisecond)
	if fired.Load() != 2 {
		t.Errorf("fired = %d, want 2", fired.Load())
	}

	// A device that disappears takes its pending hold with it.
	h.event("remote", keyBack, 1)
	h.forget("remote")
	time.Sleep(90 * time.Millisecond)
	if fired.Load() != 2 {
		t.Errorf("a removed device still fired: %d", fired.Load())
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
