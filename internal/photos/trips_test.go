package photos

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func journeys(t *testing.T) *Service {
	t.Helper()
	s := New(Options{Source: Mock{}, StatePath: filepath.Join(t.TempDir(), "photos.json"), Log: quietLog})
	if err := s.SetSelection(Selection{Recursive: true, Folders: []Folder{{ID: "Journeys", Name: "Journeys", Path: "Journeys"}}}); err != nil {
		t.Fatal(err)
	}
	s.index(context.Background())
	return s
}

func TestTripsAreWorkedOutFromPlacesAndDates(t *testing.T) {
	s := journeys(t)
	trips := s.Trips()
	var got []string
	for _, tr := range trips {
		got = append(got, tr.Name+" "+tr.ID)
	}
	// The latest first; the photos at home are no trip.
	if strings.Join(got, " | ") != "Lisbon, Portugal 2023-09-02 | Reykjavik, Iceland 2023-03-10" {
		t.Fatalf("trips = %q", got)
	}
	lisbon := trips[0]
	if lisbon.Count != 9 || lisbon.Days != 5 || !lisbon.Named || lisbon.Key == "" || lisbon.Latitude < 38.7 || lisbon.Latitude > 38.75 {
		t.Errorf("Lisbon = %+v", lisbon)
	}
	if st := s.Status(); st.Trips != 2 {
		t.Errorf("status counts %d trips", st.Trips)
	}

	// Its photos, in the order they were taken.
	_, slides := s.SlidesOfTrip(lisbon.ID, 0)
	if len(slides) != 9 {
		t.Fatalf("%d slides, want 9", len(slides))
	}
	for i, sl := range slides {
		if sl.Items[0].Place != "Lisbon, Portugal" {
			t.Errorf("slide %d is from %q", i, sl.Items[0].Place)
		}
		if i > 0 && sl.Items[0].Taken.Before(slides[i-1].Items[0].Taken) {
			t.Errorf("slide %d was taken before the one in front of it", i)
		}
	}
	if _, four := s.SlidesOfTrip(lisbon.ID, 4); len(four) != 4 {
		t.Errorf("limit 4 gave %d", len(four))
	}
	if _, none := s.SlidesOfTrip("1999-01-01", 0); len(none) != 0 {
		t.Errorf("a trip that is not there has %d photos", len(none))
	}

	// A favourite is the trip's picture.
	_, all := s.SlidesOfTrip(lisbon.ID, 0)
	fav := all[7].Items[0].Key
	if err := s.Favourite(true, fav); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.tripsFor = -1 // as a new reading of the folders would
	s.mu.Unlock()
	if again := s.Trips(); again[0].Key != fav {
		t.Errorf("the trip's picture is %q, want the favourite %q", again[0].Key, fav)
	}
}

func TestWhatMakesATrip(t *testing.T) {
	day := func(d int, h int) time.Time { return time.Date(2024, 5, d, h, 0, 0, 0, time.UTC) }
	at := func(key string, when time.Time, lat, lon float64, place string) Item {
		return Item{Key: key, Taken: when, Located: lat != 0, Latitude: lat, Longitude: lon, Place: place, Version: "1"}
	}
	home := func(key string, d int) Item { return at(key, day(d, 9), 47.37, 8.54, "Zurich, Switzerland") }
	s := New(Options{Source: Mock{}, StatePath: filepath.Join(t.TempDir(), "p.json"), Log: quietLog})
	set := func(items ...Item) []Trip {
		s.mu.Lock()
		s.items = items
		s.version++
		s.mu.Unlock()
		return s.Trips()
	}
	var base []Item
	for d := 1; d <= 5; d++ {
		base = append(base, home("h"+string(rune('0'+d)), d))
	}

	// Nothing says where it was taken: no home, no trips.
	if got := set(at("a", day(1, 9), 0, 0, ""), at("b", day(2, 9), 0, 0, "")); len(got) != 0 {
		t.Fatalf("trips without places: %+v", got)
	}
	// Too few photos are not worth a line.
	few := append(append([]Item{}, base...), at("r1", day(10, 9), 41.9, 12.5, "Rome, Italy"), at("r2", day(10, 12), 41.9, 12.5, "Rome, Italy"))
	if got := set(few...); len(got) != 0 {
		t.Fatalf("two photos made a trip: %+v", got)
	}
	// A walk near home is no trip.
	near := append([]Item{}, base...)
	for i := 0; i < 8; i++ {
		near = append(near, at("n"+string(rune('0'+i)), day(12, 8+i), 47.45, 8.60, "Winterthur, Switzerland"))
	}
	if got := set(near...); len(got) != 0 {
		t.Fatalf("a walk near home made a trip: %+v", got)
	}
	// Through several towns of one country: named by the country. Photos
	// without a place in between belong to it; a photo at home ends it; and a
	// week without photos separates two trips.
	tour := append([]Item{}, base...)
	towns := []string{"Rome, Italy", "Florence, Italy", "Venice, Italy", "Bologna, Italy"}
	for i := 0; i < 8; i++ {
		tour = append(tour, at("t"+string(rune('0'+i)), day(10+i/2, 9+i), 42+float64(i)*0.4, 12, towns[i/2]))
	}
	tour = append(tour, at("nowhere", day(12, 20), 0, 0, ""))
	tour = append(tour, home("back", 16), at("after", day(16, 12), 0, 0, ""))
	for i := 0; i < 6; i++ {
		tour = append(tour, at("p"+string(rune('0'+i)), day(24, 8+i), 48.85, 2.35, "Paris, France"))
	}
	got := set(tour...)
	if len(got) != 2 || got[0].Name != "Paris, France" || got[1].Name != "Italy" {
		t.Fatalf("trips = %+v", got)
	}
	if italy := got[1]; italy.Count != 9 || italy.Days != 4 || !italy.keys["nowhere"] || italy.keys["after"] || italy.keys["back"] {
		t.Errorf("Italy = %+v", italy)
	}
	// Without names, a trip is named by where it was.
	bare := append([]Item{}, base...)
	for i := 0; i < 6; i++ {
		bare = append(bare, at("b"+string(rune('0'+i)), day(20, 8+i), -33.86, 151.21, ""))
	}
	if got := set(bare...); len(got) != 1 || got[0].Named || got[0].Name != "33.9° S, 151.2° E" {
		t.Fatalf("a trip without names: %+v", got)
	}
}
