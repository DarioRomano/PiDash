// Package apps launches external programs (Moonlight, Kodi, Steam Link, …)
// from the dashboard and tracks which one is running.
//
// An app runs in one of two modes:
//
//   - desktop: inside the normal desktop session, like starting it from the menu.
//   - console: on its own virtual terminal, outside the desktop compositor, so
//     it can drive the display directly (lower latency, HDR, refresh-rate
//     switching). When it exits the screen switches back to where it was.
//
// Only one app runs at a time.
package apps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

// Launch modes.
const (
	ModeDesktop = config.AppModeDesktop
	ModeConsole = config.AppModeConsole
)

var (
	// ErrUnknownApp is returned for an id that is not in the configuration.
	ErrUnknownApp = errors.New("unknown app")
	// ErrConsoleNotReady is returned when a console-mode app is started but
	// the one-time system setup has not been done.
	ErrConsoleNotReady = errors.New("console mode is not set up; run the installer with --console-apps")
)

// Backend starts, stops and inspects apps. The real one uses systemd.
type Backend interface {
	Start(ctx context.Context, app config.App) error
	Stop(ctx context.Context, app config.App) error
	Active(ctx context.Context, app config.App) bool
	// ConsoleReady reports whether console mode has been set up on this machine.
	ConsoleReady() bool
}

// Info describes one configured app to the frontend.
type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	HasIcon bool   `json:"hasIcon"`
}

// Status is the launcher state sent to the dashboard and the phone remote.
type Status struct {
	Apps []Info `json:"apps"`
	// Running is the id of the running app, or "".
	Running string `json:"running"`
	// ConsoleReady is false when console-mode apps cannot be started yet.
	ConsoleReady bool `json:"consoleReady"`
	// Error describes the most recent launch problem, if any.
	Error string `json:"error,omitempty"`
}

// quickExit is how soon after starting an exit is treated as a failed launch.
const quickExit = 5 * time.Second

// Manager owns the launcher state.
type Manager struct {
	apps    []config.App
	backend Backend
	log     *slog.Logger
	poll    time.Duration

	icons map[string]Icon

	mu        sync.Mutex
	running   string
	startedAt time.Time
	lastErr   string
	gen       int // bumped on every start/stop so stale watchers stop
	onChange  func(Status)
}

// NewManager returns a launcher for the configured apps.
func NewManager(apps []config.App, backend Backend, log *slog.Logger) *Manager {
	return &Manager{apps: apps, backend: backend, log: log, poll: time.Second, icons: map[string]Icon{}, onChange: func(Status) {}}
}

// ResolveIcons looks up the tile icon of every app once, at startup.
func (m *Manager) ResolveIcons(f *IconFinder) {
	for _, a := range m.apps {
		if ic, ok := f.Resolve(a); ok {
			m.icons[a.ID] = ic
		} else if a.Icon != "" {
			m.log.Warn("icon not found; using the letter tile", "app", a.ID, "icon", a.Icon)
		}
	}
}

// OnChange registers a callback invoked whenever the status changes.
func (m *Manager) OnChange(fn func(Status)) {
	m.mu.Lock()
	m.onChange = fn
	m.mu.Unlock()
}

// Status returns the current launcher state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) statusLocked() Status {
	infos := make([]Info, 0, len(m.apps))
	for _, a := range m.apps {
		_, hasIcon := m.icons[a.ID]
		infos = append(infos, Info{ID: a.ID, Name: a.Name, Mode: a.Mode, HasIcon: hasIcon})
	}
	return Status{Apps: infos, Running: m.running, ConsoleReady: m.backend.ConsoleReady(), Error: m.lastErr}
}

func (m *Manager) find(id string) (config.App, bool) {
	for _, a := range m.apps {
		if a.ID == id {
			return a, true
		}
	}
	return config.App{}, false
}

// Icon returns the resolved tile icon of an app.
func (m *Manager) Icon(id string) (Icon, bool) {
	ic, ok := m.icons[id]
	return ic, ok
}

// Run adopts an app that is already running (for example after the backend was
// restarted while Moonlight was open) and then waits for ctx to end.
func (m *Manager) Run(ctx context.Context) {
	for _, a := range m.apps {
		if m.backend.Active(ctx, a) {
			m.mu.Lock()
			m.running = a.ID
			m.startedAt = time.Now().Add(-quickExit)
			m.gen++
			gen := m.gen
			st, notify := m.statusLocked(), m.onChange
			m.mu.Unlock()
			m.log.Info("adopted running app", "app", a.ID)
			notify(st)
			go m.watch(ctx, a, gen)
			break
		}
	}
	<-ctx.Done()
}

// Start launches the app with the given id, stopping any other running app first.
func (m *Manager) Start(ctx context.Context, id string) error {
	app, ok := m.find(id)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownApp, id)
	}
	if app.Mode == ModeConsole && !m.backend.ConsoleReady() {
		m.setError(fmt.Sprintf("%s needs console mode, which is not set up yet. Run the installer with --console-apps.", app.Name))
		return ErrConsoleNotReady
	}

	m.mu.Lock()
	prevID := m.running
	m.mu.Unlock()
	if prevID == id && m.backend.Active(ctx, app) {
		return nil // already running
	}
	if prev, ok := m.find(prevID); ok && prevID != id {
		if err := m.backend.Stop(ctx, prev); err != nil {
			m.log.Warn("stopping previous app", "app", prevID, "err", err)
		}
	}

	if err := m.backend.Start(ctx, app); err != nil {
		m.setError(fmt.Sprintf("Could not start %s: %v", app.Name, err))
		return err
	}

	m.mu.Lock()
	m.running = id
	m.startedAt = time.Now()
	m.lastErr = ""
	m.gen++
	gen := m.gen
	st, notify := m.statusLocked(), m.onChange
	m.mu.Unlock()

	m.log.Info("app started", "app", id, "mode", app.Mode)
	notify(st)
	// The watcher outlives the HTTP request that started the app.
	go m.watch(context.WithoutCancel(ctx), app, gen)
	return nil
}

// Stop stops the running app, if any, which returns the screen to the dashboard.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	id := m.running
	m.mu.Unlock()
	app, ok := m.find(id)
	if !ok {
		return nil
	}
	if err := m.backend.Stop(ctx, app); err != nil {
		return err
	}
	m.mu.Lock()
	if m.running == id {
		m.running = ""
		m.gen++
	}
	st, notify := m.statusLocked(), m.onChange
	m.mu.Unlock()
	m.log.Info("app stopped", "app", id)
	notify(st)
	return nil
}

// watch polls until the app exits by itself, then publishes the change.
func (m *Manager) watch(ctx context.Context, app config.App, gen int) {
	t := time.NewTicker(m.poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		m.mu.Lock()
		current := m.gen == gen
		m.mu.Unlock()
		if !current {
			return
		}
		if m.backend.Active(ctx, app) {
			continue
		}
		m.mu.Lock()
		if m.gen != gen {
			m.mu.Unlock()
			return
		}
		m.running = ""
		m.gen++
		if time.Since(m.startedAt) < quickExit {
			m.lastErr = fmt.Sprintf("%s closed right after starting. Check its command in config.toml and the logs.", app.Name)
		}
		st, notify := m.statusLocked(), m.onChange
		m.mu.Unlock()
		m.log.Info("app exited", "app", app.ID)
		notify(st)
		return
	}
}

func (m *Manager) setError(msg string) {
	m.mu.Lock()
	m.lastErr = msg
	st, notify := m.statusLocked(), m.onChange
	m.mu.Unlock()
	notify(st)
}
