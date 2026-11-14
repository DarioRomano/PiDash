package power

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func system(answers map[string]error) (*System, *[]string) {
	var calls []string
	return &System{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		calls = append(calls, line)
		if err := answers[line]; err != nil {
			return []byte("Interactive authentication required.\n"), err
		}
		return nil, nil
	}}, &calls
}

func TestAsksSystemdAndFallsBackToSudo(t *testing.T) {
	denied := errors.New("exit status 1")

	// Where the desktop allows it, sudo is not touched.
	s, calls := system(nil)
	if err := s.Reboot(context.Background()); err != nil || strings.Join(*calls, " | ") != "systemctl --no-ask-password reboot" {
		t.Errorf("err %v, calls %v", err, *calls)
	}

	// Where it does not, sudo is tried, without ever asking for a password.
	s, calls = system(map[string]error{"systemctl --no-ask-password poweroff": denied})
	if err := s.Off(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*calls, " | "); got != "systemctl --no-ask-password poweroff | sudo -n systemctl poweroff" {
		t.Errorf("calls = %s", got)
	}

	// Where neither works, that is said in words that help.
	s, _ = system(map[string]error{"systemctl --no-ask-password reboot": denied, "sudo -n systemctl reboot": denied})
	err := s.Reboot(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sudo systemctl reboot") {
		t.Errorf("err = %v", err)
	}
}

func TestOnlyTheTwoVerbsExist(t *testing.T) {
	s, calls := system(nil)
	s.Reboot(context.Background())
	s.Off(context.Background())
	for _, c := range *calls {
		if c != "systemctl --no-ask-password reboot" && c != "systemctl --no-ask-password poweroff" {
			t.Errorf("unexpected command: %s", c)
		}
	}
}
