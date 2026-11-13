package providers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type flaky struct {
	mu       sync.Mutex
	calls    int
	failures int // fail this many times first
}

func (f *flaky) Name() string            { return "flaky" }
func (f *flaky) Interval() time.Duration { return time.Hour }
func (f *flaky) Fetch(context.Context) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failures {
		return nil, errors.New("network is down")
	}
	return map[string]int{"value": f.calls}, nil
}

func TestFailedFetchKeepsLastValueAndIsMarkedStale(t *testing.T) {
	var published []Entry
	p := &flaky{}
	r := NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)), func(e Entry) { published = append(published, e) }, p)

	if !r.fetchOnce(context.Background(), p) {
		t.Fatal("first fetch should succeed")
	}
	p.failures = 99
	if r.fetchOnce(context.Background(), p) {
		t.Fatal("second fetch should fail")
	}
	last := published[len(published)-1]
	if !last.Stale || last.Error == "" || string(last.Data) != `{"value":1}` {
		t.Errorf("after a failure the last good value should be kept and marked stale: %+v data=%s", last, last.Data)
	}
	if snap := r.Snapshot(); len(snap) != 1 || !snap[0].Stale {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestFailureBeforeAnySuccessPublishesNull(t *testing.T) {
	var got Entry
	p := &flaky{failures: 1}
	r := NewRunner(slog.New(slog.NewTextHandler(io.Discard, nil)), func(e Entry) { got = e }, p)
	r.fetchOnce(context.Background(), p)
	if string(got.Data) != "null" || !got.Stale {
		t.Errorf("entry = %+v data=%s", got, got.Data)
	}
}
