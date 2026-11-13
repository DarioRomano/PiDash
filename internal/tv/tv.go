// Package tv switches the TV off at night and on in the morning over HDMI-CEC,
// and lets the phone remote do the same on demand.
package tv

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DarioRomano/PiDash/internal/cec"
)

// Clock is a time of day.
type Clock struct{ Hour, Minute int }

// ParseClock parses "HH:MM" in 24-hour time.
func ParseClock(s string) (Clock, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	hour, err1 := strconv.Atoi(h)
	minute, err2 := strconv.Atoi(m)
	if !ok || err1 != nil || err2 != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 || len(m) != 2 {
		return Clock{}, fmt.Errorf("%q is not a time of day like 23:30", s)
	}
	return Clock{hour, minute}, nil
}

func (c Clock) String() string { return fmt.Sprintf("%02d:%02d", c.Hour, c.Minute) }

// last returns the most recent moment this time of day occurred, at or before now.
func (c Clock) last(now time.Time) time.Time {
	t := time.Date(now.Year(), now.Month(), now.Day(), c.Hour, c.Minute, 0, 0, now.Location())
	if t.After(now) {
		t = time.Date(now.Year(), now.Month(), now.Day()-1, c.Hour, c.Minute, 0, 0, now.Location())
	}
	return t
}

// TV is the part of cec.Controller the service needs.
type TV interface {
	Available() bool
	Standby(ctx context.Context) error
	Wake(ctx context.Context) error
	PowerStatus(ctx context.Context) (cec.Power, error)
}

// Options configure the service.
type Options struct {
	// Off and On are the daily switch-off and switch-on times; either may be nil.
	Off, On *Clock
	TV      TV
	// AppRunning reports whether something is being watched or played. The TV
	// is never switched off in the middle of that.
	AppRunning func() bool
	// OnSleep is called when the sleep timer runs out, before the TV is
	// switched off: the place to close an app that is still playing.
	OnSleep func()
	Log     *slog.Logger
}

// Status is sent to the dashboard and the phone remote.
type Status struct {
	// Available is false when the TV cannot be controlled (no CEC).
	Available bool `json:"available"`
	// Asleep is true while the TV is in standby, whoever put it there. The
	// dashboard stops drawing and loading photos meanwhile.
	Asleep bool   `json:"asleep"`
	Off    string `json:"off,omitempty"`
	On     string `json:"on,omitempty"`
	// SleepAt is when the sleep timer switches the TV off; SleepMinutes what
	// it was set to.
	SleepAt      *time.Time `json:"sleepAt,omitempty"`
	SleepMinutes int        `json:"sleepMinutes,omitempty"`
}

// settle is how long a TV may take to report "standby" after being told to.
const settle = 90 * time.Second

// Service runs the schedule.
type Service struct {
	opt Options

	mu         sync.Mutex
	lastTick   time.Time
	pendingOff bool      // the switch-off time has passed but an app was running
	standbyAt  time.Time // when we last sent the TV to standby; zero if we did not
	asleep     bool
	onChange   func(Status)
	// lastPoll is when the TV was last asked whether it is on; heardStandby
	// when it last announced that it is switching off.
	lastPoll     time.Time
	heardStandby time.Time
	// lastLook and lastWakeLook are when a game controller last made the
	// service ask the TV whether it is on, and ask in order to wake it.
	lastLook, lastWakeLook time.Time
	// sleepAt is when the sleep timer switches the TV off; zero when not set.
	sleepAt      time.Time
	sleepMinutes int
}

// New returns a TV service.
func New(opt Options) *Service {
	if opt.AppRunning == nil {
		opt.AppRunning = func() bool { return false }
	}
	return &Service{opt: opt, onChange: func(Status) {}}
}

// OnChange registers a callback for status changes.
func (s *Service) OnChange(fn func(Status)) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

// Status returns the current state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked()
}

func (s *Service) statusLocked() Status {
	st := Status{Available: s.opt.TV.Available(), Asleep: s.asleep, SleepMinutes: s.sleepMinutes}
	if !s.sleepAt.IsZero() {
		at := s.sleepAt
		st.SleepAt = &at
	}
	if s.opt.Off != nil {
		st.Off = s.opt.Off.String()
	}
	if s.opt.On != nil {
		st.On = s.opt.On.String()
	}
	return st
}

