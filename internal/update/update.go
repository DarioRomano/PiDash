// Package update finds out whether a newer PiDash has been released and
// installs it with the same script a person would run by hand.
package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Status is what the settings page shows.
type Status struct {
	Current string `json:"current"`
	// Latest is the newest release seen, or empty before the first look.
	Latest string `json:"latest,omitempty"`
	// Available is true when Latest is newer than Current.
	Available bool `json:"available"`
	// Auto says new versions are installed without asking.
	Auto bool `json:"auto"`
	// Possible is false when this copy cannot update itself, with Reason.
	Possible bool   `json:"possible"`
	Reason   string `json:"reason,omitempty"`
	// Installing is true from the moment an update was started.
	Installing bool   `json:"installing"`
	Error      string `json:"error,omitempty"`
}

// Options configure the updater.
type Options struct {
	// Current is the running version, e.g. "v0.3.1".
	Current string
	// Repo is "owner/name" on GitHub.
	Repo string
	// Auto installs new versions at night.
	Auto bool
	// Script is the updater to run; empty means update.sh next to the program.
	Script string
	// Busy reports whether something is being watched or played; an update
	// never interrupts that.
	Busy func() bool
	Log  *slog.Logger

	// For tests.
	LatestURL string
	Now       func() time.Time
	Run       func(script string) error
}

// Service checks for releases and installs them.
type Service struct {
	opt Options

	mu         sync.Mutex
	latest     string
	lastErr    string
	installing bool
}

// New returns an updater.
func New(opt Options) *Service {
	if opt.Repo == "" {
		opt.Repo = "DarioRomano/PiDash"
	}
	if opt.Script == "" {
		if exe, err := os.Executable(); err == nil {
			opt.Script = filepath.Join(filepath.Dir(exe), "update.sh")
		}
	}
	if opt.LatestURL == "" {
		opt.LatestURL = "https://github.com/" + opt.Repo + "/releases/latest"
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Busy == nil {
		opt.Busy = func() bool { return false }
	}
	if opt.Run == nil {
		opt.Run = runDetached
	}
	return &Service{opt: opt}
}

// reason says why this copy cannot update itself, or "" when it can.
func (s *Service) reason() string {
	if _, ok := parse(s.opt.Current); !ok {
		return "This is a development build, not a release."
	}
	if fi, err := os.Stat(s.opt.Script); err != nil || fi.IsDir() {
		return "PiDash was not installed with its installer, so it cannot update itself."
	}
	return ""
}

// Status returns the current state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	reason := s.reason()
	return Status{
		Current: s.opt.Current, Latest: s.latest, Available: newer(s.latest, s.opt.Current),
		Auto: s.opt.Auto, Possible: reason == "", Reason: reason, Installing: s.installing, Error: s.lastErr,
	}
}

// Check asks GitHub for the newest release. The "latest" address answers
// with a redirect to that release, whose last part is its version.
func (s *Service) Check(ctx context.Context) (Status, error) {
	client := &http.Client{
		Timeout:       20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.opt.LatestURL, nil)
	if err != nil {
		return s.Status(), err
	}
	resp, err := client.Do(req)
	var tag string
	if err == nil {
		resp.Body.Close()
		tag = path.Base(resp.Header.Get("Location"))
		if _, ok := parse(tag); !ok {
			err = errors.New("no release was found")
		}
	} else {
		err = errors.New("the release page could not be reached")
	}
	s.mu.Lock()
	if err == nil {
		s.latest, s.lastErr = tag, ""
	} else {
		s.lastErr = "Looking for a new version failed: " + err.Error() + "."
	}
	s.mu.Unlock()
	return s.Status(), err
}

// Install starts the update. It returns as soon as the updater runs; the
// updater replaces the program and restarts it.
func (s *Service) Install() error {
	if r := s.reason(); r != "" {
		return errors.New(r)
	}
	s.mu.Lock()
	if s.installing {
		s.mu.Unlock()
		return nil
	}
	s.installing = true
	s.mu.Unlock()
	s.opt.Log.Info("installing an update", "from", s.opt.Current)
	if err := s.opt.Run(s.opt.Script); err != nil {
		s.mu.Lock()
		s.installing, s.lastErr = false, "The update could not be started: "+err.Error()
		s.mu.Unlock()
		return err
	}
	return nil
}

// Run looks for a new release shortly after start and then every few hours.
// With Auto, a new release is installed between three and five at night,
// when nothing is being watched.
func (s *Service) Run(ctx context.Context) {
	if s.reason() != "" {
		return
	}
	wait := 2 * time.Minute
	lastCheck := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 20 * time.Minute
		now := s.opt.Now()
		if now.Sub(lastCheck) >= 6*time.Hour {
			if _, err := s.Check(ctx); err == nil {
				lastCheck = now
			}
		}
		if s.shouldInstall(now) {
			_ = s.Install()
		}
	}
}

func (s *Service) shouldInstall(now time.Time) bool {
	st := s.Status()
	return s.opt.Auto && st.Available && !st.Installing && now.Hour() >= 3 && now.Hour() < 5 && !s.opt.Busy()
}

// runDetached starts the updater outside this program, which the updater is
// about to stop and replace. PIDASH_UNATTENDED tells it nobody can answer a
// password prompt.
func runDetached(script string) error {
	// As a unit of its own it survives the restart of the PiDash service and
	// its output lands in the journal.
	if _, err := exec.LookPath("systemd-run"); err == nil {
		err := exec.Command("systemd-run", "--user", "--collect", "--quiet", "--unit=pidash-update",
			"--setenv=PIDASH_UNATTENDED=1", "--", "bash", script).Run()
		if err == nil {
			return nil
		}
	}
	// Without a systemd session: a process of its own session, which is not
	// stopped along with this program.
	cmd := exec.Command("setsid", "bash", script)
	cmd.Env = append(os.Environ(), "PIDASH_UNATTENDED=1")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("the updater could not be started: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// parse reads a release version like "v1.2.3". Anything else, such as a
// build between releases ("v1.2.3-4-gabc123"), is not one.
func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if !strings.HasPrefix(v, "v") || len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newer reports whether release a is newer than release b.
func newer(a, b string) bool {
	va, ok1 := parse(a)
	vb, ok2 := parse(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}
