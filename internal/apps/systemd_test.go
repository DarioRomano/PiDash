package apps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DarioRomano/PiDash/internal/config"
)

// fakeSystemd puts stand-in systemctl and systemd-run scripts first in PATH.
// They log their arguments and keep a state file per unit.
func fakeSystemd(t *testing.T) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
echo "$(basename "$0") $*" >> "` + logPath + `"
verb=""; unit=""
for a in "$@"; do
  case "$a" in
    start|stop|is-active) verb="$a" ;;
    --unit=*) unit="${a#--unit=}"; verb=start ;;
    *.service) unit="$a" ;;
  esac
done
case "$verb" in
  start) echo activating > "` + dir + `/$unit" ;;
  stop) rm -f "` + dir + `/$unit" ;;
  is-active) if [ -f "` + dir + `/$unit" ]; then cat "` + dir + `/$unit"; else echo inactive; exit 3; fi ;;
esac
`
	for _, name := range []string{"systemctl", "systemd-run"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func calls(t *testing.T, logPath string) []string {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestSystemdConsoleAppUsesSystemTemplateUnit(t *testing.T) {
	logPath := fakeSystemd(t)
	s := &Systemd{Exe: "/opt/pidash", ConfigPath: "/cfg.toml"}
	app := config.App{ID: "moonlight", Name: "Moonlight", Command: "moonlight-qt", Mode: ModeConsole}
	ctx := context.Background()

	if err := s.Start(ctx, app); err != nil {
		t.Fatal(err)
	}
	// "activating" (the terminal switch is still in progress) counts as running.
	if !s.Active(ctx, app) {
		t.Error("app should be reported active while its unit is activating")
	}
	if err := s.Stop(ctx, app); err != nil {
		t.Fatal(err)
	}
	if s.Active(ctx, app) {
		t.Error("app should be inactive after stop")
	}

	got := calls(t, logPath)
	want := []string{
		"systemctl --no-ask-password --no-block start pidash-console@moonlight.service",
		"systemctl --no-ask-password is-active pidash-console@moonlight.service",
		"systemctl --no-ask-password stop pidash-console@moonlight.service",
		"systemctl --no-ask-password is-active pidash-console@moonlight.service",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, c := range got {
		if strings.Contains(c, "--user") {
			t.Errorf("console apps must use the system manager, got %q", c)
		}
	}
}

func TestSystemdDesktopAppUsesTransientUserUnit(t *testing.T) {
	logPath := fakeSystemd(t)
	s := &Systemd{Exe: "/opt/pidash", ConfigPath: "/cfg.toml"}
	app := config.App{ID: "steamlink", Name: "Steam Link", Command: "steamlink", Mode: ModeDesktop}

	if err := s.Start(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	got := calls(t, logPath)
	last := got[len(got)-1]
	want := "systemd-run --user --collect --quiet --unit=pidash-app-steamlink.service --description=PiDash app: Steam Link -- /opt/pidash -config /cfg.toml -exec-app steamlink"
	if last != want {
		t.Errorf("got  %q\nwant %q", last, want)
	}
	if !s.Active(context.Background(), app) {
		t.Error("desktop app should be active after start")
	}
}

func TestConsoleReadyDependsOnInstalledUnit(t *testing.T) {
	dir := t.TempDir()
	s := &Systemd{ConsoleUnit: filepath.Join(dir, "pidash-console@.service")}
	if s.ConsoleReady() {
		t.Error("console mode should not be ready before setup")
	}
	os.WriteFile(s.ConsoleUnit, nil, 0o644)
	if !s.ConsoleReady() {
		t.Error("console mode should be ready once the unit is installed")
	}
}
