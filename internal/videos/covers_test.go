package videos

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A picture of one colour, as a PNG.
func picture(t *testing.T, w, h int, c color.Color) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func library(t *testing.T, root string) *Library {
	t.Helper()
	dir := t.TempDir()
	return &Library{Source: &Local{Roots: []string{root}}, Dir: filepath.Join(dir, "videos"), Covers: filepath.Join(dir, "covers"),
		// A player is "installed", and takes no frames: only pictures beside videos become covers.
		LookPath: func(name string) (string, error) {
			if name == "mpv" {
				return "/bin/false", nil
			}
			return "", exec.ErrNotFound
		}}
}

func TestThePictureBesideAVideoIsItsCover(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "Alien.mkv"), "a film")
	put(t, filepath.Join(root, "Alien-poster.png"), picture(t, 1000, 1500, color.RGBA{200, 30, 30, 255}))
	put(t, filepath.Join(root, "Brazil.mkv"), "another")
	put(t, filepath.Join(root, "holiday.jpg"), "not a cover of anything")
	l := library(t, root)
	ctx := context.Background()

	list, err := l.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	// Pictures are not listed; both videos may have a cover.
	if names(list.Entries) != "Alien.mkv | Brazil.mkv" || list.Entries[0].Cover == "" || list.Entries[1].Cover == "" {
		t.Fatalf("listing = %+v", list.Entries)
	}

	file, err := l.Cover(ctx, "0:Alien.mkv")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(file)
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil || img.Bounds().Dx() != 480 || img.Bounds().Dy() != 720 {
		t.Fatalf("the cover is not the poster made smaller: %v, %v", err, img)
	}
	if r, g, _, _ := img.At(240, 360).RGBA(); r>>8 < 170 || g>>8 > 70 {
		t.Errorf("the cover has another colour than the poster: %d %d", r>>8, g>>8)
	}
	// Asked again, it is the one that was kept.
	before, _ := os.Stat(file)
	if again, err := l.Cover(ctx, "0:Alien.mkv"); err != nil || again != file {
		t.Errorf("asked again: %q %v", again, err)
	}
	if after, _ := os.Stat(file); !after.ModTime().Equal(before.ModTime()) {
		t.Error("the cover was made a second time")
	}

	// A video with nothing beside it, where no frame can be taken: none, and
	// that is remembered rather than tried at every look.
	if _, err := l.Cover(ctx, "0:Brazil.mkv"); err != ErrNoCover {
		t.Errorf("a video without a cover: %v", err)
	}
	marks, _ := filepath.Glob(filepath.Join(l.Covers, "*.none"))
	if len(marks) != 1 {
		t.Errorf("that there is none was not noted: %v", marks)
	}
	// A file that was replaced by another of the same name gets a new cover.
	put(t, filepath.Join(root, "Alien.mkv"), "a longer film")
	list, _ = l.List(ctx, "")
	if list.Entries[0].Cover == coverKey("0:Alien.mkv", 6) {
		t.Error("a new file kept the old cover's mark")
	}

	// Not of what is not offered: outside the folders, or not a video.
	for _, bad := range []string{"0:../etc/passwd", "0:holiday.jpg", "0:Nothing.mkv", "9:Alien.mkv", ""} {
		if _, err := l.Cover(ctx, bad); err == nil {
			t.Errorf("a cover of %q", bad)
		}
	}
	// And none are made while a video plays.
	put(t, filepath.Join(root, "Casablanca.mkv"), "x")
	put(t, filepath.Join(root, "Casablanca.png"), picture(t, 200, 300, color.White))
	l.Busy = func() bool { return true }
	if _, err := l.Cover(ctx, "0:Casablanca.mkv"); err != ErrNoCover {
		t.Errorf("while a video plays: %v", err)
	}
	l.Busy = func() bool { return false }
	if _, err := l.Cover(ctx, "0:Casablanca.mkv"); err != nil {
		t.Errorf("after it: %v", err)
	}
}

