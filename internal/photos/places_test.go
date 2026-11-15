package photos

import (
	"context"
	"image"
	"image/color"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlacesAndThePhotosTakenThere(t *testing.T) {
	s := New(Options{Source: Mock{}, StatePath: filepath.Join(t.TempDir(), "photos.json"), Log: quietLog})
	if err := s.SetSelection(Selection{Recursive: true, Folders: []Folder{{ID: "Holidays", Name: "Holidays", Path: "Holidays"}}}); err != nil {
		t.Fatal(err)
	}
	s.index(context.Background())
	places := s.Places()
	var names []string
	for _, p := range places {
		names = append(names, p.Name)
	}
	if got := strings.Join(names, " | "); got != "Cascais, Portugal | Zermatt, Switzerland" {
		t.Fatalf("places = %q", got)
	}
	p := places[0]
	if p.Count != 6 || !p.Named || p.First != 2024 || p.Last != 2024 || p.Latitude < 38.69 || p.Latitude > 38.72 || p.Longitude > -9.38 || p.Key == "" {
		t.Errorf("Cascais = %+v", p)
	}
	if st := s.Status(); st.Located != 12 {
		t.Errorf("located = %d, want 12", st.Located)
	}

	// Its photos, the newest first, and nothing from anywhere else.
	_, slides := s.SlidesAt(p.ID, 0)
	if len(slides) != 6 {
		t.Fatalf("%d slides from Cascais, want 6", len(slides))
	}
	for i, sl := range slides {
		if sl.Items[0].Place != "Cascais, Portugal" {
			t.Errorf("slide %d is from %q", i, sl.Items[0].Place)
		}
		if i > 0 && sl.Items[0].Taken.After(slides[i-1].Items[0].Taken) {
			t.Errorf("slide %d is newer than the one before it", i)
		}
	}
	if _, three := s.SlidesAt(p.ID, 3); len(three) != 3 {
		t.Errorf("limit 3 gave %d", len(three))
	}
	for _, id := range []string{"", "n:Nowhere", "c:0.0,0.0"} {
		if _, none := s.SlidesAt(id, 0); len(none) != 0 {
			t.Errorf("SlidesAt(%q) gave %d slides", id, len(none))
		}
	}
}

func TestPlacesWithoutANameAreAPatchOfTheMap(t *testing.T) {
	at := func(lat, lon float64) Item { return Item{Located: true, Latitude: lat, Longitude: lon} }
	if a, b := placeID(at(47.3712, 8.5391)), placeID(at(47.4188, 8.5421)); a != b || a != "c:47.4,8.5" {
		t.Errorf("two spots in the same town: %q and %q", a, b)
	}
	if a, b := placeID(at(47.37, 8.54)), placeID(at(46.95, 7.45)); a == b {
		t.Errorf("Zurich and Bern are one place: %q", a)
	}
	if id := placeID(Item{Place: "Somewhere"}); id != "" {
		t.Errorf("a photo without a position has a place: %q", id)
	}
	if got := coordinates(-33.86, 151.21); got != "33.9° S, 151.2° E" {
		t.Errorf("coordinates = %q", got)
	}
}

// A folder on disk says nothing about places: they are read from the files,
// once, and remembered.
func TestPlacesOfAFolderAreReadFromTheFiles(t *testing.T) {
	dir, cache := t.TempDir(), t.TempDir()
	data, _ := os.ReadFile("testdata/located.jpg")
	if err := os.WriteFile(filepath.Join(dir, "harbour.jpg"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.jpg"), jpegBytes(uniform(400, 300, color.RGBA{1, 2, 3, 255})), 0o644); err != nil {
		t.Fatal(err)
	}
	src := NewLocalDir(dir)
	open := func(places bool) *Service {
		s := New(Options{Source: src, StatePath: filepath.Join(cache, "photos.json"), CacheDir: cache, Places: places, Log: quietLog})
		if err := s.SetSelection(Selection{Folders: []Folder{{ID: ".", Name: "x", Path: "x"}}}); err != nil {
			t.Fatal(err)
		}
		s.index(context.Background())
		return s
	}
	ctx := context.Background()

	// Without the map nothing is looked into.
	off := open(false)
	off.scanSpots(ctx)
	if len(off.Places()) != 0 {
		t.Fatal("places were read although the map is switched off")
	}

	s := open(true)
	if len(s.Places()) != 0 {
		t.Fatal("places are known before the files were looked into")
	}
	s.scanSpots(ctx)
	places := s.Places()
	if len(places) != 1 || places[0].Named || places[0].Name != "33.9° S, 151.2° E" || places[0].Count != 1 || places[0].First != 0 && places[0].First < 1990 {
		t.Fatalf("places = %+v", places)
	}
	if _, slides := s.SlidesAt(places[0].ID, 0); len(slides) != 1 || slides[0].Items[0].Name != "harbour.jpg" {
		t.Fatalf("slides = %+v", slides)
	}
	if st := s.Status(); st.Located != 1 {
		t.Errorf("located = %d", st.Located)
	}

	// The next start knows without looking again, also for the file that said nothing.
	again := open(true)
	if len(again.Places()) != 1 {
		t.Fatal("the places were forgotten")
	}
	again.mu.Lock()
	known := len(again.spots)
	again.mu.Unlock()
	if known != 2 {
		t.Errorf("%d files remembered as looked at, want 2", known)
	}
}

func TestScreenshotsAndVerySmallPicturesAreLeftOut(t *testing.T) {
	for name, want := range map[string]bool{
		"IMG_2041.jpg": false, "Screenshot_2024-03-01.png": true, "Bildschirmfoto 2024.png": true, "screen shot 3.jpg": true,
		"holiday.jpg": false, "Capture d’écran 2024.png": true,
	} {
		if got := odd(Item{Name: name, Size: 3 << 20}); got != want {
			t.Errorf("odd(%q) = %v, want %v", name, got, want)
		}
	}
	if !odd(Item{Name: "icon.jpg", Size: 12 << 10}) {
		t.Error("a 12 KB picture counts as a photo")
	}
	if odd(Item{Name: "photo.jpg"}) {
		t.Error("a photo of unknown size is left out")
	}

	// With the setting on they are not in the slideshow; off, they are.
	dir := t.TempDir()
	big := jpegBytes(noisy(900, 600))
	for _, name := range []string{"IMG_1.jpg", "Screenshot_1.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), big, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "tiny.jpg"), jpegBytes(uniform(40, 30, color.RGBA{9, 9, 9, 255})), 0o644); err != nil {
		t.Fatal(err)
	}
	count := func(skip bool) int {
		s := New(Options{Source: NewLocalDir(dir), StatePath: filepath.Join(t.TempDir(), "p.json"), SkipOdd: skip, Log: quietLog})
		if err := s.SetSelection(Selection{Folders: []Folder{{ID: ".", Name: "x", Path: "x"}}}); err != nil {
			t.Fatal(err)
		}
		s.index(context.Background())
		return s.Status().Count
	}
	if got := count(false); got != 3 {
		t.Errorf("with everything shown: %d photos, want 3", got)
	}
	if got := count(true); got != 1 {
		t.Errorf("with the odd ones left out: %d photos, want 1", got)
	}
}

func TestPhotosOfADayAndOfAYear(t *testing.T) {
	s, _ := library(t, 6)
	_, all := s.Playlist(0)
	if len(all) == 0 {
		t.Fatal("no photos")
	}
	first := all[0].Taken
	_, day := s.SlidesTaken(first.Year(), first.Month(), first.Day(), 0)
	if len(day) == 0 {
		t.Fatal("no photos from the day one of them was taken")
	}
	for _, sl := range day {
		if y, m, d := sl.Items[0].Taken.Date(); y != first.Year() || m != first.Month() || d != first.Day() {
			t.Errorf("a photo from %v among those of %v", sl.Items[0].Taken, first)
		}
	}
	_, year := s.SlidesTaken(first.Year(), 0, 0, 0)
	if len(year) < len(day) {
		t.Errorf("the year has %d photos, its day %d", len(year), len(day))
	}
	for i := 1; i < len(year); i++ {
		if year[i].Items[0].Taken.Before(year[i-1].Items[0].Taken) {
			t.Error("the photos of a year are not in the order they were taken")
		}
	}
	if _, none := s.SlidesTaken(1999, 0, 0, 0); len(none) != 0 {
		t.Errorf("%d photos from 1999", len(none))
	}
	s.SetInterval(2 * time.Minute)
	if st := s.Status(); st.IntervalSeconds != 120 {
		t.Errorf("interval = %d", st.IntervalSeconds)
	}
}

// noisy is a picture that does not compress to nothing, as a photo does not.
func noisy(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewSource(1))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.Intn(256))
	}
	return img
}
