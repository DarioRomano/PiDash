package videos

import (
	"bufio"
	"context"
	"crypto/md5" //nolint:gosec // the name mpv gives the file that remembers where a video stopped
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// What was begun and not finished. mpv remembers where each video stopped,
// in a file named after the video; it removes that file when the video is
// played to its end. PiDash keeps beside it which videos were played last,
// so that the ones among them that mpv still remembers can be offered at
// the top: to carry on watching.

// recentKept is how many of the videos played last are remembered.
const recentKept = 40

// played is one video that was started from PiDash.
type played struct {
	Path string    `json:"path"`
	Name string    `json:"name"`
	Size int64     `json:"size,omitempty"`
	Mark string    `json:"mark"` // the name of mpv's file for it
	At   time.Time `json:"at"`
}

func (l *Library) recentFile() string { return filepath.Join(l.Dir, "recent.json") }

// watchMark is the name mpv gives the file that remembers a video.
func watchMark(target string) string {
	sum := md5.Sum([]byte(target)) //nolint:gosec // see the import
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

func (l *Library) recent() []played {
	data, err := os.ReadFile(l.recentFile())
	if err != nil {
		return nil
	}
	var list []played
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

// notePlayed puts a video at the head of those played last.
func (l *Library) notePlayed(e Entry, target string) {
	if l.Dir == "" {
		return
	}
	l.recentMu.Lock()
	defer l.recentMu.Unlock()
	list := []played{{Path: e.Path, Name: e.Name, Size: e.Size, Mark: watchMark(target), At: time.Now()}}
	for _, p := range l.recent() {
		if p.Path != e.Path && len(list) < recentKept {
			list = append(list, p)
		}
	}
	if data, err := json.Marshal(list); err == nil && os.MkdirAll(l.Dir, 0o700) == nil {
		tmp := l.recentFile() + ".tmp"
		if os.WriteFile(tmp, data, 0o600) == nil {
			_ = os.Rename(tmp, l.recentFile())
		}
	}
}

// position reads from mpv's file where a video stopped, in seconds. It
// reports false when mpv does not remember the video.
func (l *Library) position(mark string) (float64, bool) {
	if l.Dir == "" {
		return 0, false
	}
	f, err := os.Open(filepath.Join(l.mpvDir(), "watch_later", mark))
	if err != nil {
		return 0, false
	}
	defer f.Close()
	in := bufio.NewScanner(f)
	for in.Scan() {
		if v, ok := strings.CutPrefix(in.Text(), "start="); ok {
			if at, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && at >= 0 {
				return at, true
			}
		}
	}
	return 0, true // remembered, without a place: from the start
}

// Continue returns the videos that were begun and not finished, the one
// watched last first.
func (l *Library) Continue(_ context.Context) []Entry {
	if name, _ := l.player(); name != "mpv" {
		return nil // only mpv tells where a video stopped
	}
	l.recentMu.Lock()
	list := l.recent()
	l.recentMu.Unlock()
	out := []Entry{}
	for _, p := range list {
		at, ok := l.position(p.Mark)
		if !ok || len(out) >= 8 {
			continue
		}
		e := Entry{Name: p.Name, Path: p.Path, Size: p.Size, Started: true, Position: at}
		l.withCover(&e)
		out = append(out, e)
	}
	return out
}

// withCover gives an entry the address of its cover, where covers can be made.
func (l *Library) withCover(e *Entry) {
	if l.Covers == "" || e.Folder {
		return
	}
	if name, _ := l.coverTool(); name == "" {
		return
	}
	e.Cover = coverKey(e.Path, e.Size)
}
