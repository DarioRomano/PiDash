package kiosk

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeBrowser struct {
	active   bool
	restarts int
}

func (f *fakeBrowser) Active(context.Context) bool { return f.active }
func (f *fakeBrowser) Restart(context.Context) error {
	f.restarts++
	return nil
}

func newWatchdog() (*Watchdog, *fakeBrowser, *time.Time, *bool) {
	b := &fakeBrowser{active: true}
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	paused := false
	w := &Watchdog{Browser: b, Paused: func() bool { return paused }, Now: func() time.Time { return now },
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return w, b, &now, &paused
}

func TestADashboardThatAnswersIsLeftAlone(t *testing.T) {
	w, b, now, _ := newWatchdog()
	ctx := context.Background()
	for i := 0; i < 60*24; i++ { // a day, a beat and a check every minute
		*now = now.Add(time.Minute)
		w.Beat()
		w.Check(ctx)
	}
	if b.restarts != 0 {
		t.Errorf("a dashboard that reports in was restarted %d times", b.restarts)
	}
}

func TestAFrozenDashboardIsRestartedAfterThreeMinutes(t *testing.T) {
	w, b, now, _ := newWatchdog()
	ctx := context.Background()
	w.Check(ctx) // the browser has just been started: the page gets time to load
	*now = now.Add(2 * time.Minute)
	w.Beat()
	// From here on, silence.
	*now = now.Add(2*time.Minute + 50*time.Second)
	if w.Check(ctx) || b.restarts != 0 {
		t.Fatal("restarted before three minutes of silence had passed")
	}
	*now = now.Add(20 * time.Second)
	if !w.Check(ctx) || b.restarts != 1 {
		t.Fatalf("after 3 min 10 s of silence: %d restarts, want 1", b.restarts)
	}
	// The restarted page gets its three minutes too.
	*now = now.Add(2 * time.Minute)
	if w.Check(ctx) || b.restarts != 1 {
		t.Errorf("the new page was not given time to load: %d restarts", b.restarts)
	}
	w.Beat() // and it loaded
	*now = now.Add(time.Minute)
	w.Check(ctx)
	if b.restarts != 1 {
		t.Errorf("restarted again although the new page reports in: %d", b.restarts)
	}
}

func TestNothingIsRestartedBeforeTheDashboardHasEverReportedIn(t *testing.T) {
	// A setup in which the reports do not arrive at all must not end up
	// with its browser restarted again and again.
	w, b, now, _ := newWatchdog()
	ctx := context.Background()
	for i := 0; i < 120; i++ {
		*now = now.Add(time.Minute)
		w.Check(ctx)
	}
	if b.restarts != 0 {
		t.Fatalf("%d restarts without ever having heard from the dashboard", b.restarts)
	}
	// Once it has reported in, silence counts; also for the page that the
	// restart brings up and that never loads.
	w.Beat()
	*now = now.Add(3*time.Minute + time.Second)
	if !w.Check(ctx) || b.restarts != 1 {
		t.Fatalf("after reporting in and going quiet: %d restarts, want 1", b.restarts)
	}
	*now = now.Add(3*time.Minute + time.Second)
	if !w.Check(ctx) || b.restarts != 2 {
		t.Errorf("the restarted page never loaded: %d restarts, want 2", b.restarts)
	}
}

func TestSilenceMeansNothingWhileAnAppIsOpenOrTheBrowserIsClosed(t *testing.T) {
	w, b, now, paused := newWatchdog()
	ctx := context.Background()
	w.Check(ctx)
	w.Beat()

	// Two hours of a film: the hidden dashboard may sleep.
	*paused = true
	for i := 0; i < 120; i++ {
		*now = now.Add(time.Minute)
		w.Check(ctx)
	}
	// Back on the dashboard, it gets time to wake up before silence counts.
	*paused = false
	w.Check(ctx)
	*now = now.Add(2 * time.Minute)
	w.Check(ctx)
	if b.restarts != 0 {
		t.Fatalf("restarted %d times around an app", b.restarts)
	}
	*now = now.Add(2 * time.Minute)
	if !w.Check(ctx) {
		t.Error("a dashboard that stays silent after the app closed should be restarted")
	}

	// Closed on purpose (Exit to desktop): nothing to watch.
	b.restarts, b.active = 0, false
	for i := 0; i < 60; i++ {
		*now = now.Add(time.Minute)
		w.Check(ctx)
	}
	if b.restarts != 0 {
		t.Errorf("a browser that was closed on purpose was started %d times", b.restarts)
	}
}

func TestItGivesUpAfterThreeRestartsInAnHour(t *testing.T) {
	w, b, now, _ := newWatchdog()
	ctx := context.Background()
	w.Beat()                  // it ran once
	for i := 0; i < 50; i++ { // fifty minutes of a page that never comes up
		*now = now.Add(time.Minute)
		w.Check(ctx)
	}
	if b.restarts != maxRestarts {
		t.Fatalf("%d restarts within an hour, want %d", b.restarts, maxRestarts)
	}
	// An hour later it may try again.
	*now = now.Add(70 * time.Minute)
	w.Check(ctx)
	if b.restarts != maxRestarts+1 {
		t.Errorf("after the hour had passed: %d restarts, want %d", b.restarts, maxRestarts+1)
	}
}
