//go:build integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A machine to install PiDash on: a home of its own, and stand-ins for the
// programs that would change the real machine (sudo, systemctl) or that a
// desktop has and a test machine has not (the browsers, the TV's tool).
type machine struct {
	t    *testing.T
	work string
	home string
	env  []string
}

// The stand-in for systemctl starts and stops the installed backend the
// way the user service would, and notes what it was asked.
const pretendSystemctl = `#!/bin/bash
echo "systemctl $*" >> "$IT_WORK/systemctl.log"
stop() {
  if [ -s "$IT_WORK/backend.pid" ]; then
    pid="$(cat "$IT_WORK/backend.pid")"
    kill "$pid" 2>/dev/null
    for _ in $(seq 1 50); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
    rm -f "$IT_WORK/backend.pid"
  fi
}
start() {
  stop
  # As the service file does: the program, with the user's settings.
  nohup "$HOME/.local/share/pidash/pidash" -config "$HOME/.config/pidash/config.toml" >> "$IT_WORK/backend.log" 2>&1 &
  echo $! > "$IT_WORK/backend.pid"
}
case "$*" in
  "--user restart pidash.service") start ;;
  "--user disable --now "*pidash.service*) stop ;;
  *is-enabled*|*is-active*) exit 1 ;;
esac
exit 0
`

func newMachine(t *testing.T, releases string) *machine {
	t.Helper()
	m := &machine{t: t, work: workDir(t)}
	m.home = filepath.Join(m.work, "home")
	bin := filepath.Join(m.work, "bin")
	for _, d := range []string{m.home, bin, filepath.Join(m.work, "run"), filepath.Join(m.work, "tmp")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stand := func(name, script string) {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Nothing here may change the machine the test runs on.
	stand("sudo", "#!/bin/sh\necho \"sudo $*\" >> \"$IT_WORK/sudo.log\"\nexit 0\n")
	stand("systemctl", pretendSystemctl)
	// Without this the update would be handed to the real systemd, outside
	// of what the test can see; PiDash then starts it itself.
	stand("systemd-run", "#!/bin/sh\nexit 1\n")
	for _, name := range []string{"chromium", "firefox", "cec-ctl", "pactl", "wlr-randr"} {
		stand(name, "#!/bin/sh\nexit 0\n")
	}
	stand("fc-list", "#!/bin/sh\necho '/usr/share/fonts/opentype/inter/Inter.otf: Inter'\n")
	m.env = []string{
		"HOME=" + m.home, "USER=tester", "LANG=C",
		"PATH=" + bin + ":/usr/local/bin:/usr/bin:/bin",
		"XDG_RUNTIME_DIR=" + filepath.Join(m.work, "run"),
		"TMPDIR=" + filepath.Join(m.work, "tmp"),
		"IT_WORK=" + m.work,
		// Releases come from the test's own server, never from GitHub.
		"PIDASH_BASE_URL=" + releases + "/latest/download",
		"PIDASH_LATEST_URL=" + releases + "/releases/latest",
	}
	handOver(t, m.work)
	t.Cleanup(func() {
		if pid, err := os.ReadFile(filepath.Join(m.work, "backend.pid")); err == nil {
			_ = exec.Command("kill", strings.TrimSpace(string(pid))).Run()
		}
	})
	return m
}

// run runs a command on the machine as its user, and returns what it
// printed and whether it ended well.
func (m *machine) run(argv ...string) (string, error) {
	m.t.Helper()
	full := asUser(m.t, argv...)
	cmd := exec.Command(full[0], full[1:]...)
	cmd.Env = m.env
	cmd.Dir = m.work
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (m *machine) app(name string) string {
	return filepath.Join(m.home, ".local", "share", "pidash", name)
}
func (m *machine) conf(name string) string { return filepath.Join(m.home, ".config", "pidash", name) }

func (m *machine) exists(path string) bool {
	_, err := m.run("test", "-e", path)
	return err == nil
}

func (m *machine) read(path string) string {
	m.t.Helper()
	out, err := m.run("cat", path)
	if err != nil {
		m.t.Fatalf("reading %s: %v %s", path, err, out)
	}
	return out
}

// address is where the installed backend answers: it says so in a file, as
// it may have had to take another port than the one asked for.
func (m *machine) address() string {
	data, err := os.ReadFile(filepath.Join(m.work, "run", "pidash", "url"))
	if err != nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0]), "/")
}

