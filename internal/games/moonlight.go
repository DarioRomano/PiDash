// Package games turns the games of a PC, as Moonlight knows them, into tiles
// for the app row: one press wakes the PC and starts that game.
//
// Moonlight keeps what it knows in its own settings file: the PCs it is
// paired with and, for each, the list of games the PC offers. PiDash reads
// that file; it never talks to the PC itself, apart from waking it.
package games

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/DarioRomano/PiDash/internal/config"
)

// Host is a PC Moonlight is paired with.
type Host struct {
	Name string
	UUID string
	// Address is where the PC is found in the home network; Port the one
	// its streaming service answers on.
	Address string
	Port    int
	// MAC is the address of its network card, by which it is woken.
	MAC   net.HardwareAddr
	Games []Game
}

// Game is one thing the PC offers to stream: a game, or its desktop.
type Game struct {
	Name string
	ID   int
}

// defaultPort is where a PC's streaming service answers unless told otherwise.
const defaultPort = 47989

// SettingsFiles returns where Moonlight keeps its settings, for a normal
// installation and for one from Flathub, and next to each the folder with
// the cover pictures it has fetched.
func SettingsFiles(home string) [][2]string {
	const org, app = "Moonlight Game Streaming Project", "Moonlight"
	flat := filepath.Join(home, ".var", "app", "com.moonlight_stream.Moonlight")
	return [][2]string{
		{filepath.Join(home, ".config", org, app+".conf"), filepath.Join(home, ".cache", org, app, "boxart")},
		{filepath.Join(flat, "config", org, app+".conf"), filepath.Join(flat, "cache", org, app, "boxart")},
	}
}