// Run checks the schedule twice a minute until ctx ends. Polling the clock,
// rather than sleeping until the next event, stays correct across clock
// changes, daylight saving and the Pi being suspended.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		s.Step(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Step advances the schedule to now. It is exported for tests.
func (s *Service) Step(ctx context.Context, now time.Time) {
	s.mu.Lock()
	last := s.lastTick
	s.lastTick = now
	first := last.IsZero()
	// A schedule time "happened" if its latest occurrence falls after the previous check.
	passed := func(c *Clock) bool { return c != nil && !first && c.last(now).After(last) }
	turnOn, turnOff := passed(s.opt.On), passed(s.opt.Off)
	if turnOff {
		s.pendingOff = true
	}
	if turnOn {
		s.pendingOff = false
	}
	pending := s.pendingOff
	standbyAt := s.standbyAt
	sleepDue := !s.sleepAt.IsZero() && !now.Before(s.sleepAt)
	if sleepDue {
		s.sleepAt, s.sleepMinutes = time.Time{}, 0
	}
	s.mu.Unlock()

	if sleepDue {
		// Whoever set the timer has fallen asleep: close what is playing, too.
		s.opt.Log.Info("sleep timer: switching the TV off")
		if s.opt.OnSleep != nil {
			s.opt.OnSleep()
		}
		if err := s.setPower(ctx, false, now); err != nil {
			s.opt.Log.Warn("sleep timer: switch off failed", "err", err)
		}
		return
	}

	switch {
	case turnOn:
		s.opt.Log.Info("tv schedule: switching the TV on")
		if err := s.setPower(ctx, true, now); err != nil {
			s.opt.Log.Warn("tv schedule: switch on failed", "err", err)
		}
		return
	case pending:
		if s.opt.AppRunning() {
			// Someone is watching: try again on the following checks.
			return
		}
		s.mu.Lock()
		s.pendingOff = false
		s.mu.Unlock()
		s.opt.Log.Info("tv schedule: switching the TV off")
		if err := s.setPower(ctx, false, now); err != nil {
			s.opt.Log.Warn("tv schedule: switch off failed", "err", err)
		}
		return
	}

	// Follow what the TV reports about itself. The dashboard rests while the
	// TV is off, however it was switched off, and carries on when it is back.
	// Right after sending it to standby this is checked on every step,
	// otherwise once a minute.
	s.mu.Lock()
	due := !standbyAt.IsZero() || now.Sub(s.lastPoll) >= pollEvery
	if due {
		s.lastPoll = now
	}
	s.mu.Unlock()
	if !due || !s.opt.TV.Available() {
		return
	}
	power, _ := s.opt.TV.PowerStatus(ctx)
	s.mu.Lock()
	was := s.asleep
	switch {
	case power == cec.PowerStandby:
		s.asleep = true
	case power == cec.PowerOn:
		s.asleep, s.standbyAt = false, time.Time{}
	case standbyAt.IsZero():
		// A TV that does not say: believe what was last heard from it.
	case now.Sub(standbyAt) < settle:
		// Still shutting down; keep the current assumption.
	default:
		// We switched it off, but it does not report: stop assuming it is off.
		s.asleep, s.standbyAt = false, time.Time{}
	}
	changed := was != s.asleep
	st, fn := s.statusLocked(), s.onChange
	s.mu.Unlock()
	if changed {
		s.opt.Log.Info("tv power changed", "asleep", st.Asleep)
		fn(st)
	}
}

// pollEvery is how often the TV is asked whether it is on.
const pollEvery = time.Minute

// quiet is how long after a "going to standby" signs of life from the TV are
// ignored: it still talks while it shuts down.
const quiet = 8 * time.Second

// NoteStandby records that the TV announced it is switching off, which TVs
// do when switched off with their own remote.
func (s *Service) NoteStandby() {
	s.mu.Lock()
	was := s.asleep
	s.asleep, s.heardStandby = true, time.Now()
	st, fn := s.statusLocked(), s.onChange
	s.mu.Unlock()
	if !was {
		s.opt.Log.Info("the TV announced standby; resting")
		fn(st)
	}
}

