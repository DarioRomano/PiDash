package games

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

// Library keeps the game tiles in step with what Moonlight and RetroArch know.
type Library struct {
	// Home is the person's home folder, where both keep their settings.
	Home string
	// Moonlight and RetroArch are the two apps as set up in PiDash; one
	// without a command is not set up, or its games are not wanted.
	Moonlight config.App
	RetroArch config.App
	Log       *slog.Logger
	// OnChange is called with the tiles whenever they are read anew.
	OnChange func([]config.App)
	Wake     Wake
	// PlayedPath is the file that remembers when each game was last started,
	// so that the ones played last come first. Empty: nothing is remembered.
	PlayedPath string
	// Now is the clock, for tests; nil means time.Now.
	Now func() time.Time

	mu     sync.Mutex
	hosts  map[string]Host // tile id -> the PC it runs on
	tiles  []config.App
	stamp  string
	played map[string]int64 // tile id -> when it was last started
}

// most is how many game tiles there are at most: a PC with a long list would
// bury the apps.
const most = 40

// stampOf says when a file was last written, to notice that it changed.
func stampOf(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return path + fi.ModTime().String()
}

// watched returns the stamp of every file the tiles are read from.
func (l *Library) watched() string {
	stamp := ""
	if l.Moonlight.Command != "" {
		for _, files := range SettingsFiles(l.Home) {
			stamp += stampOf(files[0])
		}
	}
	if l.RetroArch.Command != "" {
		for _, dir := range RetroDirs(l.Home) {
			lists, _ := retroFiles(dir, l.Home)
			for _, f := range append(lists, filepath.Join(dir, "retroarch.cfg")) {
				stamp += stampOf(f)
			}
		}
	}
	return stamp
}

// Load reads the lists of Moonlight and RetroArch and returns the tiles, the
// games played last first.
func (l *Library) Load() []config.App {
	var tiles []config.App
	hosts := map[string]Host{}
	stamp := l.watched()
	if l.Moonlight.Command != "" {
		for _, files := range SettingsFiles(l.Home) {
			if _, err := os.Stat(files[0]); err != nil {
				continue
			}
			pcs, err := Read(files[0])
			if err != nil {
				l.Log.Warn("games: cannot read Moonlight's settings", "file", files[0], "err", err)
				continue
			}
			for _, t := range Tiles(pcs, l.Moonlight, files[1]) {
				if _, dup := hosts[t.ID]; !dup {
					tiles = append(tiles, t)
				}
			}
			for _, h := range pcs {
				for _, g := range h.Games {
					hosts[TileID(h, g)] = h
				}
			}
			if len(tiles) > 0 {
				break // one installation of Moonlight is the one in use
			}
		}
	}
	if l.RetroArch.Command != "" {
		retro, _ := RetroTiles(l.Home, l.RetroArch)
		tiles = append(tiles, retro...)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.played == nil {
		l.loadPlayedLocked()
	}
	l.hosts, l.stamp = hosts, stamp
	l.tiles = l.sortedLocked(tiles)
	return append([]config.App(nil), l.tiles...)
}

// sortedLocked puts the games played last first; the others keep the order
// their lists have.
func (l *Library) sortedLocked(tiles []config.App) []config.App {
	sort.SliceStable(tiles, func(a, b int) bool { return l.played[tiles[a].ID] > l.played[tiles[b].ID] })
	if len(tiles) > most {
		tiles = tiles[:most]
	}
	return tiles
}

func (l *Library) loadPlayedLocked() {
	l.played = map[string]int64{}
	if l.PlayedPath == "" {
		return
	}
	if data, err := os.ReadFile(l.PlayedPath); err == nil {
		_ = json.Unmarshal(data, &l.played)
	}
}

// Played notes that a game was started just now, and moves its tile to the
// front. For an id that is no game it does nothing.
func (l *Library) Played(id string) {
	l.mu.Lock()
	known := false
	for _, t := range l.tiles {
		known = known || t.ID == id
	}
	if !known {
		l.mu.Unlock()
		return
	}
	now := time.Now()
	if l.Now != nil {
		now = l.Now()
	}
	if l.played == nil {
		l.loadPlayedLocked()
	}
	l.played[id] = now.UnixMilli()
	// Games that left the lists long ago are forgotten.
	if len(l.played) > 500 {
		for k, at := range l.played {
			if now.UnixMilli()-at > (365 * 24 * time.Hour).Milliseconds() {
				delete(l.played, k)
			}
		}
	}
	l.tiles = l.sortedLocked(l.tiles)
	tiles := append([]config.App(nil), l.tiles...)
	data, err := json.Marshal(l.played)
	path := l.PlayedPath
	l.mu.Unlock()

	if path != "" && err == nil {
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
			if err = os.WriteFile(path+".tmp", data, 0o600); err == nil {
				err = os.Rename(path+".tmp", path)
			}
		}
		if err != nil && l.Log != nil {
			l.Log.Warn("games: cannot remember what was played", "err", err)
		}
	}
	if l.OnChange != nil {
		l.OnChange(tiles)
	}
}

// Find returns the tile with the given id.
func (l *Library) Find(id string) (config.App, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, t := range l.tiles {
		if t.ID == id {
			return t, true
		}
	}
	return config.App{}, false
}

// Prepare wakes the PC a game runs on, if it is asleep, and waits for it.
// onWaking is called with the PC's name if it has to be woken. For an id
// that is no game on a PC it does nothing.
func (l *Library) Prepare(ctx context.Context, id string, onWaking func(pc string)) error {
	l.mu.Lock()
	h, ok := l.hosts[id]
	l.mu.Unlock()
	if !ok {
		return nil
	}
	return l.Wake.Do(ctx, h, 90*time.Second, func() {
		l.Log.Info("games: waking the PC", "pc", h.Name)
		if onWaking != nil {
			onWaking(h.Name)
		}
	})
}

// Run reads the lists again whenever they change: after a PC was paired,
// Moonlight fetched a new list of games, or something was played in RetroArch.
func (l *Library) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := l.watched()
		l.mu.Lock()
		changed := now != l.stamp
		l.mu.Unlock()
		if changed {
			tiles := l.Load()
			if l.OnChange != nil {
				l.OnChange(tiles)
			}
		}
	}
}
