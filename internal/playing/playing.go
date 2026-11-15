// Package playing knows what music is playing on the Pi, and can pause it
// or skip to the next piece.
//
// Players on Linux announce themselves on the desktop's message bus in a
// common form (MPRIS): browsers playing Spotify or YouTube, Spotify Connect
// and AirPlay receivers, media players. This package listens there. It does
// not ask again and again: the bus says when something changes.
package playing

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// Track is what is playing.
type Track struct {
	// Player is the program that plays it, as it names itself.
	Player string `json:"player"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	// Playing is false while it is paused.
	Playing bool `json:"playing"`
	// Art is where the page finds the cover: an address on the internet, or
	// one on PiDash itself for a picture that is a file on the Pi. Empty
	// when there is none.
	Art string `json:"art,omitempty"`
	// CanSkip says that "next" and "previous" do something.
	CanSkip bool `json:"canSkip"`
}

const (
	prefix      = "org.mpris.MediaPlayer2."
	objectPath  = dbus.ObjectPath("/org/mpris/MediaPlayer2")
	playerIface = "org.mpris.MediaPlayer2.Player"
	rootIface   = "org.mpris.MediaPlayer2"
)

// OwnMark is the album name by which PiDash's own dashboard marks the radio
// it plays itself. The dashboard already shows that; it is left out here.
const OwnMark = "PiDash"

// Service follows the players on the session bus.
type Service struct {
	Log *slog.Logger
	// OnChange is called with what is playing whenever that changes; nil
	// when nothing is.
	OnChange func(*Track)

	mu      sync.Mutex
	conn    *dbus.Conn
	current *Track
	// name is the bus name of the player Current came from; artFile the
	// picture on disk that belongs to it, if it is one.
	name    string
	artFile string
}

// Current returns what is playing, or nil.
func (s *Service) Current() *Track {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	t := *s.current
	return &t
}

// ArtFile returns the path of the cover when it is a file on the Pi.
func (s *Service) ArtFile() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.artFile, s.artFile != ""
}

// ErrNothing is returned by Control when no player is there.
var ErrNothing = errors.New("nothing is playing")

// Control presses a button of the player: "playpause", "next" or "previous".
func (s *Service) Control(ctx context.Context, button string) error {
	method, ok := map[string]string{"playpause": "PlayPause", "next": "Next", "previous": "Previous"}[button]
	if !ok {
		return errors.New("unknown button")
	}
	s.mu.Lock()
	conn, name := s.conn, s.name
	s.mu.Unlock()
	if conn == nil || name == "" {
		return ErrNothing
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return conn.Object(name, objectPath).CallWithContext(ctx, playerIface+"."+method, 0).Err
}

// Run follows the players until ctx ends. Without a session bus it tries
// again every half minute: the desktop may start after PiDash.
func (s *Service) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := s.follow(ctx); err != nil && ctx.Err() == nil {
			s.Log.Debug("now playing: not listening", "err", err)
		}
		s.set(nil, "", "")
		select {
		case <-ctx.Done():
		case <-time.After(30 * time.Second):
		}
	}
}

func (s *Service) follow(ctx context.Context) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.conn = nil
		s.mu.Unlock()
	}()

	// A player appearing or going away, and a player saying that something
	// about it changed. The position within a piece is not announced, by
	// design, so this is quiet while music simply plays.
	if err := conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"),
		dbus.WithMatchOption("arg0namespace", strings.TrimSuffix(prefix, "."))); err != nil {
		return err
	}
	if err := conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.DBus.Properties"), dbus.WithMatchMember("PropertiesChanged"),
		dbus.WithMatchObjectPath(objectPath)); err != nil {
		return err
	}
	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)

	s.look(ctx, conn)
	for {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-signals:
			if !ok {
				return errors.New("the session bus went away")
			}
			_ = sig
			// Changes come in bursts (title, then artist, then the cover):
			// let the burst finish, then look once.
			quiet := time.NewTimer(150 * time.Millisecond)
		drain:
			for {
				select {
				case _, ok := <-signals:
					if !ok {
						quiet.Stop()
						return errors.New("the session bus went away")
					}
				case <-quiet.C:
					break drain
				case <-ctx.Done():
					quiet.Stop()
					return nil
				}
			}
			s.look(ctx, conn)
		}
	}
}

// look asks every player what it is doing and settles on one of them.
func (s *Service) look(ctx context.Context, conn *dbus.Conn) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var names []string
	if err := conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		return
	}
	sort.Strings(names)
	type found struct {
		name, art string
		track     Track
	}
	var all []found
	for _, name := range names {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		obj := conn.Object(name, objectPath)
		var props map[string]dbus.Variant
		if err := obj.CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", 0, playerIface).Store(&props); err != nil {
			continue
		}
		status, _ := props["PlaybackStatus"].Value().(string)
		if status != "Playing" && status != "Paused" {
			continue
		}
		meta, _ := props["Metadata"].Value().(map[string]dbus.Variant)
		t := Track{Playing: status == "Playing", Title: text(meta["xesam:title"]), Artist: text(meta["xesam:artist"]), Album: text(meta["xesam:album"])}
		// The dashboard's own radio, and players with nothing to say.
		if t.Album == OwnMark || t.Title == "" {
			continue
		}
		next, _ := props["CanGoNext"].Value().(bool)
		prev, _ := props["CanGoPrevious"].Value().(bool)
		t.CanSkip = next || prev
		if identity, err := obj.GetProperty(rootIface + ".Identity"); err == nil {
			t.Player, _ = identity.Value().(string)
		}
		if t.Player == "" {
			t.Player = strings.TrimPrefix(name, prefix)
		}
		art := ""
		t.Art, art = artFor(text(meta["mpris:artUrl"]))
		all = append(all, found{name, art, t})
	}

	// The one that is playing wins. If none is, the one shown so far stays
	// while it is merely paused, so that it can be started again.
	s.mu.Lock()
	previous := s.name
	s.mu.Unlock()
	var pick *found
	for i := range all {
		if all[i].track.Playing {
			pick = &all[i]
			break
		}
	}
	if pick == nil {
		for i := range all {
			if all[i].name == previous {
				pick = &all[i]
			}
		}
	}
	if pick == nil && len(all) > 0 {
		pick = &all[0]
	}
	if pick == nil {
		s.set(nil, "", "")
		return
	}
	s.set(&pick.track, pick.name, pick.art)
}

func (s *Service) set(t *Track, name, artFile string) {
	s.mu.Lock()
	same := (t == nil && s.current == nil) || (t != nil && s.current != nil && *t == *s.current)
	s.current, s.name, s.artFile = t, name, artFile
	fn := s.OnChange
	s.mu.Unlock()
	if !same && fn != nil {
		if t == nil {
			fn(nil)
			return
		}
		copy := *t
		fn(&copy)
	}
}

// text reads a piece of a player's description: a string, or a list of
// strings (several artists), which are joined.
func text(v dbus.Variant) string {
	switch x := v.Value().(type) {
	case string:
		return strings.TrimSpace(x)
	case []string:
		var parts []string
		for _, p := range x {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// artFor turns a player's cover address into what the page should load, and
// the file behind it when it is one on the Pi. Covers on the internet are
// loaded by the page itself. A file is served by PiDash under an address
// that changes with the file, so that the page notices a new cover.
func artFor(raw string) (forPage, file string) {
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return "", ""
	}
	switch u.Scheme {
	case "https", "http":
		return raw, ""
	case "file":
		if fi, err := os.Stat(u.Path); err != nil || !fi.Mode().IsRegular() || fi.Size() > maxArt {
			return "", ""
		}
		sum := sha1.Sum([]byte(raw))
		return "/api/playing/art?v=" + hex.EncodeToString(sum[:6]), u.Path
	}
	return "", ""
}

// maxArt is the largest cover that is passed on.
const maxArt = 8 << 20
