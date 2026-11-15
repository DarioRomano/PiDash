package apps

import (
	"context"
	"log/slog"
	"sync"

	"github.com/DarioRomano/PiDash/internal/config"
)

// Mock is a Backend for development: it only remembers what is "running".
type Mock struct {
	Log *slog.Logger

	mu     sync.Mutex
	active map[string]bool
}

// Start implements Backend.
func (b *Mock) Start(_ context.Context, app config.App) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active == nil {
		b.active = map[string]bool{}
	}
	b.active[app.ID] = true
	if b.Log != nil {
		b.Log.Info("app (mock) start", "app", app.ID, "mode", app.Mode, "command", app.Command)
	}
	return nil
}

// Stop implements Backend.
func (b *Mock) Stop(_ context.Context, app config.App) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.active, app.ID)
	return nil
}

// Active implements Backend.
func (b *Mock) Active(_ context.Context, app config.App) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.active[app.ID]
}

// ConsoleReady implements Backend.
func (b *Mock) ConsoleReady() bool { return true }
