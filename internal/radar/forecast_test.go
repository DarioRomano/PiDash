package radar

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A pretend RainViewer whose pictures show a patch of rain that moves: so
// far east and south with every second, in parts of the width of the world.
// still makes it stand where it is; dry leaves the pictures empty.
type weather struct {
	vx, vy float64
	dry    bool
}

func (w weather) service(t *testing.T) (*Radar, *int) {
	t.Helper()
	// The patch is near Zurich at the time of the newest picture: in the
	// middle of the piece Zurich is in, so that it stays within it for a while.
	const newest = 1700002800
	zx, zy := worldPoint(47.37, 8.54)
	zx, zy = (math.Floor(zx*128)+0.5)/128, (math.Floor(zy*128)+0.5)/128
	fetched := 0
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/public/weather-maps.json" {
			past := []string{}
			for at := newest - 3600; at <= newest; at += 600 {
				past = append(past, fmt.Sprintf(`{"time":%d,"path":"/v2/radar/%d"}`, at, at))
			}
			fmt.Fprintf(rw, `{"host":%q,"radar":{"past":[%s]}}`, srv.URL, strings.Join(past, ","))
			return
		}
		var at int64
		var z, x, y int
		if _, err := fmt.Sscanf(r.URL.Path, "/v2/radar/%d/256/%d/%d/%d/2/1_1.png", &at, &z, &x, &y); err != nil {
			t.Errorf("asked for %s", r.URL.Path)
			http.NotFound(rw, r)
			return
		}
		fetched++
		img := image.NewNRGBA(image.Rect(0, 0, 256, 256))
		if !w.dry {
			// Where the patch is at that moment, and how wide: 60 km or so.
			cx, cy := zx+w.vx*float64(at-newest), zy+w.vy*float64(at-newest)
			world := float64(int(256) << z)
			for py := 0; py < 256; py++ {
				for px := 0; px < 256; px++ {
					dx, dy := (float64(x*256+px)+0.5)/world-cx, (float64(y*256+py)+0.5)/world-cy
					// Longer one way than the other, so that it is not the same from every side.
					if d := math.Hypot(dx/1.6, dy) * 40075; d < 30 {
						img.SetNRGBA(px, py, color.NRGBA{40, 110, 230, uint8(255 - d*5)})
					}
				}
			}
		}
		_ = png.Encode(rw, img)
	}))
	t.Cleanup(srv.Close)
	r := New("PiDash/test")
	r.URL = srv.URL + "/public/weather-maps.json"
	r.HTTP = srv.Client()
	r.Now = func() time.Time { return time.Unix(newest+120, 0) }
	return r, &fetched
}

// Where the rain is in a piece: the middle of it, and how much there is.
func rainIn(t *testing.T, data []byte) (x, y, amount float64) {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for py := 0; py < 256; py++ {
		for px := 0; px < 256; px++ {
			_, _, _, a := img.At(px, py).RGBA()
			x, y, amount = x+float64(px)*float64(a), y+float64(py)*float64(a), amount+float64(a)
		}
	}
	if amount > 0 {
		x, y = x/amount, y/amount
	}
	return x, y, amount
}

