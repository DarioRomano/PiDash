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
	Log        *slog.Logger
}

// Status is sent to the dashboard and the phone remote.
type Status struct {
	// Available is false when the TV cannot be controlled (no CEC).
	Available bool `json:"available"`
	// Asleep is true while the TV is in standby because PiDash put it there.
	// The dashboard stops loading photos meanwhile.
	Asleep bool   `json:"asleep"`
	Off    string `json:"off,omitempty"`
	On     string `json:"on,omitempty"`
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
	st := Status{Available: s.opt.TV.Available(), Asleep: s.asleep}
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
	s.mu.Unlock()

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

	// After we sent the TV to standby, follow what it reports, so the
	// dashboard wakes up again when someone switches the TV on by hand.
	if !standbyAt.IsZero() {
		power, _ := s.opt.TV.PowerStatus(ctx)
		s.mu.Lock()
		was := s.asleep
		switch {
		case power == cec.PowerStandby:
			s.asleep = true
		case now.Sub(standbyAt) < settle:
			// Still shutting down; keep the current assumption.
		default:
			// On again, or a TV that does not report: stop assuming it is off.
			s.asleep = false
			s.standbyAt = time.Time{}
		}
		changed := was != s.asleep
		st, fn := s.statusLocked(), s.onChange
		s.mu.Unlock()
		if changed {
			fn(st)
		}
	}
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
