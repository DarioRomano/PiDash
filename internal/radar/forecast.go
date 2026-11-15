package radar

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"sync"
)

// Where the rain goes next.
//
// The radar service gives the pictures of the past only. The pictures to
// come are worked out here, the way a person reads a radar film: see how the
// rain moved between the last pictures, and move the newest picture on by
// as much. That is good for an hour or so, and better for a front than for
// summer showers, which come and go where they are. Nothing new is asked of
// anyone for it: the pieces are the ones that are fetched anyway.

// Motion is how the rain moves: in parts of the width of the whole map of
// the world, a second. East and south are positive.
type Motion struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Forecast says which pictures to come can be made, and from what.
type Forecast struct {
	// From is the time of the picture they are made from: the newest.
	From int64 `json:"from"`
	// Times are the moments of the pictures to come.
	Times  []int64 `json:"times"`
	Motion Motion  `json:"motion"`
}

const (
	// The pictures to come: one every ten minutes, for an hour.
	aheadStep  = 600
	aheadCount = 6
	// How the rain moved is measured this close in, on this many pieces
	// around the place, between pictures this many apart.
	motionZoom  = MaxZoom
	motionTiles = 3
	motionBack  = 2
	// For the comparison the pictures are made smaller by this, and moved
	// against one another by up to so many of the smaller picture's points.
	motionShrink = 4
	motionReach  = 20
	tileSize     = 256
)

// worldPoint is where a place lies on the map of the world, both from 0 to 1.
func worldPoint(lat, lon float64) (x, y float64) {
	lat = math.Max(-85, math.Min(85, lat))
	x = (lon + 180) / 360
	s := math.Sin(lat * math.Pi / 180)
	y = 0.5 - math.Log((1+s)/(1-s))/(4*math.Pi)
	return x - math.Floor(x), y
}

// Ahead works out the pictures to come around a place: how the rain moved
// over the last pictures, and for which moments it can be moved on. It
// reports false when there is too little to go by.
func (r *Radar) Ahead(ctx context.Context, lat, lon float64) (Forecast, bool) {
	frames, err := r.Frames(ctx)
	if err != nil || len(frames) <= motionBack {
		return Forecast{}, false
	}
	newest, before := frames[len(frames)-1], frames[len(frames)-1-motionBack]
	span := newest.Time - before.Time
	if span <= 0 || span > 3600 {
		return Forecast{}, false
	}
	wx, wy := worldPoint(lat, lon)
	n := 1 << motionZoom
	cx, cy := int(wx*float64(n)), int(wy*float64(n))

	// Measured once for a picture and a place; asked again, it is known.
	key := fmt.Sprintf("%d/%d/%d", newest.Time, cx, cy)
	r.mu.Lock()
	m, known := r.motions[key]
	r.mu.Unlock()
	if !known {
		now, errNow := r.mosaic(ctx, newest.Time, cx, cy)
		then, errThen := r.mosaic(ctx, before.Time, cx, cy)
		if errNow != nil || errThen != nil {
			return Forecast{}, false
		}
		dx, dy := shiftBetween(then, now, tileSize*motionTiles/motionShrink, motionReach)
		world := float64(tileSize * n)
		m = Motion{X: dx * motionShrink / world / float64(span), Y: dy * motionShrink / world / float64(span)}
		r.mu.Lock()
		if r.motions == nil || len(r.motions) > 50 {
			r.motions = map[string]Motion{}
		}
		r.motions[key] = m
		r.mu.Unlock()
	}
	f := Forecast{From: newest.Time, Motion: m}
	for i := 1; i <= aheadCount; i++ {
		f.Times = append(f.Times, newest.Time+int64(i*aheadStep))
	}
	return f, true
}

// mosaic is how much rain there is around a place at one moment: the pieces
// around the piece (cx, cy), side by side and made smaller, a number from 0
// (none) to 1 for every point.
func (r *Radar) mosaic(ctx context.Context, at int64, cx, cy int) ([]float32, error) {
	n := 1 << motionZoom
	side := tileSize * motionTiles / motionShrink
	out := make([]float32, side*side)
	type piece struct {
		i, j int
		img  *image.NRGBA
		err  error
	}
	var wg sync.WaitGroup
	pieces := make([]piece, 0, motionTiles*motionTiles)
	for j := 0; j < motionTiles; j++ {
		for i := 0; i < motionTiles; i++ {
			pieces = append(pieces, piece{i: i, j: j})
		}
	}
	for k := range pieces {
		p := &pieces[k]
		x, y := ((cx+p.i-motionTiles/2)%n+n)%n, cy+p.j-motionTiles/2
		if y < 0 || y >= n {
			continue // beyond the top or bottom of the map: no rain there
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.img, p.err = r.picture(ctx, at, motionZoom, x, y)
		}()
	}
	wg.Wait()
	small := tileSize / motionShrink
	for _, p := range pieces {
		if p.err != nil {
			return nil, p.err
		}
		if p.img == nil {
			continue
		}
		for y := 0; y < small; y++ {
			for x := 0; x < small; x++ {
				sum := 0
				for v := 0; v < motionShrink; v++ {
					row := p.img.PixOffset(x*motionShrink, y*motionShrink+v)
					for u := 0; u < motionShrink; u++ {
						sum += int(p.img.Pix[row+u*4+3]) // how solid the colour is: how much rain
					}
				}
				out[(p.j*small+y)*side+p.i*small+x] = float32(sum) / float32(255*motionShrink*motionShrink)
			}
		}
	}
	return out, nil
}

