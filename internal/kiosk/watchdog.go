package kiosk

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Browser is the part of Controller the watchdog needs.
type Browser interface {
	Active(ctx context.Context) bool
	Restart(ctx context.Context) error
}

// Watchdog starts the kiosk browser again when the dashboard in it has
// stopped answering.
//
// A browser that runs for weeks can end up with a page that has frozen or
// crashed while the browser itself is still there, so nothing restarts it:
// the TV shows a stuck clock, or an error page, until someone notices. The
// dashboard therefore tells the backend every few seconds that it is alive
// (Beat). When that stops for longer than Silence while the dashboard should
// be showing, the browser is restarted.
//
// A browser that crashes outright needs none of this: its service starts it
// again by itself. And nothing is done before the dashboard has reported in
// at least once since PiDash started.
type Watchdog struct {
	Browser Browser
	// Paused reports times when silence proves nothing: while an app has the
	// screen the browser may put its hidden page to sleep.
	Paused func() bool
	Log    *slog.Logger
	// Silence is how long the dashboard may stay quiet; zero means three minutes.
	Silence time.Duration
	// Now is the clock; nil means the real one.
	Now func() time.Time

	mu       sync.Mutex
	lastBeat time.Time
	// since is when the current stretch of watching began: the browser was
	// started, an app was closed, or the browser was restarted. Silence is
	// counted from here at the earliest, so a page gets time to load.
	since    time.Time
	restarts []time.Time
	gaveUp   bool
}

// maxRestarts is how often the browser is restarted within an hour. A page
// that will not stay up has a cause restarting does not cure.
const maxRestarts = 3

func (w *Watchdog) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Beat records that the dashboard on the TV has just reported in.
func (w *Watchdog) Beat() {
	w.mu.Lock()
	w.lastBeat = w.now()
	w.mu.Unlock()
}

// Check looks whether the dashboard is still answering and restarts the
// browser if not. It reports whether it did.
func (w *Watchdog) Check(ctx context.Context) bool {
	silence := w.Silence
	if silence <= 0 {
		silence = 3 * time.Minute
	}
	now := w.now()
	watching := w.Browser.Active(ctx) && (w.Paused == nil || !w.Paused())

	w.mu.Lock()
	if !watching {
		w.since = time.Time{}
		w.mu.Unlock()
		return false
	}
	if w.since.IsZero() {
		w.since = now
	}
	// Until the dashboard has reported in once, its silence proves nothing:
	// in a setup where its reports do not arrive at all (a browser that
	// reaches PiDash through another address), restarting would help
	// nobody, three times an hour.
	if w.lastBeat.IsZero() {
		w.mu.Unlock()
		return false
	}
	heard := w.since
	if w.lastBeat.After(heard) {
		heard = w.lastBeat
	}
	if now.Sub(heard) < silence {
		w.mu.Unlock()
		return false
	}
	// Quiet for too long.
	recent := w.restarts[:0]
	for _, at := range w.restarts {
		if now.Sub(at) < time.Hour {
			recent = append(recent, at)
		}
	}
	w.restarts = recent
	if len(recent) >= maxRestarts {
		first := !w.gaveUp
		w.gaveUp = true
		w.mu.Unlock()
		if first {
			w.Log.Error("the dashboard on the TV keeps freezing; its browser was restarted three times within an hour and is now left alone")
		}
		return false
	}
	w.gaveUp = false
	w.restarts = append(w.restarts, now)
	w.since = now // the restarted page gets the same time to load
	quiet := now.Sub(heard).Round(time.Second)
	w.mu.Unlock()

	w.Log.Warn("the dashboard on the TV stopped answering; starting its browser again", "quiet for", quiet.String())
	if err := w.Browser.Restart(ctx); err != nil {
		w.Log.Error("could not restart the browser on the TV", "err", err)
		return false
	}
	return true
}

// Run checks every half minute until ctx ends.
func (w *Watchdog) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.Check(ctx)
		}
	}
}
