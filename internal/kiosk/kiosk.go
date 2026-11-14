// Package kiosk starts and stops the fullscreen browser that shows the
// dashboard on the TV. The browser runs as a systemd user unit inside the
// normal desktop session, so stopping it simply returns to the desktop.
package kiosk

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable is returned when kiosk control is not configured.
var ErrUnavailable = errors.New("kiosk control is not configured")

// Controller drives the kiosk systemd user unit.
type Controller struct {
	unit string
	mock bool
	log  *slog.Logger

	mu         sync.Mutex
	mockActive bool
}

// New returns a controller for the given systemd user unit. An empty unit
// disables kiosk control. With mock set, no commands are run (development).
func New(unit string, mock bool, log *slog.Logger) *Controller {
	return &Controller{unit: strings.TrimSpace(unit), mock: mock, log: log}
}

// Available reports whether start/stop can be used.
func (c *Controller) Available() bool { return c.unit != "" }

// Active reports whether the kiosk browser is currently running.
func (c *Controller) Active(ctx context.Context) bool {
	if !c.Available() {
		return false
	}
	if c.mock {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.mockActive
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", "--user", "is-active", "--quiet", c.unit).Run() == nil
}

// Start shows the dashboard on the TV.
func (c *Controller) Start(ctx context.Context) error { return c.run(ctx, "start", true) }

// Stop closes the dashboard and returns to the desktop.
func (c *Controller) Stop(ctx context.Context) error { return c.run(ctx, "stop", false) }

// Restart closes the browser and opens it again.
func (c *Controller) Restart(ctx context.Context) error { return c.run(ctx, "restart", true) }

func (c *Controller) run(ctx context.Context, verb string, active bool) error {
	if !c.Available() {
		return ErrUnavailable
	}
	if c.mock {
		c.mu.Lock()
		c.mockActive = active
		c.mu.Unlock()
		c.log.Info("kiosk (mock)", "action", verb)
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// --no-block: return immediately; the unit waits for the backend itself.
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "--no-block", verb, c.unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %s %s: %w: %s", verb, c.unit, err, strings.TrimSpace(string(out)))
	}
	c.log.Info("kiosk", "action", verb, "unit", c.unit)
	return nil
}
