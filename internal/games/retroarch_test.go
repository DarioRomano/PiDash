package games

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// retroHome builds a home folder with a RetroArch that has two favourites
// and a history, and returns it.
func retroHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "retroarch")
	roms := filepath.Join(home, "ROMs")
	gba := filepath.Join(cfg, "cores", "mgba_libretro.so")
	snes := filepath.Join(cfg, "cores", "snes9x_libretro.so")
	for _, f := range []string{gba, snes, filepath.Join(roms, "Kart.gba"), filepath.Join(roms, "It's a (quest); rm -rf.sfc"), filepath.Join(roms, "pack.zip"), filepath.Join(roms, "Tennis.gba")} {
		write(t, f, "x")
	}
	list := func(items ...map[string]string) string {
		data, _ := json.Marshal(map[string]any{"version": "1.5", "default_core_path": "DETECT", "items": items})
		return string(data)
	}
	write(t, filepath.Join(cfg, "retroarch.cfg"), "video_fullscreen = \"true\"\nthumbnails_directory = \"~/Pictures/covers\"\ncontent_history_path = \"default\"\nplaylist_directory = \":/playlists\"\n")
	write(t, filepath.Join(cfg, "content_favorites.lpl"), list(
		map[string]string{"path": filepath.Join(roms, "Kart.gba"), "label": "Kart: Super Circuit", "core_path": gba, "db_name": "Nintendo - Game Boy Advance.lpl"},
		map[string]string{"path": filepath.Join(roms, "It's a (quest); rm -rf.sfc"), "label": "", "core_path": snes, "db_name": "Nintendo - Super Nintendo Entertainment System.lpl"},
	))
	write(t, filepath.Join(cfg, "playlists", "builtin", "content_history.lpl"), list(
		map[string]string{"path": filepath.Join(roms, "pack.zip") + "#Inner Game.gba", "label": "Inner Game", "core_path": gba, "db_name": "Nintendo - Game Boy Advance.lpl"},
		// Already a favourite: one tile is enough.
		map[string]string{"path": filepath.Join(roms, "Kart.gba"), "label": "Kart: Super Circuit", "core_path": gba, "db_name": "Nintendo - Game Boy Advance.lpl"},
		// No core chosen, a core that is gone, a game that is gone: no tile.
		map[string]string{"path": filepath.Join(roms, "Tennis.gba"), "label": "Tennis", "core_path": "DETECT"},
		map[string]string{"path": filepath.Join(roms, "Tennis.gba"), "label": "Tennis", "core_path": filepath.Join(cfg, "cores", "gone_libretro.so")},
		map[string]string{"path": filepath.Join(roms, "Deleted.gba"), "label": "Deleted", "core_path": gba},
	))
	write(t, filepath.Join(home, "Pictures", "covers", "Nintendo - Game Boy Advance", "Named_Boxarts", "Kart_ Super Circuit.png"), "png")
	return home
}

var retroApp = config.App{ID: "retroarch", Name: "RetroArch", Command: "retroarch -f", Mode: config.AppModeConsole, Icon: "retroarch"}

