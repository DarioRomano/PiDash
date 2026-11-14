package maps

import (
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTilesAreFetchedOnceAndKept(t *testing.T) {
	var asked, running, most atomic.Int32
	var agent atomic.Value
	down := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		agent.Store(r.Header.Get("User-Agent"))
		if n := running.Add(1); n > most.Load() {
			most.Store(n)
		}
		defer running.Add(-1)
		time.Sleep(20 * time.Millisecond)
		switch {
		case down.Load():
			http.Error(w, "busy", http.StatusServiceUnavailable)
		case strings.HasSuffix(r.URL.Path, "/9.png"):
			w.Write([]byte("<html>please log in</html>"))
		default:
			png.Encode(w, image.NewGray(image.Rect(0, 0, 4, 4)))
		}
	}))
	defer srv.Close()

	tiles := New(srv.URL+"/{z}/{x}/{y}.png", t.TempDir(), "PiDash/test")
	ctx := context.Background()

	// Nine at once, as the page asks for them: two at a time go out.
	done := make(chan error, 9)
	for i := 0; i < 9; i++ {
		go func(i int) {
			_, err := tiles.Tile(ctx, 10, 530+i%3, 360+i/3)
			done <- err
		}(i)
	}
	for i := 0; i < 9; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if asked.Load() != 9 || most.Load() > 2 {
		t.Errorf("%d requests, %d at a time; want 9, at most 2 at a time", asked.Load(), most.Load())
	}
	if agent.Load() != "PiDash/test" {
		t.Errorf("agent = %q", agent.Load())
	}

	path, err := tiles.Tile(ctx, 10, 530, 360)
	if err != nil || asked.Load() != 9 {
		t.Fatalf("a kept tile was asked for again: %v, %d requests", err, asked.Load())
	}
	if _, err := png.Decode(mustOpen(t, path)); err != nil {
		t.Errorf("the kept tile is not a picture: %v", err)
	}

	// Grown old: fetched again; and when that fails, the old one will do.
	old := time.Now().Add(-40 * 24 * time.Hour)
	os.Chtimes(path, old, old)
	if _, err := tiles.Tile(ctx, 10, 530, 360); err != nil || asked.Load() != 10 {
		t.Errorf("an old tile: %v, %d requests, want 10", err, asked.Load())
	}
	os.Chtimes(path, old, old)
	down.Store(true)
	if got, err := tiles.Tile(ctx, 10, 530, 360); err != nil || got != path {
		t.Errorf("with the service down an old tile should do: %v", err)
	}
	if _, err := tiles.Tile(ctx, 10, 1, 1); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("a new tile with the service down: %v", err)
	}
	down.Store(false)

	// Not a picture, and tiles that do not exist.
	if _, err := tiles.Tile(ctx, 10, 3, 9); err == nil || !strings.Contains(err.Error(), "instead of a picture") {
		t.Errorf("a login page instead of a tile: %v", err)
	}
	for _, bad := range [][3]int{{-1, 0, 0}, {3, 8, 0}, {3, 0, 8}, {3, -1, 0}, {18, 5, 5}} {
		if _, err := tiles.Tile(ctx, bad[0], bad[1], bad[2]); err != ErrNoTile {
			t.Errorf("tile %v: %v, want ErrNoTile", bad, err)
		}
	}
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestCheckURL(t *testing.T) {
	if err := CheckURL(DefaultURL); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "tile.example.org/{z}/{x}/{y}.png", "https://tile.example.org/{z}/{x}.png", "file:///{z}/{x}/{y}"} {
		if CheckURL(bad) == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestTilesCannotBeUsedToHammerTheMapServiceOrFillTheDisk(t *testing.T) {
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		png.Encode(w, image.NewGray(image.Rect(0, 0, 2, 2)))
	}))
	defer srv.Close()
	dir := t.TempDir()
	tiles := New(srv.URL+"/{z}/{x}/{y}.png", dir, "PiDash/test")
	tiles.Most, tiles.Keeps = 30, 20
	ctx := context.Background()

	var refused int
	for x := 0; x < 50; x++ {
		if _, err := tiles.Tile(ctx, 12, x, 7); err == ErrBusy {
			refused++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if asked.Load() != 30 || refused != 20 {
		t.Errorf("%d tiles fetched and %d refused, want 30 and 20", asked.Load(), refused)
	}
	// What is kept already is still handed out.
	if _, err := tiles.Tile(ctx, 12, 29, 7); err != nil {
		t.Errorf("a kept tile while over the limit: %v", err)
	}
	// And no more is kept than was said, give or take what one tidying leaves.
	var kept int
	filepath.WalkDir(dir, func(_ string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			kept++
		}
		return nil
	})
	if kept > 21 {
		t.Errorf("%d tiles kept, want about 20", kept)
	}
}
