package photos

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTimeAndPlaceFromTheFile(t *testing.T) {
	data, err := os.ReadFile("testdata/located.jpg")
	if err != nil {
		t.Fatal(err)
	}
	extra := readExtra(data)
	if want := time.Date(2019, 7, 14, 18, 42, 7, 0, time.UTC); !extra.Taken.Equal(want) {
		t.Errorf("taken = %v, want %v", extra.Taken, want)
	}
	// 33°51'24.5" S, 151°12'54" E
	if !extra.Located || math.Abs(extra.Latitude+33.856806) > 1e-5 || math.Abs(extra.Longitude-151.215) > 1e-5 {
		t.Errorf("position = %v %v %v", extra.Located, extra.Latitude, extra.Longitude)
	}
	if got := exifOrientation(data); got != 6 {
		t.Errorf("orientation = %d, want 6", got)
	}

	// Anything else says nothing, and breaks nothing.
	for _, junk := range [][]byte{nil, []byte("not a picture"), data[:40], data[:200]} {
		if extra := readExtra(junk); extra.Located || !extra.Taken.IsZero() {
			t.Errorf("junk of %d bytes gave %+v", len(junk), extra)
		}
	}
}

func TestInfoReadsTheFileOfALocalPhoto(t *testing.T) {
	dir := t.TempDir()
	data, _ := os.ReadFile("testdata/located.jpg")
	if err := os.WriteFile(filepath.Join(dir, "harbour.jpg"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	src := NewLocalDir(dir)
	s := New(Options{Source: src, StatePath: filepath.Join(t.TempDir(), "photos.json"), Log: quietLog})
	if err := s.SetSelection(Selection{Folders: []Folder{{ID: ".", Name: "x", Path: "x"}}}); err != nil {
		t.Fatal(err)
	}
	s.index(context.Background())
	_, items := s.Playlist(0)
	if len(items) != 1 {
		t.Fatalf("%d photos, want 1", len(items))
	}
	info, err := s.Info(context.Background(), items[0].Key)
	if err != nil || len(info) != 1 {
		t.Fatalf("info: %v %v", info, err)
	}
	d := info[0]
	if d.Name != "harbour.jpg" || !d.Located || math.Round(d.Longitude*1000) != 151215 || d.Taken.Year() != 2019 || !d.WallClock {
		t.Errorf("detail = %+v", d)
	}
	if _, err := s.Info(context.Background(), "nothing"); err != ErrNotFound {
		t.Errorf("unknown photo: %v", err)
	}
}

// library is a service over n photos in one folder, none of them upright.
func library(t *testing.T, n int) (*Service, string) {
	t.Helper()
	state := filepath.Join(t.TempDir(), "photos.json")
	s := New(Options{Source: Mock{}, StatePath: state})
	s.mu.Lock()
	for i := 0; i < n; i++ {
		it := Item{Key: "p" + string(rune('a'+i/26)) + string(rune('a'+i%26)), Folder: "Album", Taken: time.Unix(int64(i)*1000, 0)}
		s.items = append(s.items, it)
		s.byKey[it.Key] = it
	}
	s.mu.Unlock()
	return s, state
}

func TestFavouritesComeRoundMoreOften(t *testing.T) {
	s, state := library(t, 40)
	if err := s.Favourite(true, "paa", "pab+pac", "nothing"); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Favourites != 3 || st.OnlyFavourites {
		t.Fatalf("status = %+v", st)
	}

	_, slides := s.Slides(0)
	seen := map[string]int{}
	marked := 0
	for i, sl := range slides {
		seen[sl.Key]++
		if i > 0 && slides[i-1].Key == sl.Key {
			t.Errorf("%s twice in a row", sl.Key)
		}
		if sl.Items[0].Favourite {
			marked++
		}
	}
	// Forty photos, and a favourite after the 4th, 12th, 20th, 28th and 36th.
	if len(slides) != 45 || len(seen) != 40 {
		t.Errorf("%d slides of %d photos, want 45 of 40", len(slides), len(seen))
	}
	extra := 0
	for key, n := range seen {
		if n > 1 {
			extra += n - 1
			if key != "paa" && key != "pab" && key != "pac" {
				t.Errorf("%s is no favourite, but comes %d times", key, n)
			}
		}
	}
	if extra != 5 || marked != 8 {
		t.Errorf("%d extra showings and %d marked slides, want 5 and 8", extra, marked)
	}
	// What the dashboard asks for is the most it gets.
	if _, slides := s.Slides(20); len(slides) != 20 {
		t.Errorf("%d slides, want the 20 asked for", len(slides))
	}

	// Only favourites.
	if err := s.OnlyFavourites(true); err != nil {
		t.Fatal(err)
	}
	version, slides := s.Slides(0)
	if len(slides) != 3 {
		t.Errorf("%d slides, want the 3 favourites", len(slides))
	}

	// Remembered across a restart, with the mode.
	again := New(Options{Source: Mock{}, StatePath: state})
	if !again.onlyFavourites || len(again.favourites) != 3 {
		t.Errorf("after a restart: only=%v, %d favourites", again.onlyFavourites, len(again.favourites))
	}
	if st := s.Export(); len(st.Favourites) != 3 {
		t.Errorf("backup has %d favourites", len(st.Favourites))
	}

	// Taking the last one away brings every photo back.
	if err := s.Favourite(false, "paa", "pab", "pac"); err != nil {
		t.Fatal(err)
	}
	st := s.Status()
	if st.OnlyFavourites || st.Favourites != 0 || st.Version == version {
		t.Errorf("status = %+v (version was %d)", st, version)
	}
	if _, slides := s.Slides(0); len(slides) != 40 {
		t.Errorf("%d slides, want all 40", len(slides))
	}
	// With none, "only favourites" cannot be switched on.
	_ = s.OnlyFavourites(true)
	if s.Status().OnlyFavourites {
		t.Error("only favourites without favourites")
	}
}

func TestBackupCarriesFavourites(t *testing.T) {
	state := filepath.Join(t.TempDir(), "photos.json")
	if err := WriteState(state, State{Favourites: []string{"a", "b"}, Hidden: []string{"c"}}); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Source: Mock{}, StatePath: state})
	if len(s.favourites) != 2 || !s.hidden["c"] {
		t.Errorf("favourites = %v, hidden = %v", s.favourites, s.hidden)
	}
}
