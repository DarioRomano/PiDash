package tv

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/cec"
)

type fakeTV struct {
	power    cec.Power
	standbys int
	wakes    int
	silent   bool // a TV that does not report its power state
}

func (f *fakeTV) Available() bool { return true }
func (f *fakeTV) Standby(context.Context) error {
	f.standbys++
	f.power = cec.PowerStandby
	return nil
}
func (f *fakeTV) Wake(context.Context) error {
	f.wakes++
	f.power = cec.PowerOn
	return nil
}
func (f *fakeTV) PowerStatus(context.Context) (cec.Power, error) {
	if f.silent {
		return cec.PowerUnknown, nil
	}
	return f.power, nil
}

func at(day, hour, minute int) time.Time {
	return time.Date(2026, 11, day, hour, minute, 0, 0, time.UTC)
}

func newService(tvSet *fakeTV, appRunning *bool) *Service {
	off, on := Clock{23, 30}, Clock{6, 30}
	return New(Options{
		Off: &off, On: &on, TV: tvSet,
		AppRunning: func() bool { return *appRunning },
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestParseClock(t *testing.T) {
	for in, want := range map[string]Clock{"23:30": {23, 30}, "06:05": {6, 5}, " 0:00 ": {0, 0}} {
		if got, err := ParseClock(in); err != nil || got != want {
			t.Errorf("ParseClock(%q) = %v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "24:00", "12:60", "12", "noon", "12:5", "-1:00"} {
		if _, err := ParseClock(bad); err == nil {
			t.Errorf("ParseClock(%q) should fail", bad)
		}
	}
}

func TestNightlyOffAndMorningOn(t *testing.T) {
	set := &fakeTV{power: cec.PowerOn}
	running := false
	s := newService(set, &running)
	ctx := context.Background()

	s.Step(ctx, at(13, 23, 0)) // backend starts in the evening: nothing happens
	s.Step(ctx, at(13, 23, 29))
	if set.standbys != 0 {
		t.Fatal("switched off before the configured time")
	}
	s.Step(ctx, at(13, 23, 30))
	if set.standbys != 1 || !s.Status().Asleep {
		t.Fatalf("23:30: standbys=%d asleep=%v", set.standbys, s.Status().Asleep)
	}
	s.Step(ctx, at(13, 23, 31))
	s.Step(ctx, at(14, 3, 0))
	if set.standbys != 1 {
		t.Errorf("standby was sent again during the night: %d", set.standbys)
	}
	s.Step(ctx, at(14, 6, 30))
	if set.wakes != 1 || s.Status().Asleep {
		t.Errorf("06:30: wakes=%d asleep=%v", set.wakes, s.Status().Asleep)
	}
	s.Step(ctx, at(14, 23, 30))
	if set.standbys != 2 {
		t.Errorf("second night: standbys=%d", set.standbys)
	}
}

func TestStartingDuringTheNightDoesNothing(t *testing.T) {
	set := &fakeTV{power: cec.PowerOn}
	running := false
	s := newService(set, &running)
	// The Pi is rebooted at 02:00. The TV is left alone until the next scheduled time.
	s.Step(context.Background(), at(14, 2, 0))
	s.Step(context.Background(), at(14, 2, 1))
	if set.standbys != 0 || set.wakes != 0 || s.Status().Asleep {
		t.Errorf("standbys=%d wakes=%d asleep=%v", set.standbys, set.wakes, s.Status().Asleep)
	}
}

func TestTVIsNotSwitchedOffWhileSomethingIsPlaying(t *testing.T) {
	set := &fakeTV{power: cec.PowerOn}
	running := true
	s := newService(set, &running)
	ctx := context.Background()

	s.Step(ctx, at(13, 23, 29))
	s.Step(ctx, at(13, 23, 30))
	s.Step(ctx, at(14, 0, 15))
	if set.standbys != 0 {
		t.Fatal("the TV was switched off during a film")
	}
	running = false // the film ends
	s.Step(ctx, at(14, 0, 16))
	if set.standbys != 1 {
		t.Errorf("after the app closed: standbys=%d", set.standbys)
	}

	// If the film runs until morning, the pending switch-off is dropped.
	set2 := &fakeTV{power: cec.PowerOn}
	running = true
	s2 := newService(set2, &running)
	s2.Step(ctx, at(13, 23, 29))
	s2.Step(ctx, at(13, 23, 30))
	s2.Step(ctx, at(14, 6, 30))
	running = false
	s2.Step(ctx, at(14, 6, 31))
	if set2.standbys != 0 {
		t.Errorf("TV switched off in the morning after an all-night session: %d", set2.standbys)
	}
}

func TestDashboardWakesWhenTheTVIsSwitchedOnByHand(t *testing.T) {
	set := &fakeTV{power: cec.PowerOn}
	running := false
	s := newService(set, &running)
	ctx := context.Background()
	var changes []bool
	s.OnChange(func(st Status) { changes = append(changes, st.Asleep) })

	s.Step(ctx, at(13, 23, 29))
	s.Step(ctx, at(13, 23, 30))
	s.Step(ctx, at(13, 23, 31))
	if !s.Status().Asleep {
		t.Fatal("should be asleep")
	}
	set.power = cec.PowerOn // someone presses the power button at 1 am
	s.Step(ctx, at(14, 1, 0))
	if s.Status().Asleep {
		t.Error("still asleep after the TV was switched on by hand")
	}
	if len(changes) < 2 || changes[0] != true || changes[len(changes)-1] != false {
		t.Errorf("status changes = %v", changes)
	}
	s.Step(ctx, at(14, 1, 1))
	if set.standbys != 1 {
		t.Errorf("the TV was switched off again: %d", set.standbys)
	}
}

func TestTVThatDoesNotReportItsStateIsNotAssumedOffForLong(t *testing.T) {
	set := &fakeTV{power: cec.PowerOn, silent: true}
	running := false
	s := newService(set, &running)
	ctx := context.Background()
	s.Step(ctx, at(13, 23, 29))
	s.Step(ctx, at(13, 23, 30))
	s.Step(ctx, at(13, 23, 30).Add(30*time.Second))
	if !s.Status().Asleep {
		t.Error("should be assumed off right after the command")
	}
	s.Step(ctx, at(13, 23, 33))
	if s.Status().Asleep {
		t.Error("without confirmation from the TV the dashboard must not stay asleep")
	}
}

func TestManualPower(t *testing.T) {
	set := &fakeTV{power: cec.PowerOn}
	running := true // manual control ignores what is playing
	s := newService(set, &running)
	ctx := context.Background()
	if err := s.SetPower(ctx, false); err != nil || set.standbys != 1 || !s.Status().Asleep {
		t.Errorf("off: err=%v standbys=%d asleep=%v", err, set.standbys, s.Status().Asleep)
	}
	if err := s.SetPower(ctx, true); err != nil || set.wakes != 1 || s.Status().Asleep {
		t.Errorf("on: err=%v wakes=%d asleep=%v", err, set.wakes, s.Status().Asleep)
	}
	if st := s.Status(); st.Off != "23:30" || st.On != "06:30" || !st.Available {
		t.Errorf("status = %+v", st)
	}
}

func TestSleepTimer(t *testing.T) {
	fake := &fakeTV{power: cec.PowerOn}
	stopped := 0
	s := New(Options{TV: fake, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), OnSleep: func() { stopped++ }})
	start := time.Now()
	s.Step(context.Background(), start)

	st := s.SetSleep(30)
	if st.SleepAt == nil || st.SleepMinutes != 30 {
		t.Fatalf("status = %+v", st)
	}
	s.Step(context.Background(), start.Add(29*time.Minute))
	if fake.standbys != 0 {
		t.Fatal("the TV was switched off early")
	}
	s.Step(context.Background(), start.Add(31*time.Minute))
	if fake.standbys != 1 || stopped != 1 || s.Status().SleepAt != nil {
		t.Errorf("standbys %d, apps stopped %d, status %+v; want one of each and the timer cleared", fake.standbys, stopped, s.Status())
	}

	// Cancelled in time, nothing happens.
	s.SetSleep(10)
	s.SetSleep(0)
	s.Step(context.Background(), start.Add(3*time.Hour))
	if fake.standbys != 1 {
		t.Errorf("a cancelled timer switched the TV off")
	}
}