// running is the version of the backend that answers, or "".
func (m *machine) running() string {
	base := m.address()
	if base == "" {
		return ""
	}
	var health struct {
		Version string `json:"version"`
	}
	if err := getJSON(m.t, base+"/healthz", &health); err != nil {
		return ""
	}
	return health.Version
}

// buildRelease makes the package of one version, as the release does.
func buildRelease(t *testing.T, root, into, version, arch string) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "dist.sh"), version)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PIDASH_DIST_DIR="+into, "PIDASH_DIST_ARCHES="+arch)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the package of %s: %v\n%s", version, err, out)
	}
}

func TestInstallUpdateAndUninstall(t *testing.T) {
	have(t, "bash", "curl", "tar", "sha256sum", "setsid")
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "web", "dist", "index.html")); err != nil {
		t.Fatal(`the pages are not built: run "make build" first`)
	}
	arch := packageArch(t)

	// Three releases: one to install, one to update to, and one whose
	// package was damaged on the way.
	releases := workDir(t)
	for _, v := range []string{"v0.0.1", "v0.0.2"} {
		buildRelease(t, root, filepath.Join(releases, v), v, arch)
	}
	pkg := "pidash_linux_" + arch + ".tar.gz"
	bad := filepath.Join(releases, "v0.0.3")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	good, _ := os.ReadFile(filepath.Join(releases, "v0.0.2", pkg))
	sums, _ := os.ReadFile(filepath.Join(releases, "v0.0.2", "SHA256SUMS"))
	_ = os.WriteFile(filepath.Join(bad, pkg), append(good[:len(good)-1:len(good)-1], good[len(good)-1]^0xff), 0o644)
	_ = os.WriteFile(filepath.Join(bad, "SHA256SUMS"), sums, 0o644)

	// The place releases come from, in GitHub's manner: "latest" leads to
	// the newest one, whose files are under ".../latest/download".
	var latest atomic.Value
	latest.Store("v0.0.1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := latest.Load().(string)
		switch {
		case r.URL.Path == "/releases/latest":
			http.Redirect(w, r, "/releases/tag/"+now, http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/latest/download/"):
			http.ServeFile(w, r, filepath.Join(releases, now, filepath.Base(r.URL.Path)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	m := newMachine(t, srv.URL)
	installer := filepath.Join(root, "install.sh")

	// --- Installing, as the one line of the README does -----------------------
	out, err := m.run("bash", installer, "--autostart")
	if err != nil {
		t.Fatalf("installing: %v\n%s", err, out)
	}
	if !strings.Contains(out, "PiDash v0.0.1 is installed.") || strings.Contains(out, "WARNING") {
		t.Errorf("the installer said:\n%s", out)
	}
	for _, f := range []string{m.app("pidash"), m.app("update.sh"), m.app("uninstall.sh"), m.app("kiosk.sh"), m.app("launch.sh"), m.app("doctor.sh"),
		m.conf("config.toml"),
		filepath.Join(m.home, ".config", "systemd", "user", "pidash.service"),
		filepath.Join(m.home, ".config", "systemd", "user", "pidash-kiosk.service"),
		filepath.Join(m.home, ".local", "share", "applications", "pidash.desktop"),
		filepath.Join(m.home, ".config", "autostart", "pidash.desktop")} {
		if !m.exists(f) {
			t.Errorf("not installed: %s", f)
		}
	}
	if got := strings.TrimSpace(m.read(m.app("install-flags"))); got != "--autostart" {
		t.Errorf("the options it was installed with are kept as %q", got)
	}
	// The menu entry starts the launcher where it was installed.
	if entry := m.read(filepath.Join(m.home, ".local", "share", "applications", "pidash.desktop")); !strings.Contains(entry, m.app("launch.sh")) || strings.Contains(entry, "@APP_DIR@") {
		t.Errorf("the menu entry:\n%s", entry)
	}
	// The settings file it wrote is one this version accepts, and is the user's alone.
	if out, err := m.run(m.app("pidash"), "-check-config", "-config", m.conf("config.toml")); err != nil || !strings.Contains(out, "The settings file is fine.") {
		t.Errorf("the installed settings file: %v %s", err, out)
	}
	if mode, _ := m.run("stat", "-c", "%a", m.conf("config.toml")); strings.TrimSpace(mode) != "600" {
		t.Errorf("the settings file's permissions are %s", mode)
	}
	// It was started, and answers.
	eventually(t, "the installed backend answers", 30*time.Second, func() bool { return m.running() == "v0.0.1" })
	if log := m.read(filepath.Join(m.work, "systemctl.log")); !strings.Contains(log, "systemctl --user enable pidash.service") || !strings.Contains(log, "systemctl --user restart pidash.service") {
		t.Errorf("the service was not set up:\n%s", log)
	}
	// One file of the system's is asked for, with sudo: the rule that lets
	// the remote's buttons reach apps. Nothing is written to the system otherwise.
	sudo := m.read(filepath.Join(m.work, "sudo.log"))
	if !strings.Contains(sudo, "/etc/udev/rules.d/70-pidash-uinput.rules") {
		t.Errorf("the rule for the remote's buttons was not installed:\n%s", sudo)
	}
	for _, line := range strings.Split(strings.TrimSpace(sudo), "\n") {
		if !strings.Contains(line, "70-pidash-uinput.rules") && !strings.Contains(line, "udevadm") && !strings.Contains(line, "modprobe uinput") &&
			!strings.Contains(line, "apt-get") && !strings.Contains(line, "dnf install") && !strings.Contains(line, "pacman -S") {
			t.Errorf("the installer asked for more of the system than it says it does: %s", line)
		}
	}
	base := m.address()

	// --- What the user sets must survive what follows ---------------------------
	if _, err := m.run("sh", "-c", "echo '# kept by hand' >> "+m.conf("config.toml")); err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Screen map[string]any `json:"screen"`
	}
	if err := getJSON(t, base+"/api/settings", &settings); err != nil {
		t.Fatal(err)
	}
	if code, body := send(t, http.MethodPut, base+"/api/settings", `{"screen":{"language":"de","nightStart":"","nightEnd":""}}`); code != http.StatusOK {
		t.Fatalf("saving a setting: %d %s", code, body)
	}
	language := func() string {
		var cfg struct {
			Display struct {
				Language string `json:"language"`
			} `json:"display"`
		}
		if getJSON(t, m.address()+"/api/config", &cfg) != nil {
			return ""
		}
		return cfg.Display.Language
	}
	eventually(t, "the saved setting is in force", 30*time.Second, func() bool { return language() == "de" })

	// --- Updating from the settings page's button ------------------------------
	var status struct {
		Current, Latest     string
		Available, Possible bool
		Reason              string
	}
	if code, body := send(t, http.MethodPost, base+"/api/update/check", "{}"); code != http.StatusOK || !strings.Contains(body, `"available":false`) {
		t.Errorf("with nothing newer: %d %s", code, body)
	}
	latest.Store("v0.0.2")
	if code, body := send(t, http.MethodPost, base+"/api/update/check", "{}"); code != http.StatusOK ||
		!strings.Contains(body, `"latest":"v0.0.2"`) || !strings.Contains(body, `"available":true`) || !strings.Contains(body, `"possible":true`) {
		t.Fatalf("looking for a new version: %d %s", code, body)
	}
	_ = status
	if code, body := send(t, http.MethodPost, base+"/api/update/install", "{}"); code != http.StatusOK {
		t.Fatalf("starting the update: %d %s", code, body)
	}
	eventually(t, "the new version runs", 120*time.Second, func() bool { return m.running() == "v0.0.2" })
	if out, _ := m.run(m.app("pidash"), "-version"); strings.TrimSpace(out) != "pidash v0.0.2" {
		t.Errorf("the installed program says %q", out)
	}
	// Everything the user had is as it was.
	if conf := m.read(m.conf("config.toml")); !strings.Contains(conf, "# kept by hand") {
		t.Error("the update replaced the settings file")
	}
	eventually(t, "the saved setting is still in force", 20*time.Second, func() bool { return language() == "de" })
	if got := strings.TrimSpace(m.read(m.app("install-flags"))); got != "--autostart" {
		t.Errorf("after the update the options are %q", got)
	}
	if !m.exists(filepath.Join(m.home, ".config", "autostart", "pidash.desktop")) {
		t.Error("the update dropped the autostart it was installed with")
	}
	if m.address() != base {
		t.Errorf("the backend moved from %s to %s", base, m.address())
	}

	// --- A package that does not match its checksum is not installed -----------
	latest.Store("v0.0.3")
	out, err = m.run("bash", m.app("update.sh"))
	if err == nil || !strings.Contains(out, "does not match its checksum") || !strings.Contains(out, "Nothing was installed") {
		t.Errorf("a damaged package: %v\n%s", err, out)
	}
	if v, _ := m.run(m.app("pidash"), "-version"); strings.TrimSpace(v) != "pidash v0.0.2" || m.running() != "v0.0.2" {
		t.Errorf("after refusing the damaged package: installed %q, running %q", strings.TrimSpace(v), m.running())
	}
	// And a release that is not there says so, and changes nothing.
	latest.Store("v9.9.9")
	out, err = m.run("bash", m.app("update.sh"))
	if err == nil || !strings.Contains(out, "Download failed") {
		t.Errorf("a release that is not there: %v\n%s", err, out)
	}
	latest.Store("v0.0.2")

	// --- Uninstalling keeps the settings; --purge does not ----------------------
	out, err = m.run("bash", m.app("uninstall.sh"))
	if err != nil || !strings.Contains(out, "PiDash removed.") {
		t.Fatalf("uninstalling: %v\n%s", err, out)
	}
	for _, f := range []string{m.app("pidash"), m.app("update.sh"), m.app("uninstall.sh"),
		filepath.Join(m.home, ".config", "systemd", "user", "pidash.service"),
		filepath.Join(m.home, ".local", "share", "applications", "pidash.desktop"),
		filepath.Join(m.home, ".config", "autostart", "pidash.desktop")} {
		if m.exists(f) {
			t.Errorf("left behind: %s", f)
		}
	}
	if !m.exists(m.conf("config.toml")) || !m.exists(m.conf("settings.json")) {
		t.Error("uninstalling without --purge removed the settings")
	}
	if m.running() != "" {
		t.Error("the backend still runs after uninstalling")
	}

	// Installed again, it is as it was set up before.
	if out, err := m.run("bash", installer); err != nil || !strings.Contains(out, "kept existing") {
		t.Fatalf("installing again: %v\n%s", err, out)
	}
	eventually(t, "it runs again, with the settings of before", 30*time.Second, func() bool { return m.running() == "v0.0.2" && language() == "de" })

	out, err = m.run("bash", m.app("uninstall.sh"), "--purge")
	if err != nil {
		t.Fatalf("uninstalling with --purge: %v\n%s", err, out)
	}
	for _, f := range []string{filepath.Join(m.home, ".config", "pidash"), filepath.Join(m.home, ".local", "share", "pidash"), filepath.Join(m.home, ".cache", "pidash")} {
		if m.exists(f) {
			left, _ := m.run("find", f)
			t.Errorf("--purge left behind:\n%s", left)
		}
	}
}
