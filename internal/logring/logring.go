// Package logring keeps the most recent warnings and errors in memory, so
// the status page on the TV can show what went wrong without anyone having
// to open a terminal.
package logring

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Line is one remembered log message.
type Line struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"` // "warn" or "error"
	Text  string    `json:"text"`
	// Count is how many times in a row the message was logged; Time is the
	// latest of them.
	Count int `json:"count"`
}

// Ring remembers the last few warnings and errors.
type Ring struct {
	mu    sync.Mutex
	size  int
	lines []Line
}

// New returns a ring that remembers size messages.
func New(size int) *Ring { return &Ring{size: size} }

// Lines returns the remembered messages, newest first.
func (r *Ring) Lines() []Line {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Line, 0, len(r.lines))
	for i := len(r.lines) - 1; i >= 0; i-- {
		out = append(out, r.lines[i])
	}
	return out
}

func (r *Ring) add(l Line) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Something that fails every minute would push everything else out:
	// a repeat only counts up.
	if n := len(r.lines); n > 0 && r.lines[n-1].Text == l.Text && r.lines[n-1].Level == l.Level {
		r.lines[n-1].Time = l.Time
		r.lines[n-1].Count++
		return
	}
	r.lines = append(r.lines, l)
	if len(r.lines) > r.size {
		r.lines = r.lines[len(r.lines)-r.size:]
	}
}

// Handler passes every record on to next and remembers warnings and errors.
func (r *Ring) Handler(next slog.Handler) slog.Handler { return &handler{ring: r, next: next} }

type handler struct {
	ring *Ring
	next slog.Handler
	// attrs were added with With; they belong to every message.
	attrs []slog.Attr
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= slog.LevelWarn || h.next.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= slog.LevelWarn {
		var b strings.Builder
		b.WriteString(rec.Message)
		write := func(a slog.Attr) bool {
			if a.Key == "" {
				return true
			}
			// "err" explains itself; other values need their name.
			if a.Key == "err" || a.Key == "error" {
				fmt.Fprintf(&b, ": %v", a.Value.Resolve().Any())
			} else {
				fmt.Fprintf(&b, " · %s %v", a.Key, a.Value.Resolve().Any())
			}
			return true
		}
		for _, a := range h.attrs {
			write(a)
		}
		rec.Attrs(write)
		level := "warn"
		if rec.Level >= slog.LevelError {
			level = "error"
		}
		text := b.String()
		if len(text) > 400 {
			text = text[:400] + "…"
		}
		h.ring.add(Line{Time: rec.Time, Level: level, Text: text, Count: 1})
	}
	if !h.next.Enabled(ctx, rec.Level) {
		return nil
	}
	return h.next.Handle(ctx, rec)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &handler{ring: h.ring, next: h.next.WithAttrs(attrs), attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{ring: h.ring, next: h.next.WithGroup(name), attrs: h.attrs}
}
