package videos

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // pictures beside videos come in these kinds
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// A cover is the picture on a video's card. It is the picture that lies
// beside the video under its name, as media libraries keep them, or else a
// frame taken from a little way into the video by the player itself. Either
// is made small once and kept; a video without one is not asked again for a
// while.

var pictureExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true}

func isPicture(name string) bool { return pictureExt[strings.ToLower(path.Ext(name))] }

const (
	// coverWidth is how wide a cover is kept; a card is narrower than that.
	coverWidth = 480
	// coversKept is how many covers are kept before the oldest go.
	coversKept = 3000
	// noCoverFor is how long a video that gave no cover is left alone.
	noCoverFor = 24 * time.Hour
)

// ErrNoCover is returned for a video that has no cover and cannot be given one.
var ErrNoCover = errors.New("this video has no cover")

// coverTool returns the program that takes a frame out of a video: mpv,
// which is there wherever videos are played with it, or else ffmpeg.
func (l *Library) coverTool() (name, exe string) {
	look := l.LookPath
	if look == nil {
		look = exec.LookPath
	}
	for _, name := range []string{"mpv", "ffmpeg"} {
		if exe, err := look(name); err == nil {
			return name, exe
		}
	}
	return "", ""
}

// coverKey names the kept cover of a video: by its place and its size, so
// that a file that was replaced gets a new one.
func coverKey(file string, size int64) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s\n%d", file, size)))
	return hex.EncodeToString(sum[:12])
}

// Cover returns the file that holds the cover of a video, making it first
// where it is not there yet. That can take a few seconds; covers are made
// one at a time, and not at all while a video plays.
func (l *Library) Cover(ctx context.Context, file string) (string, error) {
	if l.Covers == "" || !IsVideo(file) {
		return "", ErrNoCover
	}
	// Only of what its folder lists: that is what was offered.
	_, _, _, siblings, err := l.Source.List(ctx, l.Source.Above(file))
	if err != nil {
		return "", err
	}
	var video *Entry
	for i := range siblings {
		if e := &siblings[i]; e.Path == file && !e.Folder && !e.subtitle && !e.picture {
			video = e
		}
	}
	if video == nil {
		return "", ErrNotFound
	}
	kept := filepath.Join(l.Covers, coverKey(file, video.Size)+".jpg")
	if _, err := os.Stat(kept); err == nil {
		return kept, nil
	}
	none := strings.TrimSuffix(kept, ".jpg") + ".none"
	if fi, err := os.Stat(none); err == nil && time.Since(fi.ModTime()) < noCoverFor {
		return "", ErrNoCover
	}
	if l.Busy != nil && l.Busy() {
		return "", ErrNoCover // the Pi has a video to play; later
	}

	// One at a time: taking a frame out of a film is work for a Pi.
	l.coverOnce.Do(func() { l.coverTurn = make(chan struct{}, 1) })
	select {
	case l.coverTurn <- struct{}{}:
		defer func() { <-l.coverTurn }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if _, err := os.Stat(kept); err == nil {
		return kept, nil // made while this request waited its turn
	}
	if err := os.MkdirAll(l.Covers, 0o700); err != nil {
		return "", err
	}

	data, err := l.pictureBeside(ctx, video, siblings)
	if err != nil {
		data, err = l.frameOf(ctx, file)
	}
	if err == nil {
		data, err = shrink(data)
	}
	if err != nil {
		if ctx.Err() == nil {
			_ = os.WriteFile(none, nil, 0o600)
		}
		return "", ErrNoCover
	}
	tmp := kept + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, kept); err != nil {
		return "", err
	}
	l.pruneCovers()
	return kept, nil
}

// pictureBeside returns the picture that lies beside a video under its
// name: "Alien.jpg" or "Alien-poster.jpg" for "Alien.mkv".
func (l *Library) pictureBeside(ctx context.Context, video *Entry, siblings []Entry) ([]byte, error) {
	title := strings.ToLower(strings.TrimSuffix(video.Name, path.Ext(video.Name)))
	for _, e := range siblings {
		if !e.picture {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(e.Name, path.Ext(e.Name)))
		if name != title && name != title+"-poster" && name != title+"-thumb" && name != title+"-fanart" {
			continue
		}
		target, err := l.Source.Target(ctx, e.Path)
		if err != nil {
			continue
		}
		if data, err := fetch(ctx, target, 20<<20); err == nil {
			return data, nil
		}
	}
	return nil, ErrNoCover
}

// fetch reads what a source gave as the place of a file: a path on the Pi,
// or an address PiDash itself answers at.
func fetch(ctx context.Context, target string, most int64) ([]byte, error) {
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		f, err := os.Open(target)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, most))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, most))
}

// frameOf has the player take one frame out of a video, a little way in:
// past the titles, before anything is given away.
func (l *Library) frameOf(ctx context.Context, file string) ([]byte, error) {
	name, exe := l.coverTool()
	if name == "" {
		return nil, ErrNoCover
	}
	target, err := l.Source.Target(ctx, file)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(l.Covers, "frame-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	out := filepath.Join(dir, "00000001.jpg")
	switch name {
	case "mpv":
		// With nothing of anyone's settings, no sound, and the frame nearest
		// to the place rather than the exact one, which is much quicker.
		cmd = exec.CommandContext(ctx, exe, "--no-config", "--really-quiet", "--no-audio", "--vo=image", "--vo-image-format=jpg",
			"--vo-image-jpeg-quality=85", "--vo-image-outdir="+dir, "--frames=1", "--start=12%", "--hr-seek=no", "--hwdec=no",
			fmt.Sprintf("--vf=scale=%d:-2", coverWidth), "--", target)
	default:
		// ffmpeg does not know percentages: two minutes in, or failing that
		// (a short clip) five seconds.
		for _, at := range []string{"120", "5"} {
			cmd = exec.CommandContext(ctx, exe, "-v", "error", "-y", "-ss", at, "-i", target, "-frames:v", "1",
				"-vf", fmt.Sprintf("scale=%d:-2", coverWidth), "-q:v", "4", out)
			if cmd.Run() == nil {
				if data, err := os.ReadFile(out); err == nil && len(data) > 0 {
					return data, nil
				}
			}
		}
		return nil, ErrNoCover
	}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return os.ReadFile(out)
}

// shrink makes a picture as wide as a cover needs to be, and a JPEG.
func shrink(data []byte) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	if b.Dx() < 16 || b.Dy() < 16 {
		return nil, errors.New("too small to be a cover")
	}
	w, h := b.Dx(), b.Dy()
	if w > coverWidth {
		w, h = coverWidth, h*coverWidth/w
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 84}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pruneCovers lets the oldest covers go once there are very many.
func (l *Library) pruneCovers() {
	list, err := os.ReadDir(l.Covers)
	if err != nil || len(list) <= coversKept {
		return
	}
	type kept struct {
		name string
		at   time.Time
	}
	var all []kept
	for _, d := range list {
		if info, err := d.Info(); err == nil && !d.IsDir() {
			all = append(all, kept{d.Name(), info.ModTime()})
		}
	}
	sort.Slice(all, func(a, b int) bool { return all[a].at.Before(all[b].at) })
	for i := 0; i < len(all)-coversKept*9/10; i++ {
		_ = os.Remove(filepath.Join(l.Covers, all[i].name))
	}
}
