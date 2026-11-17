//go:build integration

package integration

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// docker runs the docker program and returns what it printed.
func docker(args ...string) (string, error) {
	out, err := exec.Command("docker", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// needDocker skips the test where no Docker answers.
func needDocker(t *testing.T) {
	t.Helper()
	have(t, "docker")
	if out, err := docker("info", "--format", "{{.ServerVersion}}"); err != nil {
		t.Skipf("Docker does not answer: %s", out)
	}
}

// pictures writes a few photographs, as far as a program reading them can
// tell, into folders: name → how many.
func pictures(t *testing.T, dir string, folders map[string]int) {
	t.Helper()
	n := 0
	for folder, count := range folders {
		if err := os.MkdirAll(filepath.Join(dir, folder), 0o755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < count; i++ {
			n++
			// With grain, as a camera's picture has: one without is a few
			// thousand bytes, which PiDash takes for an icon and leaves out.
			img := image.NewRGBA(image.Rect(0, 0, 800, 600))
			grain := uint32(n)*2654435761 + 1
			for y := 0; y < 600; y++ {
				for x := 0; x < 800; x++ {
					grain = grain*1664525 + 1013904223
					g := uint8(grain >> 27)
					img.Set(x, y, color.RGBA{uint8(40*n) + g, uint8(x/4) + g, uint8(y/3) + g, 255})
				}
			}
			f, err := os.Create(filepath.Join(dir, folder, fmt.Sprintf("IMG_%04d.jpg", n)))
			if err != nil {
				t.Fatal(err)
			}
			if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 80}); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
	// Read by another user inside a container.
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil {
			if info.IsDir() {
				_ = os.Chmod(p, 0o755)
			} else {
				_ = os.Chmod(p, 0o644)
			}
		}
		return nil
	})
}

// published is the address at which a container's port is reached from here.
func published(t *testing.T, container, port string) string {
	t.Helper()
	out, err := docker("port", container, port)
	if err != nil || out == "" {
		t.Fatalf("the port of %s: %v %s", container, err, out)
	}
	return strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
}

func TestTheContainerImage(t *testing.T) {
	needDocker(t)
	root := repoRoot(t)
	tag := "pidash-it:" + strconv.FormatInt(time.Now().UnixNano(), 36)
	name := "pidash-it-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	// Built as the release builds it. PIDASH_IT_DOCKER_BUILD_ARGS adds what
	// a machine behind a proxy needs for that.
	args := []string{"build", "-q", "-t", tag, "--build-arg", "VERSION=v0.0.9"}
	args = append(args, strings.Fields(os.Getenv("PIDASH_IT_DOCKER_BUILD_ARGS"))...)
	if out, err := docker(append(args, root)...); err != nil {
		t.Fatalf("building the image: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_, _ = docker("rm", "-f", name)
		_, _ = docker("volume", "rm", "-f", name)
		_, _ = docker("rmi", "-f", tag)
	})

	// Small, and not run as root: what the Dockerfile promises.
	if size, _ := docker("image", "inspect", "--format", "{{.Size}}", tag); size != "" {
		if n, _ := strconv.ParseInt(size, 10, 64); n > 80<<20 {
			t.Errorf("the image is %d MB; it was 23", n>>20)
		}
	}
	if user, _ := docker("image", "inspect", "--format", "{{.Config.User}}", tag); user == "" || user == "root" || user == "0" || strings.HasPrefix(user, "0:") {
		t.Errorf("the image runs as %q", user)
	}

	photos := workDir(t)
	pictures(t, photos, map[string]int{"Holiday": 2, "Garden": 1})
	// Nothing but its own folder may be written to: the rest is read-only.
	if out, err := docker("run", "-d", "--name", name, "--read-only", "-p", "127.0.0.1::8080", "-e", "TZ=Europe/Zurich",
		"-v", name+":/data", "-v", photos+":/photos:ro", tag); err != nil {
		t.Fatalf("starting the container: %v\n%s", err, out)
	}
	logs := func() string { out, _ := docker("logs", "--tail", "40", name); return out }
	healthy := func() bool {
		st, _ := docker("inspect", "--format", "{{.State.Health.Status}}", name)
		return st == "healthy"
	}
	eventually(t, "the container reports healthy", 120*time.Second, healthy)
	base := "http://" + published(t, name, "8080/tcp")

	var cfg struct {
		Version    string `json:"version"`
		Standalone bool   `json:"standalone"`
	}
	if err := getJSON(t, base+"/api/config", &cfg); err != nil || cfg.Version != "v0.0.9" || !cfg.Standalone {
		t.Fatalf("the dashboard's settings: %+v %v\n%s", cfg, err, logs())
	}
	// The check the image runs on itself, by hand.
	if out, err := docker("exec", name, "/pidash", "-healthcheck", "-listen", ":8080"); err != nil {
		t.Errorf("the health check: %v %s", err, out)
	}
	// What is of the machine is not offered in a container.
	var apps struct {
		Apps []any `json:"apps"`
	}
	if err := getJSON(t, base+"/api/apps", &apps); err != nil || len(apps.Apps) != 0 {
		t.Errorf("apps in a container: %+v %v", apps, err)
	}
	if code, _ := send(t, http.MethodPost, base+"/api/update/install", "{}"); code < 400 {
		t.Errorf("a container offered to update itself: %d", code)
	}

	// The pictures that were mounted are shown, with nothing set up.
	var st struct {
		Source          string `json:"source"`
		Count           int    `json:"count"`
		IntervalSeconds int    `json:"intervalSeconds"`
	}
	eventually(t, "the mounted pictures are found", 60*time.Second, func() bool {
		return getJSON(t, base+"/api/photos", &st) == nil && st.Count == 3
	})
	if !strings.Contains(st.Source, "/photos") {
		t.Errorf("the photos come from %q", st.Source)
	}
	var list struct {
		Items []struct {
			Key string `json:"key"`
		} `json:"items"`
	}
	if err := getJSON(t, base+"/api/photos/playlist?limit=5", &list); err != nil || len(list.Items) != 3 {
		t.Fatalf("the slideshow: %+v %v", list, err)
	}
	resp, err := http.Get(base + "/photos/" + list.Items[0].Key)
	if err != nil || resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		t.Fatalf("a photo, prepared for the screen: %v %+v\n%s", err, resp, logs())
	}
	resp.Body.Close()

	// What is saved on the settings page is in force at once (the program
	// starts itself anew inside the container) and outlives the container's restart.
	if code, body := send(t, http.MethodPut, base+"/api/settings", `{"photos":{"source":"folder","intervalSeconds":45}}`); code != http.StatusOK {
		t.Fatalf("saving a setting: %d %s", code, body)
	}
	interval := func() int {
		var now struct {
			IntervalSeconds int `json:"intervalSeconds"`
		}
		if getJSON(t, "http://"+published(t, name, "8080/tcp")+"/api/photos", &now) != nil {
			return 0
		}
		return now.IntervalSeconds
	}
	eventually(t, "the saved setting is in force", 30*time.Second, func() bool { return interval() == 45 })
	if restarts, _ := docker("inspect", "--format", "{{.RestartCount}} {{.State.Running}}", name); restarts != "0 true" {
		t.Errorf("applying the setting cost the container its life: %s", restarts)
	}
	if out, err := docker("restart", "-t", "10", name); err != nil {
		t.Fatalf("restarting: %v %s", err, out)
	}
	eventually(t, "it is back after a restart, with the setting", 90*time.Second, func() bool { return interval() == 45 })

	// Asked to stop, it stops: promptly, and without complaint.
	began := time.Now()
	if out, err := docker("stop", "-t", "20", name); err != nil {
		t.Fatalf("stopping: %v %s", err, out)
	}
	if took := time.Since(began); took > 10*time.Second {
		t.Errorf("stopping took %v: it had to be killed", took)
	}
	if code, _ := docker("inspect", "--format", "{{.State.ExitCode}}", name); code != "0" {
		t.Errorf("it ended with exit code %s\n%s", code, logs())
	}
}

