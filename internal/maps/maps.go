// Package maps fetches the map pictures shown beside a photo that knows where
// it was taken, and keeps them, so that the map service is asked for each
// piece of the world once and not again for a long time.
package maps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultURL is OpenStreetMap's own tile server. {z}, {x} and {y} stand for
// the zoom level and the position of a tile.
const DefaultURL = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"

// MaxZoom is the closest look that is asked for.
const MaxZoom = 17

// ErrNoTile is returned for a tile that does not exist.
var ErrNoTile = errors.New("no such map tile")

// Tiles is a store of map tiles in a directory.
type Tiles struct {
	// URL is where a tile is fetched from, with {z}, {x} and {y} in it.
	URL string
	// Dir is where tiles are kept.
	Dir string
	// Agent names this program to the map service, as its rules ask.
	Agent string
	// Keep is how long a tile is used before it is fetched again (default: 30 days).
	Keep time.Duration
	HTTP *http.Client

	// Most is how many tiles are fetched in any ten minutes (default 400),
	// and Keeps how many are kept on disk (default 4000). Anyone on the home
	// network can ask for a tile; these keep that from being used to hammer
	// the map service in the Pi's name or to fill its disk.
	Most  int
	Keeps int

	// slots keeps to the two downloads at a time the map service allows.
	slots chan struct{}

	mu      sync.Mutex
	fetched []time.Time // when the last tiles were fetched
	written int         // tiles written since the store was last tidied
}

// ErrBusy is returned when too many tiles were asked for in a short time.
var ErrBusy = errors.New("too many pieces of map were asked for; try again in a few minutes")

// New returns a store. An empty address means OpenStreetMap.
func New(rawURL, dir, agent string) *Tiles {
	if strings.TrimSpace(rawURL) == "" {
		rawURL = DefaultURL
	}
	return &Tiles{
		URL: rawURL, Dir: dir, Agent: agent, Keep: 30 * 24 * time.Hour, Most: 400, Keeps: 4000,
		HTTP:  &http.Client{Timeout: 15 * time.Second},
		slots: make(chan struct{}, 2),
	}
}

// CheckURL reports what is wrong with a tile address.
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("must be a web address like %s", DefaultURL)
	}
	for _, part := range []string{"{z}", "{x}", "{y}"} {
		if !strings.Contains(raw, part) {
			return fmt.Errorf("must contain %s, as in %s", part, DefaultURL)
		}
	}
	return nil
}

// Tile returns the file holding one tile, fetching it when it is not there
// or has grown old. An old tile is better than none: it is used when the map
// service cannot be reached.
func (t *Tiles) Tile(ctx context.Context, z, x, y int) (string, error) {
	if z < 0 || z > MaxZoom || x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		return "", ErrNoTile
	}
	path := filepath.Join(t.Dir, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
	fi, statErr := os.Stat(path)
	if statErr == nil && time.Since(fi.ModTime()) < t.Keep {
		return path, nil
	}
	if err := t.fetch(ctx, z, x, y, path); err != nil {
		if statErr == nil {
			return path, nil
		}
		return "", err
	}
	return path, nil
}

func (t *Tiles) fetch(ctx context.Context, z, x, y int, path string) error {
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	// Someone else may have fetched it while this request waited its turn.
	if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < t.Keep {
		return nil
	}
	if !t.mayFetch(time.Now()) {
		return ErrBusy
	}
	address := strings.NewReplacer("{z}", strconv.Itoa(z), "{x}", strconv.Itoa(x), "{y}", strconv.Itoa(y)).Replace(t.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", t.Agent)
	resp, err := t.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("cannot reach the map service: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the map service answered HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if kind := http.DetectContentType(data); !strings.HasPrefix(kind, "image/") {
		return fmt.Errorf("the map service sent %s instead of a picture", kind)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tile-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	t.tidy()
	return nil
}

// mayFetch counts a fetch against the limit and reports whether it may go ahead.
func (t *Tiles) mayFetch(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Most <= 0 {
		return true
	}
	recent := t.fetched[:0]
	for _, at := range t.fetched {
		if now.Sub(at) < 10*time.Minute {
			recent = append(recent, at)
		}
	}
	t.fetched = recent
	if len(t.fetched) >= t.Most {
		return false
	}
	t.fetched = append(t.fetched, now)
	return true
}

// tidy removes the tiles that were not used for the longest time once there
// are more than are kept. It looks only now and then: listing the store is
// work.
func (t *Tiles) tidy() {
	t.mu.Lock()
	t.written++
	due := t.Keeps > 0 && t.written >= max(1, t.Keeps/20)
	if due {
		t.written = 0
	}
	t.mu.Unlock()
	if !due {
		return
	}
	type tile struct {
		path string
		at   time.Time
	}
	var all []tile
	_ = filepath.WalkDir(t.Dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".png") {
			if fi, err := d.Info(); err == nil {
				all = append(all, tile{path, fi.ModTime()})
			}
		}
		return nil
	})
	if len(all) <= t.Keeps {
		return
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })
	for _, old := range all[:len(all)-t.Keeps] {
		_ = os.Remove(old.path)
	}
}
