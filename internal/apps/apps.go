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
	// Game marks a tile PiDash made itself from the games of a PC. It is
	// not one of the apps the person arranged, and cannot be moved.
	Game bool `json:"game,omitempty"`
}

// Status is the launcher state sent to the dashboard and the phone remote.
type Status struct {
	Apps []Info `json:"apps"`
	// Running is the id of the app that has the screen, or "".
	Running string `json:"running"`
	// Background is the id of an app that keeps running behind the
	// dashboard, playing music, or "".
	Background string `json:"background,omitempty"`
	// ConsoleReady is false when console-mode apps cannot be started yet.
	ConsoleReady bool `json:"consoleReady"`
	// Error describes the most recent launch problem, if any.
	Error string `json:"error,omitempty"`
}

// quickExit is how soon after starting an exit is treated as a failed launch.
const quickExit = 5 * time.Second

// Manager owns the launcher state.
type Manager struct {
	apps []config.App
	// games are tiles made from what a PC offers through Moonlight. They
	// come and go with what Moonlight knows, and follow the apps in the row.
	games   []config.App
	backend Backend
	log     *slog.Logger
	poll    time.Duration

	icons map[string]Icon
	// Focus gives the open app's window the keyboard; nil does nothing.
	Focus *Focuser

	mu sync.Mutex
	// running is the app whose program runs; with hidden set it does so
	// behind the dashboard, and the dashboard has the screen.
	running   string
	hidden    bool
	startedAt time.Time
	lastErr   string
	gen       int // bumped on every start/stop so stale watchers stop
	onChange  func(Status)
}

// NewManager returns a launcher for the configured apps.
func NewManager(apps []config.App, backend Backend, log *slog.Logger) *Manager {
	return &Manager{apps: apps, backend: backend, log: log, poll: 2 * time.Second, icons: map[string]Icon{}, onChange: func(Status) {}}
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
	infos := make([]Info, 0, len(m.apps)+len(m.games))
	for _, a := range m.apps {
		_, hasIcon := m.icons[a.ID]
		infos = append(infos, Info{ID: a.ID, Name: a.Name, Mode: a.Mode, HasIcon: hasIcon})
	}
	for _, g := range m.games {
		_, hasIcon := m.icons[g.ID]
		infos = append(infos, Info{ID: g.ID, Name: g.Name, Mode: g.Mode, HasIcon: hasIcon, Game: true})
	}
	st := Status{Apps: infos, Running: m.running, ConsoleReady: m.backend.ConsoleReady(), Error: m.lastErr}
	if m.hidden {
		st.Running, st.Background = "", m.running
	}
	return st
}

// Reorder puts the apps in the order of ids, which must name each app once.
// It reports whether it did.
func (m *Manager) Reorder(ids []string) bool {
	m.mu.Lock()
	if len(ids) != len(m.apps) {
		m.mu.Unlock()
		return false
	}
	byID := map[string]config.App{}
	for _, a := range m.apps {
		byID[a.ID] = a
	}
	next := make([]config.App, 0, len(ids))
	for _, id := range ids {
		a, ok := byID[id]
		if !ok {
			m.mu.Unlock()
			return false
		}
		delete(byID, id) // an id named twice is not found the second time
		next = append(next, a)
	}
	m.apps = next
	st, notify := m.statusLocked(), m.onChange
	m.mu.Unlock()
	notify(st)
	return true
}

func (m *Manager) find(id string) (config.App, bool) {
	m.mu.Lock()
	lists := [][]config.App{m.apps, m.games}
	m.mu.Unlock()
	for _, list := range lists {
		for _, a := range list {
			if a.ID == id {
				return a, true
			}
		}
	}
	return config.App{}, false
}

// Icon returns the resolved tile icon of an app.
func (m *Manager) Icon(id string) (Icon, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ic, ok := m.icons[id]
	return ic, ok
}

// SetGames replaces the game tiles. A game that is running stays known
// until it has ended, also when it is no longer on the list.
func (m *Manager) SetGames(games []config.App, f *IconFinder) {
	icons := map[string]Icon{}
	for _, g := range games {
		if ic, ok := f.Resolve(g); ok {
			icons[g.ID] = ic
		}
	}
	m.mu.Lock()
	if running := m.running; running != "" {
		listed := false
		for _, g := range games {
			listed = listed || g.ID == running
		}
		if !listed {
			for _, g := range m.games {
				if g.ID == running {
					games = append(games, g)
					if ic, ok := m.icons[g.ID]; ok {
						icons[g.ID] = ic
					}
				}
			}
		}
	}
	same := len(games) == len(m.games)
	for i := 0; same && i < len(games); i++ {
		same = games[i] == m.games[i]
	}
	for _, g := range m.games {
		delete(m.icons, g.ID)
	}
	for id, ic := range icons {
		m.icons[id] = ic
	}
	m.games = games
	st, notify := m.statusLocked(), m.onChange
	m.mu.Unlock()
	if !same {
		notify(st)
	}
}

