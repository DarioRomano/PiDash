// Package providers defines the data-source interface and the runner that
// polls each provider, caches its last good value and publishes updates.
package providers

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// Provider is a periodically polled data source for one dashboard widget.
type Provider interface {
	// Name identifies the provider to the frontend, e.g. "weather".
	Name() string
	// Interval is how often Fetch is called.
	Interval() time.Duration
	// Fetch returns the widget's state. It must be JSON-serialisable.
	Fetch(ctx context.Context) (any, error)
}

// Entry is the cached state of one provider as sent to the frontend.
type Entry struct {
	Provider  string          `json:"provider"`
	Data      json.RawMessage `json:"data"`
	UpdatedAt time.Time       `json:"updatedAt"`
	// Stale is true when the most recent Fetch failed; Data is the last good value.
	Stale bool   `json:"stale"`
	Error string `json:"error,omitempty"`
}

// Runner polls providers and keeps their latest state.
type Runner struct {
	mu        sync.RWMutex
	entries   map[string]Entry
	providers []Provider
	publish   func(Entry)
	log       *slog.Logger
}

// NewRunner creates a runner that calls publish on every state change.
func NewRunner(log *slog.Logger, publish func(Entry), ps ...Provider) *Runner {
	return &Runner{
		entries:   map[string]Entry{},
		providers: ps,
		publish:   publish,
		log:       log,
	}
}

// Snapshot returns the current state of every provider.
func (r *Runner) Snapshot() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, 0, len(r.entries))
	for _, p := range r.providers {
		if e, ok := r.entries[p.Name()]; ok {
			out = append(out, e)
		}
	}
	return out
}

// Refresh polls one provider right away, for a change that should show at once.
func (r *Runner) Refresh(ctx context.Context, name string) {
	for _, p := range r.providers {
		if p.Name() == name {
			r.fetchOnce(ctx, p)
		}
	}
}

// Run polls all providers until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, p := range r.providers {
		wg.Add(1)
		go func(p Provider) {
			defer wg.Done()
			r.loop(ctx, p)
		}(p)
	}
	wg.Wait()
}

// retryDelay is the first wait after a failed fetch; it doubles on every
// further failure, up to the provider's normal interval.
const retryDelay = 15 * time.Second

func (r *Runner) loop(ctx context.Context, p Provider) {
	failures := 0
	for {
		wait := p.Interval()
		if r.fetchOnce(ctx, p) {
			failures = 0
		} else {
			// Retry soon instead of waiting a whole interval. This matters most
			// right after boot, when the backend starts before the network is up.
			if d := retryDelay << min(failures, 10); d < wait {
				wait = d
			}
			failures++
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// fetchOnce polls the provider and publishes the result. It reports success.
func (r *Runner) fetchOnce(ctx context.Context, p Provider) bool {
	fctx, cancel := context.WithTimeout(ctx, min(p.Interval(), time.Minute))
	defer cancel()

	data, err := p.Fetch(fctx)

	r.mu.Lock()
	prev, had := r.entries[p.Name()]
	var e Entry
	switch {
	case err == nil:
		raw, merr := json.Marshal(data)
		if merr != nil {
			err = merr
			break
		}
		e = Entry{Provider: p.Name(), Data: raw, UpdatedAt: time.Now()}
	}
	if err != nil {
		r.log.Warn("provider fetch failed", "provider", p.Name(), "err", err)
		e = prev
		if !had {
			e = Entry{Provider: p.Name(), Data: json.RawMessage("null")}
		}
		e.Stale = true
		e.Error = err.Error()
	}
	r.entries[p.Name()] = e
	r.mu.Unlock()

	r.publish(e)
	return err == nil
}