func TestWhereTheRainGoesNext(t *testing.T) {
	// 60 km/h to the east and 20 to the south, at Zurich's latitude, where
	// the width of the world on the map is some 27,000 km.
	const kmWorld = 40075 * 0.677
	w := weather{vx: 60.0 / 3600 / kmWorld, vy: 20.0 / 3600 / kmWorld}
	r, fetched := w.service(t)
	ctx := context.Background()

	ahead, ok := r.Ahead(ctx, 47.37, 8.54)
	if !ok {
		t.Fatal("no forecast though the rain moved")
	}
	if ahead.From != 1700002800 || len(ahead.Times) != 6 || ahead.Times[0] != 1700003400 || ahead.Times[5] != 1700006400 {
		t.Errorf("pictures to come = %+v", ahead)
	}
	if math.Abs(ahead.Motion.X-w.vx) > 0.12*w.vx || math.Abs(ahead.Motion.Y-w.vy) > 0.25*w.vy {
		t.Errorf("the rain moves (%.3g, %.3g), measured (%.3g, %.3g)", w.vx, w.vy, ahead.Motion.X, ahead.Motion.Y)
	}
	// Measuring took the pieces around the place, of two pictures; asked
	// again, nothing is fetched or measured anew.
	if *fetched != 18 {
		t.Errorf("%d pieces fetched for measuring, want 18", *fetched)
	}
	if again, ok := r.Ahead(ctx, 47.37, 8.54); !ok || again.Motion != ahead.Motion || *fetched != 18 {
		t.Errorf("asked again: %+v, %d pieces", again, *fetched)
	}

	// The piece Zurich is in, now and in half an hour: the rain has moved on
	// by what it moves in half an hour, and there is as much of it.
	zx, zy := worldPoint(47.37, 8.54)
	x, y := int(zx*128), int(zy*128)
	now, err := r.Tile(ctx, ahead.From, 7, x, y)
	if err != nil {
		t.Fatal(err)
	}
	later, err := r.TileAhead(ctx, ahead.From, ahead.Times[2], ahead.Motion, 7, x, y)
	if err != nil {
		t.Fatal(err)
	}
	nx, ny, _ := rainIn(t, now)
	lx, ly, _ := rainIn(t, later)
	wantX, wantY := w.vx*1800*256*128, w.vy*1800*256*128
	if math.Abs(lx-nx-wantX) > 6 || math.Abs(ly-ny-wantY) > 6 {
		t.Errorf("in half an hour the rain moved (%.1f, %.1f) points, want about (%.1f, %.1f)", lx-nx, ly-ny, wantX, wantY)
	}
	// Made once: the same piece again is not made again.
	if same, err := r.TileAhead(ctx, ahead.From, ahead.Times[2], ahead.Motion, 7, x, y); err != nil || !bytes.Equal(same, later) {
		t.Errorf("the piece changed when asked for again: %v", err)
	}

	// What is not a forecast of this radar is refused: made from an older
	// picture, further ahead than an hour, the past, or faster than weather.
	for name, c := range map[string]struct {
		from, at int64
		m        Motion
	}{
		"an older picture": {ahead.From - 600, ahead.From + 600, ahead.Motion},
		"two hours ahead":  {ahead.From, ahead.From + 7200, ahead.Motion},
		"the past":         {ahead.From, ahead.From - 600, ahead.Motion},
		"a storm of 1000":  {ahead.From, ahead.Times[0], Motion{X: 1e-5}},
	} {
		if _, err := r.TileAhead(ctx, c.from, c.at, c.m, 7, x, y); err != ErrNoTile {
			t.Errorf("%s: %v, want ErrNoTile", name, err)
		}
	}
}

func TestRainThatStandsStillAndNoRainAtAll(t *testing.T) {
	ctx := context.Background()
	still, _ := weather{}.service(t)
	ahead, ok := still.Ahead(ctx, 47.37, 8.54)
	if !ok || math.Hypot(ahead.Motion.X, ahead.Motion.Y) > 1e-9 {
		t.Errorf("rain that stands still was sent off: %+v %v", ahead, ok)
	}
	// Nothing to go by: the pictures to come are the newest one, unmoved.
	dry, _ := weather{dry: true, vx: 1e-7}.service(t)
	ahead, ok = dry.Ahead(ctx, 47.37, 8.54)
	if !ok || ahead.Motion != (Motion{}) {
		t.Errorf("without rain: %+v %v", ahead, ok)
	}
	data, err := dry.TileAhead(ctx, ahead.From, ahead.Times[0], ahead.Motion, 6, 33, 22)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, amount := rainIn(t, data); amount != 0 {
		t.Errorf("rain out of nothing: %v", amount)
	}
}

func TestFindingTheMoveBetweenTwoPictures(t *testing.T) {
	const side = 96
	patch := func(cx, cy float64) []float32 {
		out := make([]float32, side*side)
		for y := 0; y < side; y++ {
			for x := 0; x < side; x++ {
				if d := math.Hypot((float64(x)-cx)/1.5, float64(y)-cy); d < 12 {
					out[y*side+x] = float32(1 - d/12)
				}
			}
		}
		return out
	}
	for _, c := range []struct{ dx, dy float64 }{{0, 0}, {5, 0}, {-7, 3}, {2.5, -4.5}, {12, 9}} {
		dx, dy := shiftBetween(patch(40, 48), patch(40+c.dx, 48+c.dy), side, 20)
		if math.Abs(dx-c.dx) > 0.4 || math.Abs(dy-c.dy) > 0.4 {
			t.Errorf("moved by (%v, %v), found (%.2f, %.2f)", c.dx, c.dy, dx, dy)
		}
	}
}