// Read returns the PCs in one of Moonlight's settings files, with their games.
func Read(path string) ([]Host, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// The file is made of sections. In the section "hosts" every line reads
	// "1\hostname=…" for the first PC, "1\apps\2\name=…" for its second game.
	// "hostsbackup" holds the same PCs without their games; it fills in
	// whatever "hosts" lacks.
	type raw struct {
		fields map[string]string
		games  map[int]map[string]string
	}
	sections := map[string]map[int]*raw{}
	section := ""
	lines := bufio.NewScanner(f)
	lines.Buffer(make([]byte, 0, 64<<10), 4<<20) // certificates make long lines
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(line[1 : len(line)-1])
			continue
		}
		if section != "hosts" && section != "hostsbackup" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		parts := strings.Split(key, `\`)
		n, err := strconv.Atoi(parts[0])
		if err != nil {
			continue // "size=2"
		}
		if sections[section] == nil {
			sections[section] = map[int]*raw{}
		}
		h := sections[section][n]
		if h == nil {
			h = &raw{fields: map[string]string{}, games: map[int]map[string]string{}}
			sections[section][n] = h
		}
		switch {
		case len(parts) == 2:
			h.fields[strings.ToLower(parts[1])] = value
		case len(parts) == 4 && strings.EqualFold(parts[1], "apps"):
			if g, err := strconv.Atoi(parts[2]); err == nil {
				if h.games[g] == nil {
					h.games[g] = map[string]string{}
				}
				h.games[g][strings.ToLower(parts[3])] = value
			}
		}
	}
	if err := lines.Err(); err != nil {
		return nil, err
	}

	byUUID := map[string]*Host{}
	var order []string
	for _, name := range []string{"hosts", "hostsbackup"} {
		var numbers []int
		for n := range sections[name] {
			numbers = append(numbers, n)
		}
		sort.Ints(numbers)
		for _, n := range numbers {
			r := sections[name][n]
			text := func(key string) string { s, _ := Unescape(r.fields[key]); return strings.TrimSpace(s) }
			uuid := text("uuid")
			if uuid == "" || text("hostname") == "" {
				continue
			}
			h := byUUID[uuid]
			if h == nil {
				h = &Host{UUID: uuid}
				byUUID[uuid] = h
				order = append(order, uuid)
			}
			if h.Name == "" {
				h.Name = text("hostname")
			}
			// Where it is at home; an address typed in by hand is the fallback.
			for _, pair := range [][2]string{{"localaddress", "localport"}, {"manualaddress", "manualport"}} {
				if h.Address == "" && text(pair[0]) != "" {
					h.Address = text(pair[0])
					h.Port, _ = strconv.Atoi(text(pair[1]))
				}
			}
			if h.Port <= 0 || h.Port > 65535 {
				h.Port = defaultPort
			}
			if len(h.MAC) == 0 {
				if _, mac := Unescape(r.fields["mac"]); len(mac) == 6 && !allZero(mac) {
					h.MAC = net.HardwareAddr(mac)
				}
			}
			if len(h.Games) == 0 {
				var ids []int
				for g := range r.games {
					ids = append(ids, g)
				}
				sort.Ints(ids)
				for _, g := range ids {
					get := func(key string) string { s, _ := Unescape(r.games[g][key]); return strings.TrimSpace(s) }
					id, err := strconv.Atoi(get("id"))
					if err != nil || get("name") == "" || get("hidden") == "true" {
						continue
					}
					h.Games = append(h.Games, Game{Name: get("name"), ID: id})
				}
			}
		}
	}
	hosts := make([]Host, 0, len(order))
	for _, uuid := range order {
		hosts = append(hosts, *byUUID[uuid])
	}
	return hosts, nil
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// Unescape reads a value as Qt writes it into a settings file. It returns
// the value as text and, for values that are raw bytes ("@ByteArray(…)"),
// those bytes.
//
// Qt writes plain text as it is, puts quotes around text with commas or
// spaces at its ends, and writes what cannot be printed as "\n", "\x1b"
// and the like.
func Unescape(raw string) (string, []byte) {
	raw = strings.TrimSpace(raw)
	isBytes := false
	switch {
	case strings.HasPrefix(raw, "@ByteArray(") && strings.HasSuffix(raw, ")"):
		raw, isBytes = raw[len("@ByteArray("):len(raw)-1], true
	case strings.HasPrefix(raw, "@String(") && strings.HasSuffix(raw, ")"):
		raw = raw[len("@String(") : len(raw)-1]
	case strings.HasPrefix(raw, "@@"):
		raw = raw[1:]
	}
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		raw = raw[1 : len(raw)-1]
	}
	var text strings.Builder
	var bytes []byte
	put := func(r rune) {
		text.WriteRune(r)
		bytes = append(bytes, byte(r))
	}
	simple := map[byte]rune{'n': '\n', 'r': '\r', 't': '\t', 'a': '\a', 'b': '\b', 'f': '\f', 'v': '\v', '\\': '\\', '"': '"', '\'': '\'', '?': '?'}
	isHex := func(c byte) bool { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c != '\\' || i+1 >= len(raw) {
			if isBytes || c < 0x80 {
				put(rune(c))
				continue
			}
			// Text written as it is, in UTF-8: keep the whole character.
			r := []rune(raw[i:])[0]
			text.WriteRune(r)
			bytes = append(bytes, raw[i:i+len(string(r))]...)
			i += len(string(r)) - 1
			continue
		}
		i++
		switch e := raw[i]; {
		case e == 'x':
			// As many hex digits as follow; Qt makes sure a digit that
			// belongs to the text is not written right behind them.
			j := i + 1
			for j < len(raw) && isHex(raw[j]) {
				j++
			}
			n, _ := strconv.ParseUint(raw[i+1:j], 16, 32)
			put(rune(n))
			i = j - 1
		case e >= '0' && e <= '7':
			j := i
			for j < len(raw) && j < i+3 && raw[j] >= '0' && raw[j] <= '7' {
				j++
			}
			n, _ := strconv.ParseUint(raw[i:j], 8, 32)
			put(rune(n))
			i = j - 1
		default:
			if r, ok := simple[e]; ok {
				put(r)
			} else {
				put(rune(e))
			}
		}
	}
	return text.String(), bytes
}

// Prefix starts the id of every game tile.
const Prefix = "game-"

// TileID is the id of a game's tile: the same for the same game on the same
// PC every time, and made of letters and digits only.
func TileID(h Host, g Game) string {
	sum := sha1.Sum([]byte(h.UUID))
	return fmt.Sprintf("%s%s-%d", Prefix, hex.EncodeToString(sum[:3]), uint32(g.ID))
}

// Tiles returns the games of the PCs as apps for the app row. moonlight is
// the Moonlight app the person has set up: games start the same program, in
// the same way (on the desktop, or on a screen of its own). art is the
// folder with Moonlight's cover pictures.
func Tiles(hosts []Host, moonlight config.App, art string) []config.App {
	var out []config.App
	for _, h := range hosts {
		// Moonlight finds the PC by its name, its id or its address. The id
		// cannot be mistaken for another PC of the same name.
		for _, g := range h.Games {
			app := config.App{
				ID:   TileID(h, g),
				Name: g.Name,
				Mode: moonlight.Mode,
				// The names come from the PC: quoted, so that nothing in
				// them is taken for a command.
				Command: moonlight.Command + " stream " + quote(h.UUID) + " " + quote(g.Name),
				Icon:    moonlight.Icon,
				Window:  moonlight.Window,
			}
			if art != "" {
				if cover := filepath.Join(art, h.UUID, strconv.Itoa(g.ID)+".png"); isFile(cover) {
					app.Icon = cover
				}
			}
			out = append(out, app)
		}
	}
	return out
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// quote makes a word safe to hand to the shell, whatever it contains.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// IsMoonlight reports whether an app is Moonlight itself, by its command.
func IsMoonlight(app config.App) bool {
	if app.URL != "" || app.Command == "" {
		return false
	}
	return strings.Contains(strings.ToLower(app.Command), "moonlight")
}
