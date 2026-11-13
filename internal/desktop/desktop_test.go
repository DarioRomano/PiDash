package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListShowsWhatTheMenuShows(t *testing.T) {
	user, system := t.TempDir(), t.TempDir()
	entry := func(lines ...string) string {
		return "[Desktop Entry]\nType=Application\n" + strings.Join(lines, "\n") + "\n"
	}

	write(t, system+"/applications/vlc.desktop", entry("Name=VLC media player", "Comment=Play films", "Exec=/usr/bin/vlc --started-from-file %U", "Icon=vlc")+
		"\n[Desktop Action new]\nName=Other\nExec=other\n")
	write(t, system+"/applications/kde/kdenlive.desktop", entry("Name=Kdenlive", "Exec=kdenlive %F"))
	write(t, system+"/applications/htop.desktop", entry("Name=Htop", "Exec=htop", "Terminal=true"))
	write(t, system+"/applications/helper.desktop", entry("Name=Helper", "Exec=helper", "NoDisplay=true"))
	write(t, system+"/applications/gone.desktop", entry("Name=Gone", "Exec=gone", "TryExec=/nonexistent/gone"))
	write(t, system+"/applications/link.desktop", "[Desktop Entry]\nType=Link\nName=A link\nURL=https://example.com\n")
	// The user hid one system program and changed another.
	write(t, system+"/applications/hidden.desktop", entry("Name=Hidden by the user", "Exec=hidden"))
	write(t, user+"/applications/hidden.desktop", entry("Name=Hidden by the user", "Exec=hidden", "Hidden=true"))
	write(t, system+"/applications/games.desktop", entry("Name=Games", "Exec=games"))
	write(t, user+"/applications/games.desktop", entry("Name=Games", "Exec=games --big-picture"))

	var got []string
	for _, e := range List([]string{user, filepath.Join(user, "missing"), system}) {
		got = append(got, e.ID+"="+e.Command())
	}
	want := "games=games --big-picture; kde-kdenlive=kdenlive; vlc=/usr/bin/vlc --started-from-file"
	if s := strings.Join(got, "; "); s != want {
		t.Errorf("got  %s\nwant %s", s, want)
	}
	if e, ok := Find([]string{user, system}, "vlc"); !ok || e.Name != "VLC media player" || e.Icon != "vlc" || e.Comment != "Play films" {
		t.Errorf("Find(vlc) = %+v, %v", e, ok)
	}
	if _, ok := Find([]string{user, system}, "htop"); ok {
		t.Error("a terminal program was offered")
	}
}

func TestCommandDropsPlaceholders(t *testing.T) {
	for exec, want := range map[string]string{
		`steam %U`:                               `steam`,
		`env GDK_BACKEND=x11 app --name=%c "%f"`: `env GDK_BACKEND=x11 app --name=`,
		`sh -c "echo 100%% done"`:                `sh -c "echo 100% done"`,
		`/usr/bin/flatpak run --branch=stable org.example.App @@u %U @@`: `/usr/bin/flatpak run --branch=stable org.example.App @@u @@`,
	} {
		if got := (Entry{Exec: exec}).Command(); got != want {
			t.Errorf("Command(%q) = %q, want %q", exec, got, want)
		}
	}
}
