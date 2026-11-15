// Package radar fetches pictures of where it rains: the rain radar of the
// last hours, as pieces that lie over a map. They come from RainViewer,
// which gathers the weather services' radars; the Pi fetches them, so the
// dashboard's pages need to reach nothing but the Pi.
package radar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultURL is where RainViewer says which pictures there are.
const DefaultURL = "https://api.rainviewer.com/public/weather-maps.json"

// MaxZoom is the closest look the service gives without payment.
const MaxZoom = 7

// Frame is the radar at one moment.
type Frame struct {
	// Time is when it was measured, in seconds since 1970.
	Time int64 `json:"time"`
	path string
}

// Radar fetches frames and their pieces, and keeps them for a while.
type Radar struct {
	// URL is the list of pictures; Agent names this program to the service.
	URL   string
	Agent string
	HTTP  *http.Client
	// Most is how many pieces are fetched in any ten minutes, and Keeps how
	// many are kept in memory.
	Most  int
	Keeps int
	Now   func() time.Time

	mu       sync.Mutex
	host     string
	frames   []Frame
	listedAt time.Time
	tiles    map[string][]byte
	order    []string // the keys of tiles, oldest first
	fetched  []time.Time
}

// New returns a radar that asks RainViewer.
func New(agent string) *Radar {
	return &Radar{URL: DefaultURL, Agent: agent, HTTP: &http.Client{Timeout: 15 * time.Second}, Most: 1500, Keeps: 900}
}

var (
	// ErrNoTile is returned for a piece that does not exist, or of a frame
	// that is no longer offered.
	ErrNoTile = errors.New("there is no such piece of the rain radar")
	// ErrBusy is returned when too many pieces were asked for in a short time.
	ErrBusy = errors.New("too many pieces of the rain radar were asked for; try again in a few minutes")
)

func (r *Radar) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Radar) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	if r.Agent != "" {
		req.Header.Set("User-Agent", r.Agent)
	}
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return nil, errors.New("the rain radar could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the rain radar answered HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// Frames returns the radar pictures of the last two hours, oldest first. The
// list is asked for at most every five minutes; when the service is away,
// the last list is used for as long as its pictures are recent.
func (r *Radar) Frames(ctx context.Context) ([]Frame, error) {
	r.mu.Lock()
	fresh := r.now().Sub(r.listedAt) < 5*time.Minute
	frames := append([]Frame{}, r.frames...)
	r.mu.Unlock()
	if fresh {
		return frames, nil
	}
	data, err := r.get(ctx, r.URL, 1<<20)
	var list struct {
		Host  string `json:"host"`
		Radar struct {
			Past []struct {
				Time int64  `json:"time"`
				Path string `json:"path"`
			} `json:"past"`
		} `json:"radar"`
	}
	if err == nil {
		if json.Unmarshal(data, &list) != nil || len(list.Radar.Past) == 0 {
			err = errors.New("the rain radar gave an answer that could not be read")
		}
	}
	if err == nil {
		// The pictures are fetched from where the list says, over an
		// encrypted connection unless the list itself came without one.
		plain := strings.HasPrefix(r.URL, "http://")
		if u, perr := url.Parse(list.Host); perr != nil || u.Host == "" || (u.Scheme != "https" && !(plain && u.Scheme == "http")) {
			err = errors.New("the rain radar named no place to fetch its pictures from")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		// What is in hand is good while its newest picture is under half an hour old.
		if n := len(r.frames); n > 0 && r.now().Sub(time.Unix(r.frames[n-1].Time, 0)) < 30*time.Minute {
			return append([]Frame{}, r.frames...), nil
		}
		return nil, err
	}
	r.host = strings.TrimRight(list.Host, "/")
	r.frames = r.frames[:0]
	for _, f := range list.Radar.Past {
		// A path is part of an address: nothing but what such a path is made of.
		if f.Time > 0 && strings.HasPrefix(f.Path, "/") && !strings.ContainsAny(f.Path, "?#\\ \t\r\n") && !strings.Contains(f.Path, "..") {
			r.frames = append(r.frames, Frame{Time: f.Time, path: f.Path})
		}
	}
	r.listedAt = r.now()
	// Pieces of frames that are no longer offered are of no use.
	current := map[int64]bool{}
	for _, f := range r.frames {
		current[f.Time] = true
	}
	kept := r.order[:0]
	for _, k := range r.order {
		var t int64
		_, _ = fmt.Sscanf(k, "%d/", &t)
		if current[t] {
			kept = append(kept, k)
		} else {
			delete(r.tiles, k)
		}
	}
	r.order = kept
	return append([]Frame{}, r.frames...), nil
}

// Tile returns one piece of the radar picture of a moment, as a PNG.
func (r *Radar) Tile(ctx context.Context, at int64, z, x, y int) ([]byte, error) {
	if z < 0 || z > MaxZoom || x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		return nil, ErrNoTile
	}
	frames, err := r.Frames(ctx)
	if err != nil {
		return nil, err
	}
	path := ""
	for _, f := range frames {
		if f.Time == at {
			path = f.path
		}
	}
	if path == "" {
		return nil, ErrNoTile
	}
	key := fmt.Sprintf("%d/%d/%d/%d", at, z, x, y)
	r.mu.Lock()
	if data, ok := r.tiles[key]; ok {
		r.mu.Unlock()
		return data, nil
	}
	// Not more than so many in ten minutes, whoever asks.
	now := r.now()
	recent := r.fetched[:0]
	for _, t := range r.fetched {
		if now.Sub(t) < 10*time.Minute {
			recent = append(recent, t)
		}
	}
	r.fetched = recent
	if r.Most > 0 && len(r.fetched) >= r.Most {
		r.mu.Unlock()
		return nil, ErrBusy
	}
	r.fetched = append(r.fetched, now)
	host := r.host
	r.mu.Unlock()

	// 256 pixels a piece; colours "2" (blue for rain); smoothed, with snow shown apart.
	data, err := r.get(ctx, fmt.Sprintf("%s%s/256/%d/%d/%d/2/1_1.png", host, path, z, x, y), 2<<20)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tiles == nil {
		r.tiles = map[string][]byte{}
	}
	if _, ok := r.tiles[key]; !ok {
		r.tiles[key] = data
		r.order = append(r.order, key)
		for r.Keeps > 0 && len(r.order) > r.Keeps {
			delete(r.tiles, r.order[0])
			r.order = r.order[1:]
		}
	}
	return data, nil
}
