package logring

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRemembersWarningsAndErrorsOnly(t *testing.T) {
	var out bytes.Buffer
	ring := New(3)
	log := slog.New(ring.Handler(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo})))

	log.Debug("not even printed")
	log.Info("started", "port", 8080)
	log.Warn("cannot reach the NAS", "err", errors.New("connection refused"))
	log.With("provider", "weather").Error("fetch failed", "status", 503)

	lines := ring.Lines()
	if len(lines) != 2 {
		t.Fatalf("remembered %d lines, want 2: %+v", len(lines), lines)
	}
	if lines[0].Level != "error" || lines[0].Text != "fetch failed · provider weather · status 503" {
		t.Errorf("newest = %+v", lines[0])
	}
	if lines[1].Level != "warn" || lines[1].Text != "cannot reach the NAS: connection refused" {
		t.Errorf("older = %+v", lines[1])
	}
	// The normal log is not changed by listening in.
	printed := out.String()
	if strings.Contains(printed, "not even printed") || !strings.Contains(printed, "started") || !strings.Contains(printed, "cannot reach the NAS") {
		t.Errorf("log output changed:\n%s", printed)
	}
}

func TestRepeatsCountUpAndOldLinesFallOut(t *testing.T) {
	ring := New(3)
	log := slog.New(ring.Handler(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	for range 50 {
		log.Warn("TV does not answer")
	}
	if lines := ring.Lines(); len(lines) != 1 || lines[0].Count != 50 {
		t.Fatalf("a repeated message should be one line counted 50 times: %+v", lines)
	}
	for _, m := range []string{"a", "b", "c"} {
		log.Warn(m)
	}
	lines := ring.Lines()
	if len(lines) != 3 || lines[0].Text != "c" || lines[2].Text != "a" {
		t.Errorf("want the last three, newest first: %+v", lines)
	}
}