// picture is one piece of the radar, read as a picture.
func (r *Radar) picture(ctx context.Context, at int64, z, x, y int) (*image.NRGBA, error) {
	data, err := r.Tile(ctx, at, z, x, y)
	if err != nil {
		return nil, err
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("a piece of the rain radar could not be read: %w", err)
	}
	if b := src.Bounds(); b.Dx() != tileSize || b.Dy() != tileSize {
		return nil, fmt.Errorf("a piece of the rain radar has the wrong size: %d x %d", b.Dx(), b.Dy())
	}
	img := image.NewNRGBA(image.Rect(0, 0, tileSize, tileSize))
	draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
	return img, nil
}

// shiftBetween finds by how many points the rain moved from the picture
// then to the picture now: the move under which the two fit best. Both are
// square, side points wide, and moves of up to reach points each way are
// tried. Without rain to go by, the answer is "not at all".
func shiftBetween(then, now []float32, side, reach int) (dx, dy float64) {
	var rain float64
	for i := range then {
		rain += float64(then[i]) + float64(now[i])
	}
	if rain < float64(side*side)*0.004 {
		return 0, 0
	}
	// How badly the pictures fit under a move: the difference over the part
	// where they overlap, so that no move is favoured for covering less.
	misfit := func(mx, my int) float64 {
		var sum float64
		x0, x1 := max(0, -mx), min(side, side-mx)
		y0, y1 := max(0, -my), min(side, side-my)
		for y := y0; y < y1; y++ {
			a, b := then[y*side:], now[(y+my)*side:]
			for x := x0; x < x1; x++ {
				d := a[x] - b[x+mx]
				if d < 0 {
					d = -d
				}
				sum += float64(d)
			}
		}
		return sum / float64((x1-x0)*(y1-y0))
	}
	scores := make([]float64, (2*reach+1)*(2*reach+1))
	at := func(mx, my int) *float64 { return &scores[(my+reach)*(2*reach+1)+mx+reach] }
	bx, by, best := 0, 0, math.Inf(1)
	for my := -reach; my <= reach; my++ {
		for mx := -reach; mx <= reach; mx++ {
			s := misfit(mx, my)
			// Of two moves that fit alike, the smaller: rain that stands
			// still must not be sent off for want of a difference.
			s += 1e-7 * float64(mx*mx+my*my)
			*at(mx, my) = s
			if s < best {
				bx, by, best = mx, my, s
			}
		}
	}
	// Between the points: where the fit would be best if it could be tried.
	finer := func(lo, mid, hi float64) float64 {
		if d := lo - 2*mid + hi; d > 0 {
			return 0.5 * (lo - hi) / d
		}
		return 0
	}
	dx, dy = float64(bx), float64(by)
	if bx > -reach && bx < reach {
		dx += finer(*at(bx-1, by), best, *at(bx+1, by))
	}
	if by > -reach && by < reach {
		dy += finer(*at(bx, by-1), best, *at(bx, by+1))
	}
	return dx, dy
}

// TileAhead returns one piece of a picture to come: the newest picture,
// moved on by as much as the rain moves until then. from must be the newest
// picture's time, so that an old forecast is not mistaken for a new one.
func (r *Radar) TileAhead(ctx context.Context, from, at int64, m Motion, z, x, y int) ([]byte, error) {
	if z < 0 || z > MaxZoom || x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		return nil, ErrNoTile
	}
	frames, err := r.Frames(ctx)
	if err != nil {
		return nil, err
	}
	lead := at - from
	if len(frames) == 0 || frames[len(frames)-1].Time != from || lead <= 0 || lead > aheadStep*aheadCount {
		return nil, ErrNoTile
	}
	// Faster than any weather: not a forecast anyone was given by Ahead.
	if speed := math.Hypot(m.X, m.Y); math.IsNaN(speed) || speed > 3e-6 {
		return nil, ErrNoTile
	}
	n := 1 << z
	world := float64(tileSize * n)
	sx, sy := int(math.Round(m.X*float64(lead)*world)), int(math.Round(m.Y*float64(lead)*world))
	key := fmt.Sprintf("%d>%d/%d/%d/%d/%d/%d", from, at, z, x, y, sx, sy)
	r.mu.Lock()
	if r.madeFrom != from {
		r.made, r.madeFrom = map[string][]byte{}, from
	}
	data, ok := r.made[key]
	r.mu.Unlock()
	if ok {
		return data, nil
	}

	// What is at a point then is what is now where it comes from.
	left, top := x*tileSize-sx, y*tileSize-sy
	out := image.NewNRGBA(image.Rect(0, 0, tileSize, tileSize))
	floor := func(a int) int { return int(math.Floor(float64(a) / tileSize)) }
	for ty := floor(top); ty <= floor(top+tileSize-1); ty++ {
		if ty < 0 || ty >= n {
			continue
		}
		for tx := floor(left); tx <= floor(left+tileSize-1); tx++ {
			src, err := r.picture(ctx, from, z, ((tx%n)+n)%n, ty)
			if err != nil {
				return nil, err
			}
			draw.Draw(out, image.Rect(tx*tileSize-left, ty*tileSize-top, (tx+1)*tileSize-left, (ty+1)*tileSize-top), src, image.Point{}, draw.Src)
		}
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, out); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.madeFrom == from {
		if len(r.made) >= 600 {
			r.made = map[string][]byte{} // more than a screen's worth of an hour: start over
		}
		r.made[key] = buf.Bytes()
	}
	return buf.Bytes(), nil
}
