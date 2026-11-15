package apps

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DesktopEnv returns env with the variables an app needs to open a window in
// the running desktop session. systemd user services do not always inherit
// them, so the Wayland socket or X display is located directly.
func DesktopEnv(env []string) []string {
	return desktopEnv(env, "/run/user/"+strconv.Itoa(os.Getuid()), "/tmp/.X11-unix", os.Getenv("HOME"))
}

func desktopEnv(env []string, defaultRuntimeDir, x11Dir, home string) []string {
	vars := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vars[k] = v
		}
	}
	set := func(k, v string) {
		if vars[k] == "" {
			vars[k] = v
			env = append(env, k+"="+v)
		}
	}

	set("XDG_RUNTIME_DIR", defaultRuntimeDir)

	if vars["WAYLAND_DISPLAY"] == "" {
		matches, _ := filepath.Glob(filepath.Join(vars["XDG_RUNTIME_DIR"], "wayland-[0-9]*"))
		for _, p := range matches {
			if fi, err := os.Stat(p); err == nil && fi.Mode()&os.ModeSocket != 0 {
				set("WAYLAND_DISPLAY", filepath.Base(p))
				break
			}
		}
	}
	// X display: the session itself on X11, or XWayland for X11-only apps.
	if fi, err := os.Stat(filepath.Join(x11Dir, "X0")); err == nil && fi.Mode()&os.ModeSocket != 0 {
		set("DISPLAY", ":0")
		if xa := filepath.Join(home, ".Xauthority"); vars["WAYLAND_DISPLAY"] == "" {
			if _, err := os.Stat(xa); err == nil {
				set("XAUTHORITY", xa)
			}
		}
	}
	return env
}

// ConsoleEnv returns env without desktop display variables, so toolkits pick
// their direct-to-display backend (KMS/DRM) instead of looking for a compositor.
func ConsoleEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "WAYLAND_DISPLAY", "DISPLAY", "XAUTHORITY":
			continue
		}
		out = append(out, kv)
	}
	return out
}