func TestRetroArchGamesBecomeTiles(t *testing.T) {
	home := retroHome(t)
	tiles, files := RetroTiles(home, retroApp)
	if len(files) != 2 {
		t.Errorf("lists read: %v", files)
	}
	var names []string
	for _, tile := range tiles {
		names = append(names, tile.Name)
		if !strings.HasPrefix(tile.ID, Prefix+"ra-") || tile.Mode != config.AppModeConsole {
			t.Errorf("tile %+v", tile)
		}
	}
	if got := strings.Join(names, " | "); got != "Kart: Super Circuit | It's a (quest); rm -rf | Inner Game" {
		t.Fatalf("tiles = %s", got)
	}
	roms, cores := filepath.Join(home, "ROMs"), filepath.Join(home, ".config", "retroarch", "cores")
	if want := "retroarch -f -L '" + cores + "/mgba_libretro.so' '" + roms + "/Kart.gba'"; tiles[0].Command != want {
		t.Errorf("command = %s\n     want %s", tiles[0].Command, want)
	}
	// A name full of things a shell would act on stays one word.
	if want := `'` + roms + `/It'\''s a (quest); rm -rf.sfc'`; !strings.HasSuffix(tiles[1].Command, want) {
		t.Errorf("command = %s\n want it to end in %s", tiles[1].Command, want)
	}
	if !strings.HasSuffix(tiles[2].Command, "/pack.zip#Inner Game.gba'") {
		t.Errorf("a game inside an archive: %s", tiles[2].Command)
	}
	// The cover, where RetroArch has one; its own icon otherwise.
	if !strings.HasSuffix(tiles[0].Icon, "Named_Boxarts/Kart_ Super Circuit.png") || tiles[1].Icon != "retroarch" {
		t.Errorf("icons = %q, %q", tiles[0].Icon, tiles[1].Icon)
	}
	// The same game is the same tile every time.
	again, _ := RetroTiles(home, retroApp)
	if again[0].ID != tiles[0].ID || tiles[0].ID == tiles[2].ID {
		t.Errorf("ids: %s, %s, %s", tiles[0].ID, again[0].ID, tiles[2].ID)
	}

	// Installed as a Flatpak, and an old list written as plain lines.
	flat := t.TempDir()
	dir := filepath.Join(flat, ".var", "app", "org.libretro.RetroArch", "config", "retroarch")
	write(t, filepath.Join(dir, "cores", "a_libretro.so"), "x")
	write(t, filepath.Join(flat, "game.bin"), "x")
	write(t, filepath.Join(dir, "content_history.lpl"), "/old/game.bin\nOld\nDETECT\nDETECT\nDETECT\nOld.lpl\n")
	write(t, filepath.Join(dir, "content_favorites.lpl"), `{"items":[{"path":"`+filepath.Join(flat, "game.bin")+`","label":"Flat","core_path":"`+filepath.Join(dir, "cores", "a_libretro.so")+`"}]}`)
	if tiles, _ := RetroTiles(flat, config.App{Command: "flatpak run org.libretro.RetroArch -f"}); len(tiles) != 1 || tiles[0].Name != "Flat" || !strings.HasPrefix(tiles[0].Command, "flatpak run org.libretro.RetroArch -f -L '") {
		t.Errorf("flatpak tiles = %+v", tiles)
	}
	if tiles, _ := RetroTiles(t.TempDir(), retroApp); len(tiles) != 0 {
		t.Errorf("no RetroArch, but tiles: %+v", tiles)
	}
}

func TestWhichAppIsRetroArch(t *testing.T) {
	for cmd, want := range map[string]bool{"retroarch -f": true, "flatpak run org.libretro.RetroArch": true, "/usr/games/RetroArch": true, "moonlight-qt": false, "": false} {
		if got := IsRetroArch(config.App{Command: cmd}); got != want {
			t.Errorf("IsRetroArch(%q) = %v", cmd, got)
		}
	}
	if IsRetroArch(config.App{URL: "https://retroarch.com"}) {
		t.Error("a website is not RetroArch")
	}
}

func TestTheGamePlayedLastComesFirst(t *testing.T) {
	home := retroHome(t)
	played := filepath.Join(t.TempDir(), "state", "played.json")
	clock := time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC)
	var told [][]config.App
	lib := &Library{Home: home, RetroArch: retroApp, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), PlayedPath: played,
		Now: func() time.Time { return clock }, OnChange: func(tiles []config.App) { told = append(told, tiles) }}
	order := func(tiles []config.App) string {
		var names []string
		for _, tile := range tiles {
			names = append(names, strings.Fields(tile.Name)[0])
		}
		return strings.Join(names, " ")
	}
	tiles := lib.Load()
	if got := order(tiles); got != "Kart: It's Inner" {
		t.Fatalf("order = %s", got)
	}

	lib.Played(tiles[2].ID)
	clock = clock.Add(time.Hour)
	lib.Played(tiles[1].ID)
	lib.Played("youtube") // no game: nothing happens
	if len(told) != 2 || order(told[1]) != "It's Inner Kart:" {
		t.Fatalf("after playing: told %d times, order %s", len(told), order(told[len(told)-1]))
	}
	if first, ok := lib.Find(tiles[1].ID); !ok || first.Name != tiles[1].Name {
		t.Error("a played game can no longer be found")
	}

	// Remembered after a restart.
	again := &Library{Home: home, RetroArch: retroApp, Log: lib.Log, PlayedPath: played}
	if got := order(again.Load()); got != "It's Inner Kart:" {
		t.Errorf("after a restart: %s", got)
	}
	// Without RetroArch among the apps there are no tiles.
	if tiles := (&Library{Home: home, Log: lib.Log}).Load(); len(tiles) != 0 {
		t.Errorf("tiles without any app: %+v", tiles)
	}
}
