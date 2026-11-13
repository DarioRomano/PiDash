package apps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DarioRomano/PiDash/internal/config"
)

func write(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuiltinIconsForWebApps(t *testing.T) {
	f := &IconFinder{}
	cases := []struct {
		app  config.App
		want string // a string unique to the expected built-in SVG
	}{
		{config.App{ID: "youtube", URL: "https://www.youtube.com/tv"}, "#ff0033"},
		{config.App{ID: "yt", URL: "https://m.youtube.com/"}, "#ff0033"},
		{config.App{ID: "netflix", URL: "https://www.netflix.com/browse"}, "#e50914"},
		{config.App{ID: "photos", URL: "http://nas.local:5000/?launchApp=SYNO.Foto.AppInstance"}, "#2a9df4"},
		{config.App{ID: "pics", URL: "https://nas.example.com/photo/"}, "#2a9df4"},
		{config.App{ID: "synology-photos", URL: "http://192.168.1.10:5000/"}, "#2a9df4"},
		{config.App{ID: "news", URL: "https://example.com/"}, "<circle"}, // generic globe
	}
	for _, c := range cases {
		ic, ok := f.Resolve(c.app)
		if !ok || ic.ContentType != "image/svg+xml" || !strings.Contains(string(ic.Data), c.want) {
			t.Errorf("%s (%s): wrong icon (ok=%v)", c.app.ID, c.app.URL, ok)
		}
	}
	// A site that merely mentions a known name is not mistaken for it.
	if ic, _ := f.Resolve(config.App{ID: "x", URL: "https://notyoutube.com.example.org/"}); strings.Contains(string(ic.Data), "#ff0033") {
		t.Error("unrelated host got the YouTube icon")
	}
}

func TestInstalledProgramIconViaDesktopFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "applications", "com.moonlight_stream.Moonlight.desktop"),
		"[Desktop Entry]\nName=Moonlight\nExec=moonlight-qt %u\nIcon=moonlight\n\n[Desktop Action x]\nExec=other\nIcon=wrong\n")
	write(t, filepath.Join(dir, "icons", "hicolor", "64x64", "apps", "moonlight.png"), "png")
	svg := write(t, filepath.Join(dir, "icons", "hicolor", "scalable", "apps", "moonlight.svg"), "<svg/>")
	f := &IconFinder{DataDirs: []string{dir}}

	ic, ok := f.Resolve(config.App{ID: "stream", Command: "moonlight-qt --fullscreen"})
	if !ok || ic.Path != svg {
		t.Errorf("icon = %+v ok=%v, want the scalable theme icon %s", ic, ok, svg)
	}
}

func TestFlatpakAppIconAndAbsoluteIconPath(t *testing.T) {
	dir := t.TempDir()
	png := write(t, filepath.Join(dir, "opt", "kodi.png"), "png")
	write(t, filepath.Join(dir, "applications", "tv.kodi.Kodi.desktop"),
		"[Desktop Entry]\nExec=/usr/bin/flatpak run --branch=stable tv.kodi.Kodi\nIcon="+png+"\n")
	// Another flatpak app must not be picked just because it is also started by "flatpak".
	write(t, filepath.Join(dir, "applications", "org.other.App.desktop"),
		"[Desktop Entry]\nExec=/usr/bin/flatpak run org.other.App\nIcon=other\n")
	write(t, filepath.Join(dir, "icons", "hicolor", "scalable", "apps", "other.svg"), "<svg/>")
	f := &IconFinder{DataDirs: []string{dir}}

	ic, ok := f.Resolve(config.App{ID: "kodi", Command: "flatpak run tv.kodi.Kodi"})
	if !ok || ic.Path != png {
		t.Errorf("icon = %+v ok=%v, want %s", ic, ok, png)
	}
}

func TestIconFallbacks(t *testing.T) {
	dir := t.TempDir()
	pix := write(t, filepath.Join(dir, "pixmaps", "steamlink.png"), "png")
	big := write(t, filepath.Join(dir, "icons", "hicolor", "256x256", "apps", "kodi.png"), "png")
	write(t, filepath.Join(dir, "icons", "hicolor", "48x48", "apps", "kodi.png"), "png")
	custom := write(t, filepath.Join(dir, "my.svg"), "<svg/>")
	f := &IconFinder{DataDirs: []string{dir}}

	// No .desktop file: the theme is searched by command name; largest bitmap wins.
	if ic, ok := f.Resolve(config.App{ID: "media", Command: "kodi-standalone"}); !ok || ic.Path != big {
		t.Errorf("kodi icon = %+v ok=%v, want %s", ic, ok, big)
	}
	if ic, ok := f.Resolve(config.App{ID: "steamlink", Command: "steamlink"}); !ok || ic.Path != pix {
		t.Errorf("steamlink icon = %+v ok=%v, want %s", ic, ok, pix)
	}
	// Explicit settings win: a file path, a built-in name, a theme name.
	if ic, _ := f.Resolve(config.App{ID: "kodi", Command: "kodi", Icon: custom}); ic.Path != custom {
		t.Errorf("explicit path not used: %+v", ic)
	}
	if ic, _ := f.Resolve(config.App{ID: "a", Command: "true", Icon: "netflix"}); !strings.Contains(string(ic.Data), "#e50914") {
		t.Errorf("built-in name not used: %+v", ic)
	}
	if ic, _ := f.Resolve(config.App{ID: "a", Command: "true", Icon: "steamlink"}); ic.Path != pix {
		t.Errorf("theme name not used: %+v", ic)
	}
	// A configured icon that does not exist falls back to the automatic choice.
	if ic, _ := f.Resolve(config.App{ID: "kodi", Command: "kodi", Icon: "/nonexistent.png"}); ic.Path != big {
		t.Errorf("missing explicit icon should fall back: %+v", ic)
	}
	// Nothing known: no icon, the tile shows a letter.
	if _, ok := f.Resolve(config.App{ID: "mystery", Command: "some-tool"}); ok {
		t.Error("unknown program should have no icon")
	}
	// Icon names are never treated as paths.
	if p := f.themeIcon("../../etc/passwd"); p != "" {
		t.Errorf("themeIcon accepted a path: %s", p)
	}
}
