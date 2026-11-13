package photos

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var quietLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// startService runs a service until the test ends and returns a function that
// waits for a status matching a condition.
func startService(t *testing.T, opt Options) (*Service, func(string, func(Status) bool) Status) {
	t.Helper()
	if opt.CacheDir == "" {
		opt.CacheDir = t.TempDir()
	}
	opt.Log = quietLog
	svc := New(opt)
	ch := make(chan Status, 64)
	svc.OnChange(func(s Status) {
		select {
		case ch <- s:
		default:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	wait := func(what string, ok func(Status) bool) Status {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			if st := svc.Status(); ok(st) {
				return st
			}
			select {
			case <-ch:
			case <-time.After(20 * time.Millisecond):
			case <-deadline:
				t.Fatalf("timed out waiting for %s; status %+v", what, svc.Status())
			}
		}
	}
	return svc, wait
}

func idle(n int) func(Status) bool {
	return func(s Status) bool { return !s.Indexing && s.Count == n }
}

func frameColor(t *testing.T, path string, x, y int) color.Color {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img.At(x, y)
}

func TestChoosingSynologyFoldersBuildsTheSlideshow(t *testing.T) {
	nas, syn := newFakeNAS(t)
	state := filepath.Join(t.TempDir(), "state", "photos.json")
	svc, wait := startService(t, Options{Source: syn, PreferOriginal: true, StatePath: state})

	// Nothing is shown until folders are picked.
	st := wait("initial index", func(s Status) bool { return !s.Indexing })
	if st.Count != 0 || len(st.Selection.Folders) != 0 || st.Source != "Synology Photos" {
		t.Fatalf("initial status = %+v", st)
	}

	// Pick "2024" with subfolders: Holiday has 2 JPEGs and 1 HEIC (the video is skipped).
	if err := svc.SetSelection(Selection{Recursive: true, Folders: []Folder{{ID: "p:2", Name: "2024", Path: "Personal Space / 2024"}}}); err != nil {
		t.Fatal(err)
	}
	st = wait("3 photos", idle(3))
	if st.Error != "" || st.Version == 0 {
		t.Errorf("status = %+v", st)
	}

	// Without subfolders the same choice has no photos of its own.
	svc.SetSelection(Selection{Recursive: false, Folders: []Folder{{ID: "p:2", Name: "2024"}}})
	wait("0 photos", idle(0))

	// Several folders across both libraries.
	svc.SetSelection(Selection{Recursive: true, Folders: []Folder{{ID: "p:3", Name: "Holiday"}, {ID: "s:101", Name: "Family"}, {ID: "p:3", Name: "dup"}}})
	st = wait("4 photos", idle(4))
	if len(st.Selection.Folders) != 2 {
		t.Errorf("duplicate folder was not removed: %+v", st.Selection.Folders)
	}

	version, list := svc.Playlist(3)
	if len(list) != 3 || version != st.Version {
		t.Errorf("playlist: %d items, version %d (status %d)", len(list), version, st.Version)
	}
	if _, all := svc.Playlist(0); len(all) != 4 {
		t.Errorf("full playlist has %d items", len(all))
	}

	// A JPEG is rendered from the full file (red, stored sideways): the result
	// is an upright portrait, centred on a dark backdrop.
	path, err := svc.Image(context.Background(), "p-10")
	if err != nil {
		t.Fatal(err)
	}
	if c := frameColor(t, path, FrameW/2, FrameH/2); !near(c, color.RGBA{220, 30, 30, 255}) {
		t.Errorf("centre = %v, want the red original", c)
	}
	if c := frameColor(t, path, 30, FrameH/2); near(c, color.RGBA{220, 30, 30, 255}) {
		t.Errorf("left edge = %v: a portrait photo should not fill the frame", c)
	}
	// A HEIC cannot be decoded, so Synology's preview (green) is used.
	path, err = svc.Image(context.Background(), "p-13")
	if err != nil {
		t.Fatal(err)
	}
	if c := frameColor(t, path, FrameW/2, FrameH/2); !near(c, color.RGBA{30, 200, 60, 255}) {
		t.Errorf("HEIC centre = %v, want the green preview", c)
	}

	// Rendered frames are cached: asking again does not contact the NAS.
	nas.mu.Lock()
	before := nas.downloads + nas.thumbs
	nas.mu.Unlock()
	if _, err := svc.Image(context.Background(), "p-10"); err != nil {
		t.Fatal(err)
	}
	nas.mu.Lock()
	after := nas.downloads + nas.thumbs
	nas.mu.Unlock()
	if after != before {
		t.Errorf("cached frame caused %d more NAS requests", after-before)
	}

	// Only photos in the chosen folders can be requested.
	if _, err := svc.Image(context.Background(), "p-1000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("photo outside the selection: err = %v, want ErrNotFound", err)
	}

	// The choice survives a restart.
	svc2, wait2 := startService(t, Options{Source: syn, StatePath: state})
	st = wait2("restored selection", idle(4))
	if len(st.Selection.Folders) != 2 || !st.Selection.Recursive {
		t.Errorf("restored selection = %+v", st.Selection)
	}
	_ = svc2
}

func TestPreviewQualityNeverDownloadsFullFiles(t *testing.T) {
	nas, syn := newFakeNAS(t)
	svc, wait := startService(t, Options{Source: syn, PreferOriginal: false})
	svc.SetSelection(Selection{Folders: []Folder{{ID: "p:3", Name: "Holiday"}}})
	wait("3 photos", idle(3))
	path, err := svc.Image(context.Background(), "p-10")
	if err != nil {
		t.Fatal(err)
	}
	if c := frameColor(t, path, FrameW/2, FrameH/2); !near(c, color.RGBA{30, 200, 60, 255}) {
		t.Errorf("centre = %v, want the green preview", c)
	}
	if nas.downloads != 0 {
		t.Errorf("full files downloaded: %d", nas.downloads)
	}
}

func TestSlideshowKeepsRunningWhenTheNASGoesAway(t *testing.T) {
	nas, syn := newFakeNAS(t)
	svc, wait := startService(t, Options{Source: syn})
	sel := Selection{Recursive: true, Folders: []Folder{{ID: "p:3", Name: "Holiday", Path: "Holiday"}}}
	svc.SetSelection(sel)
	st := wait("3 photos", idle(3))
	version := st.Version

	// Password changed on the NAS: the next index fails entirely.
	nas.mu.Lock()
	nas.password = "changed"
	nas.sid += 3
	nas.mu.Unlock()
	svc.SetSelection(sel) // triggers a re-index
	st = wait("error reported", func(s Status) bool { return !s.Indexing && s.Error != "" })
	if st.Count != 3 || st.Version != version {
		t.Errorf("photos were dropped after a failed index: %+v", st)
	}

	// Fixed again: the error clears.
	nas.mu.Lock()
	nas.password = "secret"
	nas.mu.Unlock()
	svc.SetSelection(sel)
	wait("recovered", func(s Status) bool { return !s.Indexing && s.Error == "" && s.Count == 3 })
}

func TestLocalFolderSource(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Pictures")
	put := func(rel string) {
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, jpegBytes(uniform(400, 300, color.RGBA{30, 60, 220, 255})), 0o644)
	}
	put("a.jpg")
	put("2024/b.JPEG")
	put("2024/trip/c.png.txt") // not a photo
	put("2024/trip/d.jpg")
	put(".hidden/e.jpg")
	put("@eaDir/f.jpg")
	outside := filepath.Join(filepath.Dir(root), "secret.jpg")
	os.WriteFile(outside, []byte("x"), 0o644)

	src := NewLocalDir(root)
	ctx := context.Background()

	top, err := src.Folders(ctx, "")
	if err != nil || len(top) != 1 || top[0].ID != "." || top[0].Name != "Pictures" {
		t.Fatalf("top = %+v, err %v", top, err)
	}
	sub, _ := src.Folders(ctx, ".")
	if len(sub) != 1 || sub[0].ID != "2024" || sub[0].Path != "Pictures / 2024" {
		t.Errorf("subfolders = %+v (hidden and metadata folders must be skipped)", sub)
	}
	for _, bad := range []string{"..", "../", "/etc", "2024/../../x"} {
		if _, err := src.Folders(ctx, bad); err == nil {
			t.Errorf("Folders(%q) should be refused", bad)
		}
		if _, _, err := src.Fetch(ctx, Item{ID: bad}, true); err == nil {
			t.Errorf("Fetch(%q) should be refused", bad)
		}
	}
	if _, _, err := src.Fetch(ctx, Item{ID: "../secret.jpg"}, true); err == nil {
		t.Error("a file outside the root must not be readable")
	}

	// By default everything under the root is shown.
	svc, wait := startService(t, Options{Source: src})
	wait("3 photos", idle(3))
	_, list := svc.Playlist(0)
	path, err := svc.Image(ctx, list[0].Key)
	if err != nil {
		t.Fatal(err)
	}
	if c := frameColor(t, path, FrameW/2, FrameH/2); !near(c, color.RGBA{30, 60, 220, 255}) {
		t.Errorf("centre = %v", c)
	}
	// Narrow it down to one subfolder, without its subfolders.
	svc.SetSelection(Selection{Folders: []Folder{{ID: "2024", Name: "2024"}}})
	wait("1 photo", idle(1))
}

func TestCacheIsTrimmedToItsLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 10; i++ {
		p := filepath.Join(dir, string(rune('a'+i))+".jpg")
		os.WriteFile(p, make([]byte, 1000), 0o600)
		os.Chtimes(p, now.Add(time.Duration(i)*time.Hour), now.Add(time.Duration(i)*time.Hour))
	}
	os.WriteFile(filepath.Join(dir, "keep.txt"), make([]byte, 5000), 0o600)
	svc := New(Options{Source: Mock{}, CacheDir: dir, CacheBytes: 5000, Log: quietLog})
	svc.evict()

	left, _ := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if len(left) != 4 { // trimmed to 90% of 5000 bytes
		t.Errorf("%d frames left, want 4", len(left))
	}
	for _, name := range []string{"a.jpg", "f.jpg"} { // the oldest ones went first
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("%s should have been removed", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "j.jpg")); err != nil {
		t.Error("the most recently used frame was removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Error("unrelated files must not be touched")
	}
}

func TestMockSourceRenders(t *testing.T) {
	svc, wait := startService(t, Options{Source: Mock{}})
	wait("sample photos", func(s Status) bool { return !s.Indexing && s.Count > 0 })
	_, list := svc.Playlist(2)
	for _, it := range list {
		if _, err := svc.Image(context.Background(), it.Key); err != nil {
			t.Errorf("%s: %v", it.Key, err)
		}
	}
}