func TestVideosThatWereBegunAreOfferedToCarryOn(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Films/Alien.mkv", "Films/Brazil.mkv", "Films/Casablanca.mkv", "Dune.mkv"} {
		put(t, filepath.Join(root, name), "x")
	}
	l := library(t, root)
	l.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	ctx := context.Background()

	if list, _ := l.List(ctx, ""); len(list.Continue) != 0 {
		t.Fatalf("before anything was played: %+v", list.Continue)
	}
	// Three are started, one after the other. mpv remembers where two of
	// them stopped; the third was watched to its end.
	remember := func(file, content string) {
		target, _ := l.Source.Target(ctx, file)
		put(t, filepath.Join(l.mpvDir(), "watch_later", watchMark(target)), content)
	}
	for _, file := range []string{"0:Films/Alien.mkv", "0:Films/Brazil.mkv", "0:Dune.mkv"} {
		if _, _, err := l.Play(ctx, file); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	remember("0:Films/Alien.mkv", "start=3753.200000\nvolume=80\n")
	remember("0:Dune.mkv", "# a file without a place\n")

	list, err := l.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Continue) != 2 {
		t.Fatalf("to carry on = %+v", list.Continue)
	}
	// The one watched last first; with where it stopped.
	first, second := list.Continue[0], list.Continue[1]
	if first.Path != "0:Dune.mkv" || !first.Started || first.Position != 0 || first.Name != "Dune.mkv" {
		t.Errorf("first = %+v", first)
	}
	if second.Path != "0:Films/Alien.mkv" || second.Position != 3753.2 || second.Cover == "" {
		t.Errorf("second = %+v", second)
	}
	// In its folder a begun video says the same.
	films, _ := l.List(ctx, "0:Films")
	if len(films.Continue) != 0 {
		t.Errorf("the row is the top's alone: %+v", films.Continue)
	}
	for _, e := range films.Entries {
		if want := e.Name == "Alien.mkv"; e.Started != want || (want && e.Position != 3753.2) {
			t.Errorf("%s: started %v at %v", e.Name, e.Started, e.Position)
		}
	}

	// Played again, it moves to the front; and only once is it listed.
	if _, _, err := l.Play(ctx, "0:Films/Alien.mkv"); err != nil {
		t.Fatal(err)
	}
	if again := l.Continue(ctx); len(again) != 2 || again[0].Path != "0:Films/Alien.mkv" {
		t.Errorf("after playing it again: %+v", again)
	}
	// Watched to the end, mpv forgets it, and so does the row.
	target, _ := l.Source.Target(ctx, "0:Films/Alien.mkv")
	os.Remove(filepath.Join(l.mpvDir(), "watch_later", watchMark(target)))
	if left := l.Continue(ctx); len(left) != 1 || left[0].Path != "0:Dune.mkv" {
		t.Errorf("after it was finished: %+v", left)
	}
	// Another player than mpv does not say where a video stopped.
	l.Player = "vlc"
	if list, _ := l.List(ctx, ""); len(list.Continue) != 0 {
		t.Errorf("with VLC: %+v", list.Continue)
	}
}

// With mpv and ffmpeg on the machine, the real thing: a frame out of a
// video that ffmpeg makes.
func TestAFrameOfTheVideoIsItsCover(t *testing.T) {
	ffmpeg, errF := exec.LookPath("ffmpeg")
	if _, errM := exec.LookPath("mpv"); errM != nil || errF != nil {
		t.Skip("mpv and ffmpeg are not both installed")
	}
	root := t.TempDir()
	video := filepath.Join(root, "Test.mp4")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=12:duration=20",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", video).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make the sample video here: %v %s", err, out)
	}
	for _, tool := range []string{"mpv", "ffmpeg"} {
		l := library(t, root)
		l.LookPath = func(name string) (string, error) {
			if name != tool {
				return "", exec.ErrNotFound
			}
			return exec.LookPath(name)
		}
		file, err := l.Cover(context.Background(), "0:Test.mp4")
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		data, _ := os.ReadFile(file)
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil || img.Bounds().Dx() != 480 || img.Bounds().Dy() != 270 {
			t.Errorf("%s: the cover is not a frame of the video: %v %v", tool, err, img)
		}
		if left, _ := filepath.Glob(filepath.Join(l.Covers, "frame-*")); len(left) != 0 {
			t.Errorf("%s left its work lying about: %v", tool, strings.Join(left, " "))
		}
	}
}
