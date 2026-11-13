package apps

import (
	"bufio"
	"embed"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/DarioRomano/PiDash/internal/config"
)

// Icons PiDash ships itself, for web apps that have no installed package to
// take an icon from. They are simple drawings in each service's colours, not
// the services' official artwork; set `icon` in the config to use your own.
//
//go:embed icons/*.svg
var builtinIcons embed.FS

// Icon is a resolved tile image: either a file on disk or embedded data.
type Icon struct {
	Path        string
	Data        []byte
	ContentType string
}

func builtinIcon(name string) (Icon, bool) {
	data, err := builtinIcons.ReadFile("icons/" + name + ".svg")
	if err != nil {
		return Icon{}, false
	}
	return Icon{Data: data, ContentType: "image/svg+xml"}, true
}

// IconFinder locates the icon an installed program ships with, the same way a
// desktop menu does: through its .desktop file and the icon theme directories.
type IconFinder struct {
	// DataDirs are XDG data directories, each possibly containing
	// "applications", "icons" and "pixmaps".
	DataDirs []string
}

// DefaultIconFinder searches the user's and the system's data directories,
// including Flatpak exports.
func DefaultIconFinder() *IconFinder {
	home, _ := os.UserHomeDir()
	dirs := []string{}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		dirs = append(dirs, d)
	} else if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share"))
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "share", "flatpak", "exports", "share"))
	}
	sys := os.Getenv("XDG_DATA_DIRS")
	if sys == "" {
		sys = "/usr/local/share:/usr/share"
	}
	dirs = append(dirs, filepath.SplitList(sys)...)
	dirs = append(dirs, "/var/lib/flatpak/exports/share")
	return &IconFinder{DataDirs: dirs}
}

// Resolve picks the icon for an app:
//
//  1. `icon` in the config: a file path, a built-in name, or an icon-theme name
//  2. a built-in icon matching the app (YouTube, Netflix, Synology Photos)
//  3. the icon of the installed program, via its .desktop file or the icon theme
//  4. a generic globe for other web apps
func (f *IconFinder) Resolve(app config.App) (Icon, bool) {
	if app.Icon != "" {
		if filepath.IsAbs(app.Icon) {
			if fileExists(app.Icon) {
				return Icon{Path: app.Icon}, true
			}
		} else {
			if ic, ok := builtinIcon(app.Icon); ok {
				return ic, true
			}
			if p := f.themeIcon(app.Icon); p != "" {
				return Icon{Path: p}, true
			}
		}
		// A configured icon that cannot be found falls through to the automatic choice.
	}

	if name := builtinNameFor(app); name != "" {
		if ic, ok := builtinIcon(name); ok {
			return ic, true
		}
	}

	if app.Command != "" {
		words := strings.Fields(app.Command)
		if name := f.desktopIconName(app.ID, words); name != "" {
			if filepath.IsAbs(name) && fileExists(name) {
				return Icon{Path: name}, true
			}
			if p := f.themeIcon(name); p != "" {
				return Icon{Path: p}, true
			}
		}
		candidates := []string{app.ID}
		if len(words) > 0 {
			base := filepath.Base(words[0])
			candidates = append(candidates, base, strings.TrimSuffix(base, "-qt"), strings.TrimSuffix(base, "-standalone"))
		}
		for _, c := range candidates {
			if p := f.themeIcon(c); p != "" {
				return Icon{Path: p}, true
			}
		}
	}

	if app.URL != "" {
		return builtinIcon("web")
	}
	return Icon{}, false
}

// builtinNameFor recognises the services PiDash ships an icon for, by app id
// or by the address a web app opens.
func builtinNameFor(app config.App) string {
	id := strings.ToLower(app.ID)
	host, path, query := "", "", ""
	if u, err := url.Parse(app.URL); err == nil && app.URL != "" {
		host, path, query = strings.ToLower(u.Hostname()), strings.ToLower(u.Path), strings.ToLower(u.RawQuery)
	}
	hostIs := func(domain string) bool { return host == domain || strings.HasSuffix(host, "."+domain) }
	switch {
	case id == "youtube" || hostIs("youtube.com"):
		return "youtube"
	case id == "netflix" || hostIs("netflix.com"):
		return "netflix"
	case strings.Contains(id, "synology") || strings.Contains(query, "syno.foto") ||
		(app.URL != "" && (path == "/photo" || strings.HasPrefix(path, "/photo/"))):
		return "synology-photos"
	}
	return ""
}

// desktopIconName finds the .desktop entry that starts the same program as the
// command and returns its Icon= value.
func (f *IconFinder) desktopIconName(appID string, words []string) string {
	if len(words) == 0 {
		return ""
	}
	cmdBase := filepath.Base(words[0])
	for _, dir := range f.DataDirs {
		entries, _ := filepath.Glob(filepath.Join(dir, "applications", "*.desktop"))
		for _, path := range entries {
			id := strings.TrimSuffix(filepath.Base(path), ".desktop")
			execLine, icon := readDesktopEntry(path)
			if icon == "" {
				continue
			}
			execBase := ""
			if fields := strings.Fields(execLine); len(fields) > 0 {
				execBase = filepath.Base(fields[0])
			}
			match := id == appID || id == cmdBase || (execBase == cmdBase && !isLauncher(cmdBase))
			// `flatpak run org.example.App` names the entry in its arguments.
			for _, w := range words[1:] {
				if w == id {
					match = true
				}
			}
			if match {
				return icon
			}
		}
	}
	return ""
}

// isLauncher reports commands that start other programs, where matching on the
// command name alone would pick an unrelated entry.
func isLauncher(base string) bool {
	switch base {
	case "flatpak", "snap", "env", "sh", "bash", "sudo", "gtk-launch":
		return true
	}
	return false
}

func readDesktopEntry(path string) (execLine, icon string) {
	file, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer file.Close()
	inEntry := false
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		if v, ok := strings.CutPrefix(line, "Exec="); ok && execLine == "" {
			execLine = v
		}
		if v, ok := strings.CutPrefix(line, "Icon="); ok && icon == "" {
			icon = v
		}
	}
	return execLine, icon
}

// themeIcon looks an icon name up in the hicolor theme and pixmaps, preferring
// vector artwork, then the largest bitmap.
func (f *IconFinder) themeIcon(name string) string {
	if name == "" || strings.ContainsAny(name, "/\\") {
		return ""
	}
	sizes := []string{"scalable", "512x512", "256x256", "192x192", "128x128", "96x96", "72x72", "64x64", "48x48"}
	for _, dir := range f.DataDirs {
		for _, size := range sizes {
			ext := ".png"
			if size == "scalable" {
				ext = ".svg"
			}
			if p := filepath.Join(dir, "icons", "hicolor", size, "apps", name+ext); fileExists(p) {
				return p
			}
		}
	}
	for _, dir := range f.DataDirs {
		for _, ext := range []string{".svg", ".png"} {
			if p := filepath.Join(dir, "pixmaps", name+ext); fileExists(p) {
				return p
			}
		}
	}
	return ""
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
