package videos

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// player returns the program that plays the videos: its kind ("mpv", "vlc"
// or the command that was set) and where it is. Both empty: there is none.
func (l *Library) player() (name, exe string) {
	look := l.LookPath
	if look == nil {
		look = exec.LookPath
	}
	if l.Player != "" {
		fields := strings.Fields(l.Player)
		if exe, err := look(fields[0]); err == nil {
			kind := filepath.Base(fields[0])
			if kind != "mpv" && kind != "vlc" {
				kind = l.Player
			}
			return kind, exe
		}
		return "", ""
	}
	for _, name := range []string{"mpv", "vlc"} {
		if exe, err := look(name); err == nil {
			return name, exe
		}
	}
	return "", ""
}

func (l *Library) mpvDir() string { return filepath.Join(l.Dir, "mpv") }

// ownedMark is the first line of the files PiDash writes for mpv. A file
// without it was changed by hand, and is left alone.
const ownedMark = "# Written by PiDash. Remove this line to keep changes you make here.\n"

// mpvConf makes mpv a TV's video player: full screen, the picture decoded by
// the Pi's video chip, cheap scaling (the Pi's graphics chip is small), and
// a video that was broken off carries on where it stopped.
const mpvConf = ownedMark + `fullscreen=yes
force-window=immediate
hwdec=auto-safe
save-position-on-quit=yes
sub-auto=fuzzy
osd-duration=2500
osd-font-size=40
osd-bar-h=2.5
scale=bilinear
dscale=bilinear
cscale=bilinear
dither=no
hdr-compute-peak=no
`

// mpvKeys are a TV remote's buttons, as they arrive from PiDash: OK is
// Enter, Back is Escape.
const mpvKeys = ownedMark + `ENTER cycle pause
KP_ENTER cycle pause
SPACE cycle pause
PLAYPAUSE cycle pause
PLAY set pause no
PAUSE set pause yes
LEFT seek -10
RIGHT seek 30
UP seek 300
DOWN seek -300
REWIND seek -60
FORWARD seek 60
PREV add chapter -1
NEXT add chapter 1
ESC quit-watch-later
BS quit-watch-later
STOP quit-watch-later
q quit-watch-later
1 cycle sub
2 cycle audio
3 show-progress
i show-progress
0 seek 0 absolute
`

// writeOwned writes one of PiDash's files for mpv, unless someone made it their own.
func writeOwned(path, content string) error {
	if old, err := os.ReadFile(path); err == nil && (string(old) == content || !strings.HasPrefix(string(old), ownedMark)) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// command builds the player's command line.
func (l *Library) command(name, exe, target, title string, subs []string) ([]string, string, error) {
	switch name {
	case "mpv":
		dir := l.mpvDir()
		if err := writeOwned(filepath.Join(dir, "mpv.conf"), mpvConf); err != nil {
			return nil, "", err
		}
		if err := writeOwned(filepath.Join(dir, "input.conf"), mpvKeys); err != nil {
			return nil, "", err
		}
		// Where each video stopped is kept beside the settings; newer
		// versions of mpv would put it elsewhere.
		argv := []string{exe, "--config-dir=" + dir, "--watch-later-directory=" + filepath.Join(dir, "watch_later"), "--no-terminal", "--force-media-title=" + title}
		for _, s := range subs {
			argv = append(argv, "--sub-file="+s)
		}
		return append(argv, "--", target), "mpv", nil
	case "vlc":
		argv := []string{exe, "--fullscreen", "--play-and-exit", "--no-video-title-show", "--no-qt-privacy-ask", "--qt-continue=2"}
		for _, s := range subs {
			argv = append(argv, "--sub-file="+s)
		}
		return append(argv, target), "vlc", nil
	}
	// Another program: the video is its last argument.
	fields := strings.Fields(name)
	return append(append([]string{exe}, fields[1:]...), target), filepath.Base(fields[0]), nil
}