// NoteAwake records a sign that the TV is on: it spoke on the HDMI
// connection, or someone pressed a button.
func (s *Service) NoteAwake() {
	s.mu.Lock()
	now := time.Now()
	if !s.asleep || now.Sub(s.heardStandby) < quiet || (!s.standbyAt.IsZero() && now.Sub(s.standbyAt) < quiet) {
		s.mu.Unlock()
		return
	}
	s.asleep, s.standbyAt = false, time.Time{}
	st, fn := s.statusLocked(), s.onChange
	s.mu.Unlock()
	s.opt.Log.Info("the TV is on again; carrying on")
	fn(st)
}

// lookAgain is the shortest time between two looks at the TV on behalf of a
// game controller, whose buttons can arrive many times a second.
const lookAgain = 5 * time.Second

// WakeIfOff switches the TV on, and to the Pi's input, if it is in standby.
// A TV that is on is left alone: it may be showing something else, and
// taking its input away would be rude. It reports whether the TV was woken.
func (s *Service) WakeIfOff(ctx context.Context) (bool, error) { return s.look(ctx, true) }

// Recheck asks the TV whether it is on, when it is believed to be off. It
// is for a button pressed on a game controller, which unlike the TV's own
// remote does not prove that the TV is on.
func (s *Service) Recheck(ctx context.Context) { _, _ = s.look(ctx, false) }

func (s *Service) look(ctx context.Context, wake bool) (bool, error) {
	if !s.opt.TV.Available() {
		return false, nil
	}
	now := time.Now()
	s.mu.Lock()
	// Counted apart: the logo button asks for both at once, and the question
	// must not use up the wake-up's turn.
	last := &s.lastLook
	if wake {
		last = &s.lastWakeLook
	}
	if now.Sub(*last) < lookAgain {
		s.mu.Unlock()
		return false, nil
	}
	*last = now
	believedOff := s.asleep
	s.mu.Unlock()

	power, _ := s.opt.TV.PowerStatus(ctx)
	// A TV that does not say is taken to be as it was last known.
	off := power == cec.PowerStandby || (power != cec.PowerOn && believedOff)
	if !off {
		s.mu.Lock()
		was := s.asleep
		s.asleep, s.standbyAt = false, time.Time{}
		st, fn := s.statusLocked(), s.onChange
		s.mu.Unlock()
		if was {
			s.opt.Log.Info("the TV is on again; carrying on")
			fn(st)
		}
		return false, nil
	}
	if !wake {
		return false, nil
	}
	if err := s.setPower(ctx, true, now); err != nil {
		return false, err
	}
	return true, nil
}

// SetSleep starts the sleep timer: the TV is switched off in that many
// minutes. Zero cancels it.
func (s *Service) SetSleep(minutes int) Status {
	s.mu.Lock()
	if minutes <= 0 {
		s.sleepAt, s.sleepMinutes = time.Time{}, 0
	} else {
		s.sleepAt, s.sleepMinutes = time.Now().Add(time.Duration(minutes)*time.Minute), minutes
	}
	st, fn := s.statusLocked(), s.onChange
	s.mu.Unlock()
	fn(st)
	return st
}

// SetPower switches the TV on or off now, from the phone remote.
func (s *Service) SetPower(ctx context.Context, on bool) error {
	s.mu.Lock()
	s.pendingOff = false
	s.mu.Unlock()
	return s.setPower(ctx, on, time.Now())
}

func (s *Service) setPower(ctx context.Context, on bool, now time.Time) error {
	var err error
	if on {
		err = s.opt.TV.Wake(ctx)
	} else {
		err = s.opt.TV.Standby(ctx)
	}
	s.mu.Lock()
	if on {
		s.asleep, s.standbyAt = false, time.Time{}
	} else if err == nil {
		// Assume it worked until the TV says otherwise.
		s.asleep, s.standbyAt = true, now
	}
	st, fn := s.statusLocked(), s.onChange
	s.mu.Unlock()
	fn(st)
	return err
}
