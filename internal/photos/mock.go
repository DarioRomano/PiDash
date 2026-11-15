package photos

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"strings"
	"time"
)

// Mock is a source of generated pictures for development (`pidash -mock`):
// soft colour gradients in several shapes, so cropping and the portrait
// backdrop can be seen without a NAS.
type Mock struct{}

var mockTree = map[string][]string{
	"":         {"Holidays", "Family", "Journeys"},
	"Holidays": {"Holidays/Coast", "Holidays/Mountains"},
	"Family":   {"Family/Portraits"},
	// Not among the photos shown at first: a year at home with two trips in
	// it, for trying out what is worked out from places and dates.
	"Journeys": {"Journeys/At home", "Journeys/Iceland", "Journeys/Lisbon"},
}

// mockJourneys says where and when the photos of the "Journeys" folders were
// taken: the place, where it is, the first day, and on how many days.
var mockJourneys = map[string]struct {
	place    string
	lat, lon float64
	first    time.Time
	photos   int
	// every is the number of days from one photo to the next.
	every int
}{
	"Journeys/At home": {"Zurich, Switzerland", 47.3769, 8.5417, time.Date(2023, 1, 8, 15, 0, 0, 0, time.UTC), 12, 100},
	"Journeys/Iceland": {"Reykjavik, Iceland", 64.1466, -21.9426, time.Date(2023, 3, 10, 11, 0, 0, 0, time.UTC), 7, 1},
	"Journeys/Lisbon":  {"Lisbon, Portugal", 38.7223, -9.1393, time.Date(2023, 9, 2, 10, 0, 0, 0, time.UTC), 9, 1},
}

var mockShapes = [][2]int{{1600, 1200}, {1920, 1080}, {900, 1200}, {1500, 1000}, {2400, 800}, {1080, 1350}}

// Label implements Source.
func (Mock) Label() string { return "Sample photos" }

// HasPreviews implements Source.
func (Mock) HasPreviews() bool { return false }

// DefaultSelection implements Source.
func (Mock) DefaultSelection() Selection {
	return Selection{Recursive: true, Folders: []Folder{{ID: "Holidays", Name: "Holidays", Path: "Holidays"}}}
}

// Folders implements Source.
func (Mock) Folders(_ context.Context, parent string) ([]Folder, error) {
	var out []Folder
	for _, id := range mockTree[parent] {
		name := id[strings.LastIndex(id, "/")+1:]
		out = append(out, Folder{ID: id, Name: name, Path: strings.ReplaceAll(id, "/", " / ")})
	}
	return out, nil
}

// Items implements Source.
func (Mock) Items(_ context.Context, folder Folder) ([]Item, error) {
	var out []Item
	if j, ok := mockJourneys[folder.ID]; ok {
		for i := 0; i < j.photos; i++ {
			key := fmt.Sprintf("m-%s-%d", strings.ToLower(strings.NewReplacer("/", "-", " ", "-").Replace(folder.ID)), i)
			out = append(out, Item{
				Key: key, Name: fmt.Sprintf("sample-%d.jpg", i+1), Folder: folder.Path, Version: "1", ID: key, WallClock: true,
				// Two photos a day on a trip, so that it lasts half as many days as it has photos.
				Taken: j.first.AddDate(0, 0, i*j.every/max(1, 2/j.every)).Add(time.Duration(i) * time.Hour),
				Place: j.place, Located: true, Latitude: j.lat + float64(i)*0.002, Longitude: j.lon + float64(i)*0.003,
			})
		}
		return out, nil
	}
	// The samples were "taken" somewhere, except those of the family.
	where := map[string]struct {
		place    string
		lat, lon float64
	}{
		"Holidays/Coast":     {"Cascais, Portugal", 38.6968, -9.4215},
		"Holidays/Mountains": {"Zermatt, Switzerland", 46.0207, 7.7491},
	}[folder.ID]
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("m-%s-%d", strings.ToLower(strings.ReplaceAll(folder.ID, "/", "-")), i)
		out = append(out, Item{
			Key: key, Name: fmt.Sprintf("sample-%d.jpg", i+1), Folder: folder.Path,
			Taken: time.Date(2024, time.Month(1+i*2), 3+i, 12, 0, 0, 0, time.UTC), Version: "1", ID: key,
			Place: where.place, WallClock: true,
		})
		if where.place != "" {
			at := &out[len(out)-1]
			at.Located, at.Latitude, at.Longitude = true, where.lat+float64(i)*0.004, where.lon+float64(i)*0.006
		}
	}
	return out, nil
}

// Fetch implements Source.
func (Mock) Fetch(_ context.Context, item Item, _ bool) ([]byte, bool, error) {
	h := fnv.New32a()
	h.Write([]byte(item.Key))
	seed := h.Sum32()
	shape := mockShapes[int(seed)%len(mockShapes)]
	if strings.Contains(item.Key, "portraits") {
		shape = mockShapes[2]
	}
	w, hgt := shape[0], shape[1]
	hue := float64(seed%360) / 360
	img := image.NewRGBA(image.Rect(0, 0, w, hgt))
	cx, cy := float64(w)*(0.3+float64(seed>>8%40)/100), float64(hgt)*0.38
	for y := 0; y < hgt; y++ {
		for x := 0; x < w; x++ {
			fy := float64(y) / float64(hgt)
			d := math.Hypot(float64(x)-cx, float64(y)-cy) / float64(hgt)
			// Sky-to-ground gradient with a soft "sun".
			l := 0.62 - 0.38*fy + 0.35*math.Exp(-d*d*9)
			s := 0.55 + 0.25*fy
			img.SetRGBA(x, y, hsl(math.Mod(hue+0.12*fy, 1), s, math.Min(l, 0.96)))
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		return nil, false, err
	}
	return b.Bytes(), true, nil
}

func hsl(h, s, l float64) color.RGBA {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h*6, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch int(h * 6) {
	case 0:
		r, g, b = c, x, 0
	case 1:
		r, g, b = x, c, 0
	case 2:
		r, g, b = 0, c, x
	case 3:
		r, g, b = 0, x, c
	case 4:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	to := func(v float64) uint8 { return uint8(math.Max(0, math.Min(255, (v+m)*255))) }
	return color.RGBA{to(r), to(g), to(b), 255}
}
