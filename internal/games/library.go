package games

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

// Library keeps the game tiles in step with what Moonlight knows.
type Library struct {
	// Home is the person's home folder, where Moonlight's settings are.
	Home string
	// Moonlight is the Moonlight app as set up in PiDash.
	Moonlight config.App
	Log       *slog.Logger
	// OnChange is called with the tiles whenever they are read anew.
	OnChange func([]config.App)
	Wake     Wake

	mu    sync.Mutex
	hosts map[string]Host // tile id -> the PC it runs on
	tiles []config.App
	stamp string
}

// Load reads Moonlight's settings and returns the tiles.
func (l *Library) Load() []config.App {
	var tiles []config.App
	hosts := map[string]Host{}
	stamp := ""
	for _, files := range SettingsFiles(l.Home) {
		fi, err := os.Stat(files[0])
		if err != nil {
			continue
		}
		stamp += files[0] + fi.ModTime().String()
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
	// A PC with a long list would bury the apps.
	const most = 40
	if len(tiles) > most {
		tiles = tiles[:most]
	}
	l.mu.Lock()
	l.hosts, l.tiles, l.stamp = hosts, tiles, stamp
	l.mu.Unlock()
	return tiles
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
// that is no game it does nothing.
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

// Run reads Moonlight's settings again whenever they change: after a PC was
// paired, or Moonlight fetched a new list of games.
func (l *Library) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := ""
		for _, files := range SettingsFiles(l.Home) {
			if fi, err := os.Stat(files[0]); err == nil {
				now += files[0] + fi.ModTime().String()
			}
		}
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
