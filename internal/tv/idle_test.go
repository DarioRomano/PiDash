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

func TestTheTVIsLeftOnWhileItShowsSomethingElse(t *testing.T) {
	fake := &fakeTV{power: cec.PowerOn}
	elsewhere := false
	off := Clock{23, 30}
	var told []Status
	s := New(Options{
		TV: fake, IdleOff: 30 * time.Minute, Off: &off, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		AppRunning: func() bool { return false },
		// The TV says when it turns to another input or to one of its own apps.
		Elsewhere: func() bool { return elsewhere },
	})
	s.OnChange(func(st Status) { told = append(told, st) })
	ctx := context.Background()
	clock := at(14, 20, 0)
	step := func(d time.Duration) {
		clock = clock.Add(d)
		s.Step(ctx, clock)
	}
	step(0)

	// Ten minutes of the dashboard, then two hours of a film in one of the
	// TV's own apps: its buttons do not come here, and that is no reason to
	// switch the TV off.
	step(10 * time.Minute)
	elsewhere = true
	for i := 0; i < 240; i++ {
		step(30 * time.Second)
	}
	if st := s.Status(); fake.standbys != 0 || st.SleepAt != nil || !st.Elsewhere {
		t.Fatalf("during the film: standbys %d, status %+v", fake.standbys, st)
	}
	// Back on the dashboard, the half hour starts anew.
	elsewhere = false
	step(28 * time.Minute)
	if fake.standbys != 0 || s.Status().SleepAt != nil {
		t.Fatalf("28 minutes after coming back: standbys %d, status %+v", fake.standbys, s.Status())
	}
	// The minute's notice is running when the TV turns to something else:
	// nobody sees it, and it is taken back.
	step(90 * time.Second)
	if s.Status().SleepAt == nil {
		t.Fatal("no notice")
	}
	elsewhere = true
	said := len(told)
	step(30 * time.Second)
	step(60 * time.Second)
	if st := s.Status(); fake.standbys != 0 || st.SleepAt != nil || st.Idle || len(told) == said {
		t.Fatalf("the notice was not taken back: standbys %d, status %+v", fake.standbys, st)
	}

	// The nightly switch-off waits as well, as it does for an app on the Pi,
	// and is made up for when the dashboard is back.
	step(time.Until(at(14, 23, 31)) - time.Until(clock))
	step(30 * time.Minute)
	if fake.standbys != 0 {
		t.Fatal("the TV was switched off at half past eleven, in the middle of something else")
	}
	elsewhere = false
	step(30 * time.Second)
	if fake.standbys != 1 {
		t.Errorf("back on the dashboard after the time: standbys %d, want 1", fake.standbys)
	}
}

func TestASleepTimerThatWasSetStillCounts(t *testing.T) {
	// Set by hand, it means "off in half an hour, whatever I watch".
	fake := &fakeTV{power: cec.PowerOn}
	s := New(Options{TV: fake, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		AppRunning: func() bool { return false }, Elsewhere: func() bool { return true }})
	ctx := context.Background()
	s.Step(ctx, time.Now())
	s.SetSleep(1)
	s.Step(ctx, time.Now().Add(61*time.Second))
	if fake.standbys != 1 {
		t.Errorf("a sleep timer set by hand: standbys %d, want 1", fake.standbys)
	}
}

// When the TV turns to something else or back, the dashboard hears of it
// with the TV's status, which says so.
func TestTheDashboardHearsWhenTheTVShowsSomethingElse(t *testing.T) {
	p := &Pretend{}
	s := New(Options{TV: p, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Elsewhere: p.Elsewhere})
	heard := make(chan Status, 4)
	s.OnChange(func(st Status) { heard <- st })
	p.OnElsewhere = s.NoteElsewhere
	if err := p.Show(context.Background(), "2.0.0.0"); err != nil {
		t.Fatal(err)
	}
	if st := <-heard; !st.Elsewhere {
		t.Errorf("after switching to another device: %+v", st)
	}
	p.Here()
	if st := <-heard; st.Elsewhere {
		t.Errorf("after a button of the TV's remote: %+v", st)
	}
	p.Here() // nothing changes: nothing to hear
	select {
	case st := <-heard:
		t.Errorf("told without a change: %+v", st)
	default:
	}
}
