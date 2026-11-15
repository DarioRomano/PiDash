// Package power restarts the Pi or switches it off, on request from the TV.
package power

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

// Control is what the server needs.
type Control interface {
	// Reboot restarts the Pi; Off shuts it down.
	Reboot(ctx context.Context) error
	Off(ctx context.Context) error
}

// System does it for real, through systemd.
type System struct {
	Log *slog.Logger
	// Run executes a command; nil means the real one. For tests.
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (s *System) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if s.Run != nil {
		return s.Run(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// do asks systemd for verb ("reboot" or "poweroff"). Asking as the person
// works where the desktop lets its user do that. PiDash runs in the
// background, though, which some systems do not count as "the person at the
// screen"; there it is asked again with sudo, which Raspberry Pi OS allows
// its first user without a password. Nothing ever waits for a password.
func (s *System) do(ctx context.Context, verb string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var problems []string
	for _, cmd := range [][]string{
		{"systemctl", "--no-ask-password", verb},
		{"sudo", "-n", "systemctl", verb},
	} {
		out, err := s.run(ctx, cmd[0], cmd[1:]...)
		if err == nil {
			s.Log.Info("power", "action", verb, "with", cmd[0])
			return nil
		}
		problems = append(problems, strings.TrimSpace(lastLine(string(out))))
	}
	s.Log.Warn("power: not allowed", "action", verb, "answers", strings.Join(problems, " | "))
	return errors.New("this Pi does not let PiDash do that. It works where the Pi's user may run \"sudo systemctl " + verb + "\" without a password")
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

// Reboot implements Control.
func (s *System) Reboot(ctx context.Context) error { return s.do(ctx, "reboot") }

// Off implements Control.
func (s *System) Off(ctx context.Context) error { return s.do(ctx, "poweroff") }

// Mock only notes what it was asked, for development (-mock) and tests: a
// computer that develops PiDash should not switch itself off.
type Mock struct {
	mu    sync.Mutex
	Asked []string
	// Fail makes every request fail with this error.
	Fail error
}

func (m *Mock) note(what string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Fail != nil {
		return m.Fail
	}
	m.Asked = append(m.Asked, what)
	return nil
}

// Reboot implements Control.
func (m *Mock) Reboot(context.Context) error { return m.note("reboot") }

// Off implements Control.
func (m *Mock) Off(context.Context) error { return m.note("poweroff") }

// Done returns what was asked so far.
func (m *Mock) Done() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fmt.Sprint(m.Asked)
}
