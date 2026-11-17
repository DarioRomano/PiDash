//go:build integration

// Package integration tries PiDash as it is shipped: the packages the
// release builds, installed by the real installer; the container image; the
// photo sources against real servers. These tests take minutes and need
// more than Go (bash, curl, tar, and Docker for some), so they are not part
// of "go test ./...": run them with "make integration".
package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// repoRoot is the checkout these tests are part of.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("%s is not the repository: %v", dir, err)
	}
	return dir
}

// workDir is a folder for one test, removed afterwards. PIDASH_IT_DIR names
// where it is made, for machines whose usual temporary folder other users
// may not enter (see asUser).
func workDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(os.Getenv("PIDASH_IT_DIR"), "pidash-it-")
	if err != nil {
		t.Fatal(err)
	}
	// With symbolic links resolved: programs report paths that way.
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// packageArch is the name of the release package for this machine.
func packageArch(t *testing.T) string {
	t.Helper()
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	}
	t.Skipf("no release package is tried on %s", runtime.GOARCH)
	return ""
}

// have skips the test where a program it needs is missing.
func have(t *testing.T, programs ...string) {
	t.Helper()
	for _, p := range programs {
		if _, err := exec.LookPath(p); err != nil {
			t.Skipf("%s is not installed", p)
		}
	}
}

// asUser returns the command line that runs something as an ordinary user.
// The installer refuses to run as root, which is what a test is on some
// machines; there it is run as "nobody".
func asUser(t *testing.T, argv ...string) []string {
	t.Helper()
	if os.Geteuid() != 0 {
		return argv
	}
	if _, err := exec.LookPath("setpriv"); err != nil {
		t.Skip("running as root without setpriv: the installer cannot be tried as an ordinary user")
	}
	return append([]string{"setpriv", "--reuid=65534", "--regid=65534", "--clear-groups", "--"}, argv...)
}

// handOver gives a folder to the user the installer runs as.
func handOver(t *testing.T, dir string) {
	t.Helper()
	if os.Geteuid() != 0 {
		return
	}
	if out, err := exec.Command("chown", "-R", "65534:65534", dir).CombinedOutput(); err != nil {
		t.Fatalf("chown: %v %s", err, out)
	}
	if out, err := exec.Command(asUser(t, "test", "-w", dir)[0], asUser(t, "test", "-w", dir)[1:]...).CombinedOutput(); err != nil {
		t.Skipf("an ordinary user cannot reach %s (%v %s): set PIDASH_IT_DIR to a folder others may enter", dir, err, out)
	}
}

// getJSON asks a running PiDash something.
func getJSON(t *testing.T, url string, out any) error {
	t.Helper()
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// send posts or puts JSON, and returns the status and what came back.
func send(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	var buf strings.Builder
	_, _ = fmt.Fprint(&buf, readAll(resp))
	return resp.StatusCode, buf.String()
}

func readAll(resp *http.Response) string {
	data := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		data = append(data, buf[:n]...)
		if err != nil || len(data) > 1<<20 {
			break
		}
	}
	return string(data)
}

// eventually waits until something is so, or fails the test saying what was not.
func eventually(t *testing.T, what string, within time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("never happened within %v: %s", within, what)
}