func TestPhotosFromARealSambaShare(t *testing.T) {
	needDocker(t)
	const image = "dperson/samba"
	if out, err := docker("image", "inspect", "--format", "{{.Id}}", image); err != nil {
		if out, err = docker("pull", "-q", image); err != nil {
			t.Skipf("the Samba image cannot be fetched: %s", out)
		}
	}
	shared := workDir(t)
	pictures(t, shared, map[string]int{"Alps": 4, "Autumn": 3, "Lake": 2})
	_ = os.WriteFile(filepath.Join(shared, "Alps", "notes.txt"), []byte("not a picture"), 0o644)
	name := "pidash-it-smb-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	// A share for the user "pi", and one that guests may read.
	if out, err := docker("run", "-d", "--name", name, "-p", "127.0.0.1::445", "-v", shared+":/share:ro", image,
		"-u", "pi;secret", "-s", "pictures;/share;yes;yes;no;pi", "-s", "open;/share;yes;yes;yes"); err != nil {
		t.Fatalf("starting Samba: %v\n%s", err, out)
	}
	t.Cleanup(func() { _, _ = docker("rm", "-f", name) })
	host := published(t, name, "445/tcp")
	eventually(t, "Samba listens", 60*time.Second, func() bool {
		conn, err := net.DialTimeout("tcp", host, time.Second)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	})
	t.Run("read by PiDash's own SMB client", func(t *testing.T) { shareAsALibrary(t, host) })
	t.Run("as the photos of a running PiDash", func(t *testing.T) { shareThroughTheApp(t, host) })
}
