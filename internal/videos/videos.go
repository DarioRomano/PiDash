// Package videos lets the TV walk through folders of films and home videos,
// on the Pi or on a Synology NAS, and play one with a video player.
package videos

import (
	"context"
	"errors"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// AppID is the id under which the player runs, like an app without a tile.
const AppID = "pidash-video"

// Entry is a folder or a video in a listing.
type Entry struct {
	Name string `json:"name"`
	// Path names it in the next request; what it looks like is the source's own business.
	Path   string `json:"path"`
	Folder bool   `json:"folder"`
	Size   int64  `json:"size,omitempty"`
	// Started says that playing was broken off, and carries on from there;
	// Position where, in seconds.
	Started  bool    `json:"started,omitempty"`
	Position float64 `json:"position,omitempty"`
	// Cover marks a video that can have a cover: the page asks for it by
	// the video's path, and this, which changes with the file, keeps an old
	// picture from being shown for a new file.
	Cover string `json:"cover,omitempty"`
	// subtitle marks a file of subtitles: not listed, but handed to the
	// player. picture marks a picture, which may be a video's cover.
	subtitle bool
	picture  bool
}

// Listing is one folder as the TV shows it.
type Listing struct {
	Title string `json:"title"`
	Path  string `json:"path"`
	// Parent is the folder above; nil at the top.
	Parent  *string `json:"parent,omitempty"`
	Entries []Entry `json:"entries"`
	// Player is the program that plays the videos; empty when none is installed.
	Player string `json:"player"`
	// Continue are the videos that were begun and not finished, the one
	// watched last first. Only in the listing of the top.
	Continue []Entry `json:"continue,omitempty"`
}

// ErrNotFound is returned for a folder or a video that is not there, or not
// inside the folders that were set up.
var ErrNotFound = errors.New("this folder or video is not there any more")

// Source is where the videos are.
type Source interface {
	// List returns what is in a folder: its name, the folder above (top
	// reports that there is none), and its folders, videos and subtitles.
	List(ctx context.Context, folder string) (title, parent string, top bool, entries []Entry, err error)
	// Target returns what the player is given for a file: a path or an address.
	Target(ctx context.Context, file string) (string, error)
	// Above returns the folder a file is in.
	Above(file string) string
}

var videoExt = map[string]bool{
	".mp4": true, ".m4v": true, ".mkv": true, ".mov": true, ".avi": true, ".webm": true, ".mpg": true, ".mpeg": true,
	".ts": true, ".m2ts": true, ".mts": true, ".wmv": true, ".flv": true, ".ogv": true, ".3gp": true, ".divx": true, ".vob": true,
}

var subtitleExt = map[string]bool{".srt": true, ".ass": true, ".ssa": true, ".vtt": true, ".sub": true}

// IsVideo reports whether a file name is that of a video.
func IsVideo(name string) bool { return videoExt[strings.ToLower(path.Ext(name))] }

func isSubtitle(name string) bool { return subtitleExt[strings.ToLower(path.Ext(name))] }

// skipped reports whether a name is one of the things a NAS or a desktop
// keeps in folders for itself.
func skipped(name string) bool {
	return name == "" || name[0] == '.' || name[0] == '@' || name[0] == '#' || strings.EqualFold(name, "System Volume Information") || strings.EqualFold(name, "lost+found")
}

// natural compares names the way people do: "Episode 2" before "Episode 10",
// and without regard to capitals.
func natural(a, b string) bool {
	ra, rb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si, sj := i, j
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na, nb := strings.TrimLeft(string(ra[si:i]), "0"), strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if ra[i] != rb[j] {
			return ra[i] < rb[j]
		}
		i++
		j++
	}
	return len(ra)-i < len(rb)-j
}

// arrange puts folders first and everything in the order of its names.
func arrange(entries []Entry) {
	sort.SliceStable(entries, func(a, b int) bool {
		if entries[a].Folder != entries[b].Folder {
			return entries[a].Folder
		}
		return natural(entries[a].Name, entries[b].Name)
	})
}

// Library is the videos and the program that plays them.
type Library struct {
	Source Source
	// Player is "mpv", "vlc" or another command; empty looks for mpv, then VLC.
	Player string
	// Dir is where PiDash keeps the player's settings and where each video stopped.
	Dir string
	// LookPath finds a program; nil is the usual search.
	LookPath func(string) (string, error)
	// Covers is where the videos' covers are kept; empty for none.
	Covers string
	// Busy reports that a video is playing: no covers are made meanwhile.
	Busy func() bool

	coverOnce sync.Once
	coverTurn chan struct{}
	recentMu  sync.Mutex
}

// List returns one folder, without the subtitle files, and with a mark on
// the videos that were begun.
func (l *Library) List(ctx context.Context, folder string) (Listing, error) {
	title, parent, top, all, err := l.Source.List(ctx, folder)
	if err != nil {
		return Listing{}, err
	}
	out := Listing{Title: title, Path: folder, Entries: []Entry{}}
	if !top {
		out.Parent = &parent
	}
	out.Player, _ = l.player()
	for _, e := range all {
		if e.subtitle || e.picture {
			continue
		}
		if !e.Folder && out.Player == "mpv" {
			if target, err := l.Source.Target(ctx, e.Path); err == nil {
				e.Position, e.Started = l.position(watchMark(target))
			}
		}
		l.withCover(&e)
		out.Entries = append(out.Entries, e)
	}
	if top {
		out.Continue = l.Continue(ctx)
	}
	return out, nil
}

// Play returns the command that plays one video, and the name of the
// player's window.
func (l *Library) Play(ctx context.Context, file string) (argv []string, window string, err error) {
	if !IsVideo(file) {
		return nil, "", ErrNotFound
	}
	// Only what its folder lists is played: that is what was offered.
	_, _, _, siblings, err := l.Source.List(ctx, l.Source.Above(file))
	if err != nil {
		return nil, "", err
	}
	title := ""
	var entry Entry
	for _, e := range siblings {
		if e.Path == file && !e.Folder && !e.subtitle && !e.picture {
			title, entry = strings.TrimSuffix(e.Name, path.Ext(e.Name)), e
		}
	}
	if title == "" {
		return nil, "", ErrNotFound
	}
	target, err := l.Source.Target(ctx, file)
	if err != nil {
		return nil, "", err
	}
	// Subtitles that lie beside the video under the same name.
	var subs []string
	for _, e := range siblings {
		if e.subtitle && strings.HasPrefix(strings.ToLower(e.Name), strings.ToLower(title)) && len(subs) < 8 {
			if sub, err := l.Source.Target(ctx, e.Path); err == nil {
				subs = append(subs, sub)
			}
		}
	}
	name, exe := l.player()
	if name == "" {
		return nil, "", errors.New("no program that plays videos is installed; on the Pi: sudo apt install mpv")
	}
	l.notePlayed(entry, target)
	return l.command(name, exe, target, title, subs)
}
