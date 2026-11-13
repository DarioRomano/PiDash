// Package desktop reads the programs installed on the machine from their
// .desktop files, the same entries a desktop's application menu shows.
package desktop

import (
	"bufio"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Entry is one installed program.
type Entry struct {
	// ID is the desktop file id, e.g. "org.kde.kdenlive" or "steam".
	ID string `json:"id"`
	// Name and Comment are what the application menu shows.
	Name    string `json:"name"`
	Comment string `json:"comment,omitempty"`
	// Exec is the entry's command line, still with its %f-style placeholders.
	Exec string `json:"-"`
	// Icon is an icon-theme name or an absolute path.
	Icon string `json:"-"`
}

// DataDirs returns the XDG data directories, the user's first, including
// Flatpak exports. Each may contain "applications", "icons" and "pixmaps".
func DataDirs() []string {
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
	return dirs
}

// List returns the programs a person could start from the application menu,
// sorted by name. Entries that are hidden, need a terminal, or whose program
// is missing are left out. Where two directories define the same id, the
// earlier one wins, as on the desktop.
func List(dataDirs []string) []Entry {
	seen := map[string]bool{}
	var out []Entry
	for _, dir := range dataDirs {
		root := filepath.Join(dir, "applications")
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			// A file in a subdirectory gets an id with dashes: kde/foo.desktop is "kde-foo".
			id := strings.TrimSuffix(strings.ReplaceAll(rel, string(filepath.Separator), "-"), ".desktop")
			if seen[id] {
				return nil
			}
			seen[id] = true // even when unusable: a user's hidden copy masks the system's
			if e, ok := read(path); ok {
				e.ID = id
				out = append(out, e)
			}
			return nil
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Find returns the entry with the given id.
func Find(dataDirs []string, id string) (Entry, bool) {
	for _, e := range List(dataDirs) {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func read(path string) (Entry, bool) {
	file, err := os.Open(path)
	if err != nil {
		return Entry{}, false
	}
	defer file.Close()

	kv := map[string]string{}
	inEntry := false
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry || line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if _, dup := kv[k]; !dup {
			kv[k] = strings.TrimSpace(v)
		}
	}

	if t := kv["Type"]; t != "" && t != "Application" {
		return Entry{}, false
	}
	if kv["Name"] == "" || kv["Exec"] == "" {
		return Entry{}, false
	}
	// Not meant for the menu, or text programs that need a terminal window.
	if kv["NoDisplay"] == "true" || kv["Hidden"] == "true" || kv["Terminal"] == "true" {
		return Entry{}, false
	}
	if try := kv["TryExec"]; try != "" {
		if _, err := exec.LookPath(try); err != nil {
			return Entry{}, false
		}
	}
	return Entry{Name: kv["Name"], Comment: kv["Comment"], Exec: kv["Exec"], Icon: kv["Icon"]}, true
}

// fieldCode matches the placeholders of an Exec line (%f, %U, …), with the
// quotes around one that stands alone, and the escaped percent sign.
var fieldCode = regexp.MustCompile(`"%[a-zA-Z]"|'%[a-zA-Z]'|%[a-zA-Z%]`)

// Command returns the entry's command line for `sh -c`, started without a
// file or address to open. The quoting rules of Exec lines are those of the
// shell for everything programs use in practice.
func (e Entry) Command() string {
	cmd := fieldCode.ReplaceAllStringFunc(e.Exec, func(m string) string {
		if m == "%%" {
			return "%"
		}
		return ""
	})
	return strings.Join(strings.Fields(cmd), " ")
}
