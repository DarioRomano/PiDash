package tv

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/cec"
)

func TestTheTVIsSwitchedOffWhenNobodyPressesAnything(t *testing.T) {
	fake := &fakeTV{power: cec.PowerOn}
	watching, music := false, false
	var told []Status
	s := New(Options{
		TV: fake, IdleOff: 30 * time.Minute, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		AppRunning: func() bool { return watching }, Busy: func() bool { return music },
	})
	s.OnChange(func(st Status) { told = append(told, st) })
	ctx := context.Background()
	start := at(14, 20, 0)
	clock := start
	step := func(d time.Duration) {
		clock = clock.Add(d)
		s.Step(ctx, clock)
	}
	step(0)

	// Twenty-eight minutes of nothing: still on, and nothing said.
	step(28 * time.Minute)
	if fake.standbys != 0 || s.Status().SleepAt != nil {
		t.Fatalf("after 28 minutes: standbys %d, status %+v", fake.standbys, s.Status())
	}
	// A minute before the time is up the screen is told, through the sleep timer.
	step(90 * time.Second)
	if st := s.Status(); st.SleepAt == nil || st.SleepMinutes != 1 || len(told) == 0 {
		t.Fatalf("a minute's notice was not given: %+v", st)
	}
	// Somebody presses a button: the TV stays on, and the time starts again.
	s.NoteActivity()
	if s.Status().SleepAt != nil {
		t.Fatal("a button should take the notice back")
	}
	step(2 * time.Minute)
	step(20 * time.Minute)
	if fake.standbys != 0 || s.Status().SleepAt != nil {
		t.Fatalf("20 minutes after a button: standbys %d, status %+v", fake.standbys, s.Status())
	}
	// Nobody does: notice, then off.
	step(9 * time.Minute)
	if s.Status().SleepAt == nil {
		t.Fatal("no notice the second time")
	}
	step(61 * time.Second)
	if fake.standbys != 1 || !s.Status().Asleep {
		t.Fatalf("standbys %d, asleep %v; want the TV switched off", fake.standbys, s.Status().Asleep)
	}
	// Off is off: it is not switched off again and again.
	step(2 * time.Hour)
	if fake.standbys != 1 {
		t.Errorf("standbys = %d while the TV was off already", fake.standbys)
	}

	// Switched on again; a film, then music behind the dashboard: neither is "nothing".
	if err := s.SetPower(ctx, true); err != nil {
		t.Fatal(err)
	}
	fake.power = cec.PowerOn
	step(time.Minute)
	watching = true
	step(3 * time.Hour)
	watching, music = false, true
	step(3 * time.Hour)
	if fake.standbys != 1 || s.Status().SleepAt != nil {
		t.Fatalf("while watching and listening: standbys %d, status %+v", fake.standbys, s.Status())
	}
	// The music ends: the time counts from then.
	music = false
	step(20 * time.Minute)
	if s.Status().SleepAt != nil {
		t.Error("20 minutes after the music ended is too early")
	}
	step(10 * time.Minute)
	if s.Status().SleepAt == nil {
		t.Error("30 minutes after the music ended the notice is due")
	}

	// A sleep timer somebody sets is theirs, also while the notice is up: a
	// button does not cancel it.
	s.SetSleep(90)
	s.NoteActivity()
	if st := s.Status(); st.SleepAt == nil || st.SleepMinutes != 90 {
		t.Errorf("the sleep timer somebody set: %+v", st)
	}
}

func TestWithoutTheSettingTheTVStaysOn(t *testing.T) {
	fake := &fakeTV{power: cec.PowerOn}
	s := New(Options{TV: fake, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	s.Step(context.Background(), at(14, 8, 0))
	s.Step(context.Background(), at(15, 8, 0))
	if fake.standbys != 0 || s.Status().SleepAt != nil {
		t.Errorf("standbys %d, status %+v", fake.standbys, s.Status())
	}
}
