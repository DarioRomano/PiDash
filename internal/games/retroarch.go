package games

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/DarioRomano/PiDash/internal/config"
)

// RetroArch keeps two lists of its own: the games marked as favourites, and
// the ones played last. Those become tiles; a whole collection of thousands
// of games would not fit in a row.

// retroTiles is the most tiles RetroArch gets.
const retroTiles = 24

// RetroDirs returns where RetroArch may keep its settings: installed the
// usual way, or as a Flatpak.
func RetroDirs(home string) []string {
	return []string{
		filepath.Join(home, ".config", "retroarch"),
		filepath.Join(home, ".var", "app", "org.libretro.RetroArch", "config", "retroarch"),
	}
}

// IsRetroArch reports whether an app is RetroArch itself, by its command.
func IsRetroArch(app config.App) bool {
	if app.URL != "" || app.Command == "" {
		return false
	}
	return strings.Contains(strings.ToLower(app.Command), "retroarch")
}

// retroSettings reads the lines of retroarch.cfg that say where things are:
// `key = "value"`.
func retroSettings(dir, home string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(filepath.Join(dir, "retroarch.cfg"))
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "content_history_path", "content_favorites_path", "playlist_directory", "thumbnails_directory":
		default:
			continue
		}
		// "default" and nothing mean the usual place; ":" starts a path
		// inside RetroArch's own folder, which is no place for these.
		if value == "" || value == "default" || strings.HasPrefix(value, ":") {
			continue
		}
		if strings.HasPrefix(value, "~/") {
			value = filepath.Join(home, value[2:])
		}
		if filepath.IsAbs(value) {
			out[key] = value
		}
	}
	return out
}

// retroList is a playlist file as RetroArch writes it.
type retroList struct {
	DefaultCorePath string      `json:"default_core_path"`
	Items           []retroItem `json:"items"`
}

type retroItem struct {
	Path     string `json:"path"`
	Label    string `json:"label"`
	CorePath string `json:"core_path"`
	DBName   string `json:"db_name"`
}

func readRetroList(path string) (retroList, bool) {
	var list retroList
	data, err := os.ReadFile(path)
	// Old versions wrote plain lines instead; those are left alone.
	if err != nil || json.Unmarshal(data, &list) != nil {
		return list, false
	}
	return list, true
}

// thumbName is how RetroArch names the picture of a game: its title with the
// characters a file name cannot have replaced.
func thumbName(label string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("&*/:`<>?\\|\"", r) {
			return '_'
		}
		return r
	}, label)
}

// retroFiles returns the favourites and history lists of one installation,
// and its folder of pictures.
func retroFiles(dir, home string) (lists []string, thumbs string) {
	set := retroSettings(dir, home)
	find := func(key, name string) string {
		candidates := []string{set[key], filepath.Join(dir, name), filepath.Join(dir, "playlists", "builtin", name), filepath.Join(dir, "playlists", name)}
		if p := set["playlist_directory"]; p != "" {
			candidates = append(candidates, filepath.Join(p, "builtin", name), filepath.Join(p, name))
		}
		for _, c := range candidates {
			if c != "" && isFile(c) {
				return c
			}
		}
		return ""
	}
	// Favourites first: they were chosen, the history only happened.
	for _, l := range []string{find("content_favorites_path", "content_favorites.lpl"), find("content_history_path", "content_history.lpl")} {
		if l != "" {
			lists = append(lists, l)
		}
	}
	thumbs = set["thumbnails_directory"]
	if thumbs == "" {
		thumbs = filepath.Join(dir, "thumbnails")
	}
	return lists, thumbs
}

// RetroTiles returns RetroArch's favourite and last played games as apps for
// the app row, and the files they were read from. retroarch is the RetroArch
// app the person has set up: games start the same program, in the same way.
func RetroTiles(home string, retroarch config.App) (tiles []config.App, files []string) {
	seen := map[string]bool{}
	for _, dir := range RetroDirs(home) {
		lists, thumbs := retroFiles(dir, home)
		for _, file := range lists {
			files = append(files, file)
			list, ok := readRetroList(file)
			if !ok {
				continue
			}
			for _, it := range list.Items {
				core := it.CorePath
				if core == "" || core == "DETECT" {
					core = list.DefaultCorePath
				}
				// A game inside an archive is "archive.zip#game.rom".
				onDisk, _, _ := strings.Cut(it.Path, "#")
				if core == "" || core == "DETECT" || !isFile(core) || !isFile(onDisk) {
					continue
				}
				sum := sha1.Sum([]byte(it.Path + "\x00" + core))
				id := Prefix + "ra-" + hex.EncodeToString(sum[:4])
				if seen[id] {
					continue
				}
				seen[id] = true
				name := strings.TrimSpace(it.Label)
				if name == "" {
					base := filepath.Base(onDisk)
					name = strings.TrimSuffix(base, filepath.Ext(base))
				}
				app := config.App{
					ID: id, Name: name, Mode: retroarch.Mode, Icon: retroarch.Icon, Window: retroarch.Window,
					// The names come from files: quoted, so that nothing in
					// them is taken for a command.
					Command: retroarch.Command + " -L " + quote(core) + " " + quote(it.Path),
				}
				if system := strings.TrimSuffix(it.DBName, ".lpl"); system != "" && it.Label != "" {
					if cover := filepath.Join(thumbs, system, "Named_Boxarts", thumbName(it.Label)+".png"); isFile(cover) {
						app.Icon = cover
					}
				}
				tiles = append(tiles, app)
				if len(tiles) == retroTiles {
					return tiles, files
				}
			}
		}
		if len(tiles) > 0 {
			break // one installation of RetroArch is the one in use
		}
	}
	return tiles, files
}
