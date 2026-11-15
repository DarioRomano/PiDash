package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// A file with everything that is usually asked for when something does not
// work: saved with one press on the status page, to be sent along with the
// question. It holds no passwords: the settings are written the way the
// settings page gets them, which never includes one.

var diagnosticsMu sync.Mutex

// journal is the command that prints the last lines of the services' log; a
// variable so that the tests need no journal.
var journal = func(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "journalctl", "--user", "-u", "pidash", "-u", "pidash-kiosk", "-n", "400", "--no-pager", "-o", "short-iso").CombinedOutput()
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	// What the page knows and the Pi cannot: the screen as the browser has it.
	var req struct {
		Screen string `json:"screen"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048)).Decode(&req)
	// One at a time, and not over and over: each is a file in the home folder.
	if !diagnosticsMu.TryLock() {
		http.Error(w, "a diagnostics file is being written already", http.StatusTooManyRequests)
		return
	}
	defer diagnosticsMu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	var b bytes.Buffer
	section := func(title string) { fmt.Fprintf(&b, "\n=== %s ===\n", title) }
	fmt.Fprintf(&b, "PiDash diagnostics, written %s\n", time.Now().Format(time.RFC1123))
	fmt.Fprintf(&b, "Version: %s (%s/%s)\n", s.version, runtime.GOOS, runtime.GOARCH)
	if len(req.Screen) > 0 && len(req.Screen) < 200 {
		fmt.Fprintf(&b, "Screen, as the browser on the TV has it: %q\n", req.Screen)
	}
	section("Status, with the recent warnings and errors")
	// What the status page shows, as the status page gets it.
	var status capture
	s.handleStatus(&status, r.WithContext(ctx))
	var pretty bytes.Buffer
	if json.Indent(&pretty, status.body.Bytes(), "", "  ") == nil {
		b.Write(pretty.Bytes())
	} else {
		b.Write(status.body.Bytes())
	}
	section("Settings (no passwords)")
	if s.settings != nil {
		enc := json.NewEncoder(&b)
		enc.SetIndent("", "  ")
		_ = enc.Encode(s.settings.View())
	}
	section("Log of the services (last 400 lines)")
	if out, err := journal(ctx); err == nil || len(out) > 0 {
		b.Write(out)
	} else {
		fmt.Fprintln(&b, "could not be read:", err)
	}

	// Only the newest few are kept.
	name := "pidash-diagnostics-" + time.Now().Format("2006-01-02-150405") + ".txt"
	path := filepath.Join(home, name)
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if old, _ := filepath.Glob(filepath.Join(home, "pidash-diagnostics-*.txt")); len(old) > 5 {
		for _, p := range old[:len(old)-5] {
			_ = os.Remove(p)
		}
	}
	s.log.Info("wrote a diagnostics file", "file", path)
	writeJSON(w, struct {
		File string `json:"file"`
	}{path})
}

// capture is a place for a handler to write its answer to.
type capture struct {
	header http.Header
	body   bytes.Buffer
}

func (c *capture) Header() http.Header {
	if c.header == nil {
		c.header = http.Header{}
	}
	return c.header
}
func (c *capture) Write(p []byte) (int, error) { return c.body.Write(p) }
func (c *capture) WriteHeader(int)             {}
