package radar

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A pretend RainViewer: a list of three pictures, and pieces that say which they are.
func service(t *testing.T) (*Radar, *[]string, *bool) {
	t.Helper()
	var asked []string
	down := false
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if down {
			http.Error(w, "away", http.StatusBadGateway)
			return
		}
		if r.URL.Path == "/public/weather-maps.json" {
			if r.Header.Get("User-Agent") != "PiDash/test" {
				t.Errorf("asked as %q", r.Header.Get("User-Agent"))
			}
			fmt.Fprintf(w, `{"version":"2.0","generated":1700003000,"host":%q,"radar":{"past":[{"time":1700001000,"path":"/v2/radar/1700001000"},{"time":1700001600,"path":"/v2/radar/1700001600"},{"time":1700002200,"path":"/v2/radar/../../etc"},{"time":1700002800,"path":"/v2/radar/1700002800"}],"nowcast":[]}}`, srv.URL)
			return
		}
		fmt.Fprint(w, "png of "+r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	r := New("PiDash/test")
	r.URL = srv.URL + "/public/weather-maps.json"
	r.HTTP = srv.Client()
	return r, &asked, &down
}

func TestRadarPicturesAndTheirPieces(t *testing.T) {
	r, asked, down := service(t)
	now := time.Unix(1700003000, 0)
	r.Now = func() time.Time { return now }
	ctx := context.Background()

	frames, err := r.Frames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The picture whose address climbs out of its folder is left out.
	if len(frames) != 3 || frames[0].Time != 1700001000 || frames[2].Time != 1700002800 {
		t.Fatalf("frames = %+v", frames)
	}
	// Asked again at once, the list is not fetched again.
	if _, err := r.Frames(ctx); err != nil || len(*asked) != 1 {
		t.Fatalf("the list was fetched %d times, %v", len(*asked), err)
	}

	data, err := r.Tile(ctx, 1700002800, 6, 33, 22)
	if err != nil || string(data) != "png of /v2/radar/1700002800/256/6/33/22/2/1_1.png" {
		t.Fatalf("a piece: %q %v", data, err)
	}
	// The same piece again comes from memory.
	before := len(*asked)
	if _, err := r.Tile(ctx, 1700002800, 6, 33, 22); err != nil || len(*asked) != before {
		t.Fatalf("the same piece was fetched again: %v", err)
	}
	for name, bad := range map[string][4]int64{
		"a moment that is not offered": {1700000000, 6, 33, 22},
		"closer than is offered":       {1700002800, 8, 33, 22},
		"beyond the edge of the world": {1700002800, 6, 64, 22},
		"a negative place":             {1700002800, 6, -1, 22},
	} {
		if _, err := r.Tile(ctx, bad[0], int(bad[1]), int(bad[2]), int(bad[3])); err != ErrNoTile {
			t.Errorf("%s: %v", name, err)
		}
	}

	// The service away: the list in hand serves while it is recent, then no more.
	*down = true
	now = now.Add(6 * time.Minute)
	if frames, err := r.Frames(ctx); err != nil || len(frames) != 3 {
		t.Fatalf("with the service away for a moment: %+v %v", frames, err)
	}
	if _, err := r.Tile(ctx, 1700002800, 6, 33, 22); err != nil {
		t.Fatalf("a piece in memory while the service is away: %v", err)
	}
	if _, err := r.Tile(ctx, 1700002800, 6, 34, 22); err == nil || !strings.Contains(err.Error(), "rain radar") {
		t.Fatalf("a new piece while the service is away: %v", err)
	}
	now = now.Add(40 * time.Minute)
	if _, err := r.Frames(ctx); err == nil {
		t.Fatal("an hour-old list is still handed out")
	}
}

func TestRadarKeepsToItsLimits(t *testing.T) {
	r, _, _ := service(t)
	now := time.Unix(1700003000, 0)
	r.Now = func() time.Time { return now }
	r.Most, r.Keeps = 5, 3
	ctx := context.Background()
	for x := 0; x < 5; x++ {
		if _, err := r.Tile(ctx, 1700002800, 5, x, 3); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Tile(ctx, 1700002800, 5, 9, 3); err != ErrBusy {
		t.Fatalf("a sixth piece within ten minutes: %v", err)
	}
	r.mu.Lock()
	kept := len(r.tiles)
	r.mu.Unlock()
	if kept != 3 {
		t.Fatalf("%d pieces kept, want 3", kept)
	}
	// Ten minutes on there is room again.
	now = now.Add(11 * time.Minute)
	if _, err := r.Tile(ctx, 1700002800, 5, 9, 3); err != nil {
		t.Fatalf("after ten minutes: %v", err)
	}
}
