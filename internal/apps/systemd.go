package apps

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

// ConsoleUnitPath is the system unit installed by `install.sh --console-apps`.
// Its presence is what makes console mode available.
const ConsoleUnitPath = "/etc/systemd/system/pidash-console@.service"

// Systemd runs apps as systemd units, so they survive a backend restart, log
// to the journal, and can be stopped reliably together with all their children.
//
//   - desktop mode: a transient user unit  pidash-app-<id>.service
//   - console mode: the system template    pidash-console@<id>.service
//
// Both end up running `pidash -exec-app <id>`, which looks the command up in
// the config, so the command is defined in exactly one place.
type Systemd struct {
	// Exe is the absolute path of the pidash binary; ConfigPath of its config.
	Exe        string
	ConfigPath string
	Log        *slog.Logger
	// ConsoleUnit overrides ConsoleUnitPath (tests).
	ConsoleUnit string
}

func unitName(app config.App) string {
	if app.Mode == ModeConsole {
		return "pidash-console@" + app.ID + ".service"
	}
	return "pidash-app-" + app.ID + ".service"
}

// systemctl builds a systemctl invocation in the right scope for the app.
func systemctl(ctx context.Context, app config.App, args ...string) *exec.Cmd {
	full := []string{"--no-ask-password"}
	if app.Mode != ModeConsole {
		full = append(full, "--user")
	}
	return exec.CommandContext(ctx, "systemctl", append(full, args...)...)
}

// Start implements Backend.
func (s *Systemd) Start(ctx context.Context, app config.App) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	unit := unitName(app)

	var cmd *exec.Cmd
	if app.Mode == ModeConsole {
		// The template unit switches to its virtual terminal, runs the app as
		// this user and switches back when it exits.
		cmd = systemctl(ctx, app, "--no-block", "start", unit)
	} else {
		// A previous failed run would block reuse of the unit name.
		_ = systemctl(ctx, app, "reset-failed", unit).Run()
		args := []string{"--user", "--collect", "--quiet", "--unit=" + unit,
			"--description=PiDash app: " + app.Name, "--", s.Exe}
		if s.ConfigPath != "" {
			args = append(args, "-config", s.ConfigPath)
		}
		args = append(args, "-exec-app", app.ID)
		cmd = exec.CommandContext(ctx, "systemd-run", args...)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Stop implements Backend.
func (s *Systemd) Stop(ctx context.Context, app config.App) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if out, err := systemctl(ctx, app, "stop", unitName(app)).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Active implements Backend. A unit that is still starting or stopping counts
// as running, so the dashboard does not flicker between states.
func (s *Systemd) Active(ctx context.Context, app config.App) bool {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, _ := systemctl(ctx, app, "is-active", unitName(app)).Output()
	switch strings.TrimSpace(string(out)) {
	case "active", "activating", "deactivating", "reloading":
		return true
	}
	return false
}

// ConsoleReady implements Backend.
func (s *Systemd) ConsoleReady() bool {
	path := s.ConsoleUnit
	if path == "" {
		path = ConsoleUnitPath
	}
	_, err := os.Stat(path)
	return err == nil
}