// Run adopts an app that is already running (for example after the backend was
// restarted while Moonlight was open) and then waits for ctx to end.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	list := append(append([]config.App{}, m.apps...), m.games...)
	m.mu.Unlock()
	for _, a := range list {
		if m.backend.Active(ctx, a) {
			// An app that plays in the background may be behind the dashboard
			// right now: PiDash restarts when settings are saved, the app
			// does not. The dashboard having the keyboard tells.
			behind := a.Background && m.Focus.Focused(ctx, KioskWindow)
			m.mu.Lock()
			m.hidden = behind
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
	prevID, behind := m.running, m.hidden
	m.mu.Unlock()
	if prevID == id && m.backend.Active(ctx, app) {
		if behind {
			// It was playing behind the dashboard: bring it to the front.
			m.mu.Lock()
			m.hidden = false
			st, notify := m.statusLocked(), m.onChange
			m.mu.Unlock()
			m.log.Info("app brought to the front", "app", id)
			notify(st)
			go m.Focus.Ensure(context.WithoutCancel(ctx), WindowOf(app))
		}
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
	m.hidden = false
	m.startedAt = time.Now()
	m.lastErr = ""
	m.gen++
	gen := m.gen
	st, notify := m.statusLocked(), m.onChange
	m.mu.Unlock()

	m.log.Info("app started", "app", id, "mode", app.Mode)
	notify(st)
	// Its window should get the keyboard as soon as it is there.
	if app.Mode != ModeConsole {
		go m.awaitWindow(context.WithoutCancel(ctx), app, gen)
	}
	// The watcher outlives the HTTP request that started the app.
	go m.watch(context.WithoutCancel(ctx), app, gen)
	return nil
}

// awaitWindow gives a newly started app's window the keyboard as soon as it
// is there. It gives up once the app is no longer the one in front: closed
// again, replaced by another, or sent behind the dashboard, from where
// handing it the keyboard would pull it back to the front.
func (m *Manager) awaitWindow(ctx context.Context, app config.App, gen int) {
	window := WindowOf(app)
	if window == "" || !m.Focus.Available() {
		return
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		m.mu.Lock()
		inFront := m.gen == gen && m.running == app.ID && !m.hidden
		m.mu.Unlock()
		if !inFront || m.Focus.Ensure(ctx, window) {
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(700 * time.Millisecond):
		}
	}
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
		m.hidden = false
		m.gen++
	}
	st, notify := m.statusLocked(), m.onChange
	m.mu.Unlock()
	m.log.Info("app stopped", "app", id)
	notify(st)
	go m.Focus.Ensure(context.Background(), KioskWindow) // back to the dashboard
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
		m.hidden = false
		m.gen++
		if time.Since(m.startedAt) < quickExit {
			m.lastErr = fmt.Sprintf("%s closed right after starting. If its window did open, the program handed over to a copy that was already running; otherwise check the logs.", app.Name)
		}
		st, notify := m.statusLocked(), m.onChange
		m.mu.Unlock()
		m.log.Info("app exited", "app", app.ID)
		notify(st)
		go m.Focus.Ensure(context.Background(), KioskWindow)
		return
	}
}

// Leave returns to the dashboard: what holding Back does. Most apps are
// closed. One that plays music in the background is only put behind the
// dashboard, where it carries on playing; Stop closes it for good.
func (m *Manager) Leave(ctx context.Context) error {
	m.mu.Lock()
	id, behind := m.running, m.hidden
	m.mu.Unlock()
	app, ok := m.find(id)
	if !ok || behind {
		return nil
	}
	if !app.Background || app.Mode == ModeConsole || WindowOf(app) == "" || !m.Focus.Available() {
		return m.Stop(ctx)
	}
	// The dashboard comes to the front. If its window is not there, there is
	// nothing to return to behind which the app could play.
	if !m.Focus.Ensure(ctx, KioskWindow) {
		return m.Stop(ctx)
	}
	m.mu.Lock()
	if m.running == id {
		m.hidden = true
	}
	st, notify := m.statusLocked(), m.onChange
	m.mu.Unlock()
	m.log.Info("app keeps playing behind the dashboard", "app", id)
	notify(st)
	return nil
}

// RunningWebsite reports whether the open app is a website.
func (m *Manager) RunningWebsite() bool {
	m.mu.Lock()
	id := m.running
	if m.hidden {
		id = ""
	}
	m.mu.Unlock()
	app, ok := m.find(id)
	return ok && app.URL != ""
}

// CanFocus reports whether an app's window can be given the keyboard.
func (m *Manager) CanFocus() bool { return m.Focus.Available() }

// EnsureFocus makes sure the open app's window has the keyboard. It is cheap
// enough to call on every button press of the remote.
func (m *Manager) EnsureFocus() {
	m.mu.Lock()
	id := m.running
	if m.hidden {
		id = "" // the dashboard has the screen, and keeps the keyboard
	}
	m.mu.Unlock()
	if app, ok := m.find(id); ok && app.Mode != ModeConsole {
		m.Focus.EnsureSoon(WindowOf(app))
	}
}

func (m *Manager) setError(msg string) {
	m.mu.Lock()
	m.lastErr = msg
	st, notify := m.statusLocked(), m.onChange
	m.mu.Unlock()
	notify(st)
}
