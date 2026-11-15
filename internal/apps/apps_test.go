package apps

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

type fakeBackend struct {
	mu           sync.Mutex
	active       map[string]bool
	calls        []string
	consoleReady bool
	startErr     error
}

func (f *fakeBackend) Start(_ context.Context, a config.App) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "start:"+a.ID)
	if f.startErr != nil {
		return f.startErr
	}
	f.active[a.ID] = true
	return nil
}

func (f *fakeBackend) Stop(_ context.Context, a config.App) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "stop:"+a.ID)
	delete(f.active, a.ID)
	return nil
}

func (f *fakeBackend) Active(_ context.Context, a config.App) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active[a.ID]
}

func (f *fakeBackend) ConsoleReady() bool { return f.consoleReady }

func (f *fakeBackend) exit(id string) {
	f.mu.Lock()
	delete(f.active, id)
	f.mu.Unlock()
}

func newTestManager(b *fakeBackend) (*Manager, chan Status) {
	apps := []config.App{
		{ID: "steam", Name: "Steam Link", Command: "steamlink", Mode: ModeDesktop},
		{ID: "moonlight", Name: "Moonlight", Command: "moonlight-qt", Mode: ModeConsole},
	}
	m := NewManager(apps, b, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.poll = 5 * time.Millisecond
	ch := make(chan Status, 16)
	m.OnChange(func(s Status) { ch <- s })
	return m, ch
}

func waitFor(t *testing.T, ch chan Status, what string, ok func(Status) bool) Status {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case s := <-ch:
			if ok(s) {
				return s
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestStartStop(t *testing.T) {
	b := &fakeBackend{active: map[string]bool{}, consoleReady: true}
	m, ch := newTestManager(b)
	ctx := context.Background()

	if err := m.Start(ctx, "steam"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ch, "steam running", func(s Status) bool { return s.Running == "steam" })

	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ch, "stopped", func(s Status) bool { return s.Running == "" })
	if got := m.Status(); got.Error != "" {
		t.Errorf("deliberate stop should not report an error, got %q", got.Error)
	}
}

func TestStartingAnotherAppStopsTheFirst(t *testing.T) {
	b := &fakeBackend{active: map[string]bool{}, consoleReady: true}
	m, ch := newTestManager(b)
	ctx := context.Background()

	m.Start(ctx, "steam")
	m.Start(ctx, "moonlight")
	waitFor(t, ch, "moonlight running", func(s Status) bool { return s.Running == "moonlight" })

	want := []string{"start:steam", "stop:steam", "start:moonlight"}
	b.mu.Lock()
	got := slices.Clone(b.calls)
	b.mu.Unlock()
	if !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	// The stale watcher for steam must not clear the new app.
	time.Sleep(30 * time.Millisecond)
	if r := m.Status().Running; r != "moonlight" {
		t.Errorf("running = %q after the old watcher ticked, want moonlight", r)
	}
}

func TestAppExitingByItselfIsNoticed(t *testing.T) {
	b := &fakeBackend{active: map[string]bool{}, consoleReady: true}
	m, ch := newTestManager(b)

	m.Start(context.Background(), "steam")
	waitFor(t, ch, "running", func(s Status) bool { return s.Running == "steam" })
	b.exit("steam")
	s := waitFor(t, ch, "exit noticed", func(s Status) bool { return s.Running == "" })
	if s.Error == "" {
		t.Error("an exit right after start should be reported as a failed launch")
	}
}

func TestConsoleModeNeedsSetup(t *testing.T) {
	b := &fakeBackend{active: map[string]bool{}, consoleReady: false}
	m, ch := newTestManager(b)

	err := m.Start(context.Background(), "moonlight")
	if !errors.Is(err, ErrConsoleNotReady) {
		t.Fatalf("err = %v, want ErrConsoleNotReady", err)
	}
	s := waitFor(t, ch, "error status", func(s Status) bool { return s.Error != "" })
	if s.Running != "" || s.ConsoleReady {
		t.Errorf("unexpected status %+v", s)
	}
	if len(b.calls) != 0 {
		t.Errorf("backend should not be called, got %v", b.calls)
	}
	// Desktop apps are unaffected.
	if err := m.Start(context.Background(), "steam"); err != nil {
		t.Errorf("desktop app should still start: %v", err)
	}
}

func TestUnknownAppAndStartFailure(t *testing.T) {
	b := &fakeBackend{active: map[string]bool{}, consoleReady: true}
	m, _ := newTestManager(b)

	if err := m.Start(context.Background(), "nope"); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("err = %v, want ErrUnknownApp", err)
	}
	b.startErr = errors.New("boom")
	if err := m.Start(context.Background(), "steam"); err == nil {
		t.Error("expected start error")
	}
	if s := m.Status(); s.Running != "" || s.Error == "" {
		t.Errorf("after failed start: %+v", s)
	}
}

func TestRunAdoptsAlreadyRunningApp(t *testing.T) {
	b := &fakeBackend{active: map[string]bool{"moonlight": true}, consoleReady: true}
	m, ch := newTestManager(b)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go m.Run(ctx)

	waitFor(t, ch, "adopted", func(s Status) bool { return s.Running == "moonlight" })
	b.exit("moonlight")
	s := waitFor(t, ch, "exit noticed", func(s Status) bool { return s.Running == "" })
	if s.Error != "" {
		t.Errorf("an adopted app exiting is not a failed launch, got %q", s.Error)
	}
}

func listenUnix(t *testing.T, path string) {
	t.Helper()
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("cannot create unix socket: %v", err)
	}
	t.Cleanup(func() { l.Close() })
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				m[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return m
}

func TestDesktopEnvFindsWaylandSession(t *testing.T) {
	run := t.TempDir()
	os.WriteFile(filepath.Join(run, "wayland-0.lock"), nil, 0o600) // not a socket: ignored
	listenUnix(t, filepath.Join(run, "wayland-1"))

	got := envMap(desktopEnv([]string{"PATH=/bin"}, run, filepath.Join(run, "no-x11"), run))
	if got["WAYLAND_DISPLAY"] != "wayland-1" || got["XDG_RUNTIME_DIR"] != run {
		t.Errorf("env = %v", got)
	}
	if _, ok := got["DISPLAY"]; ok {
		t.Errorf("DISPLAY should not be set without an X socket: %v", got)
	}
}

func TestDesktopEnvFallsBackToX11AndKeepsExistingValues(t *testing.T) {
	run, x11 := t.TempDir(), t.TempDir()
	listenUnix(t, filepath.Join(x11, "X0"))
	os.WriteFile(filepath.Join(run, ".Xauthority"), nil, 0o600)

	got := envMap(desktopEnv(nil, run, x11, run))
	if got["DISPLAY"] != ":0" || got["XAUTHORITY"] != filepath.Join(run, ".Xauthority") || got["WAYLAND_DISPLAY"] != "" {
		t.Errorf("x11 env = %v", got)
	}

	kept := envMap(desktopEnv([]string{"WAYLAND_DISPLAY=wayland-7", "XDG_RUNTIME_DIR=/custom", "DISPLAY=:3"}, run, x11, run))
	if kept["WAYLAND_DISPLAY"] != "wayland-7" || kept["XDG_RUNTIME_DIR"] != "/custom" || kept["DISPLAY"] != ":3" {
		t.Errorf("existing values were overwritten: %v", kept)
	}
}

func TestConsoleEnvDropsDisplayVariables(t *testing.T) {
	got := envMap(ConsoleEnv([]string{"PATH=/bin", "WAYLAND_DISPLAY=wayland-0", "DISPLAY=:0", "XAUTHORITY=/x", "HOME=/h"}))
	if len(got) != 2 || got["PATH"] != "/bin" || got["HOME"] != "/h" {
		t.Errorf("env = %v", got)
	}
}

func TestReorderKeepsTheSameAppsInANewOrder(t *testing.T) {
	list := []config.App{{ID: "a", Name: "A", Command: "a"}, {ID: "b", Name: "B", Command: "b"}, {ID: "c", Name: "C", Command: "c"}}
	m := NewManager(list, &Mock{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var seen []string
	m.OnChange(func(st Status) {
		seen = nil
		for _, a := range st.Apps {
			seen = append(seen, a.ID)
		}
	})
	if !m.Reorder([]string{"c", "a", "b"}) {
		t.Fatal("a valid order was refused")
	}
	if got := strings.Join(seen, ","); got != "c,a,b" {
		t.Errorf("order announced = %s, want c,a,b", got)
	}
	for _, bad := range [][]string{{"a", "b"}, {"a", "b", "x"}, {"a", "a", "b"}, {}} {
		if m.Reorder(bad) {
			t.Errorf("order %v must be refused", bad)
		}
	}
	var ids []string
	for _, a := range m.Status().Apps {
		ids = append(ids, a.ID)
	}
	if got := strings.Join(ids, ","); got != "c,a,b" {
		t.Errorf("a refused order changed the row: %s", got)
	}
}
