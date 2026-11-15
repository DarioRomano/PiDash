package photos

import (
	"math"
	"sort"
	"strings"
	"time"
)

// Trip is a journey the photos tell of: some days away from home.
type Trip struct {
	// ID names the trip in a request for its photos: the day it began.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Named is false where Name is only the coordinates.
	Named bool      `json:"named"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Days  int       `json:"days"`
	Count int       `json:"count"`
	// Latitude and Longitude are the middle of where its photos were taken.
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	// Key and Version name one photo of it, to show as its picture.
	Key     string `json:"key"`
	Version string `json:"v"`

	keys map[string]bool
}

const (
	// awayKm is how far from home a photo must be taken to count as away.
	awayKm = 60
	// tripGap is how long a trip may go without a photo before it counts as over.
	tripGap = 3 * 24 * time.Hour
	// tripPhotos is how many photos make a trip worth listing.
	tripPhotos = 6
	// mostTrips is how many trips are listed: the latest ones.
	mostTrips = 300
)

// distanceKm is the distance between two spots on the globe.
func distanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	const rad = math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 6371 * 2 * math.Asin(math.Min(1, math.Sqrt(a)))
}

// homeOf says where home is: the patch of the map with photos from the most
// different days. ok is false when no photo says where it was taken.
func homeOf(items []Item) (lat, lon float64, ok bool) {
	type cell struct {
		days     map[string]bool
		lat, lon float64
		n        int
	}
	cells := map[[2]int]*cell{}
	for _, it := range items {
		if !it.Located {
			continue
		}
		// A fifth of a degree is some twenty kilometres from north to south.
		k := [2]int{int(math.Round(it.Latitude * 5)), int(math.Round(it.Longitude * 5))}
		c := cells[k]
		if c == nil {
			c = &cell{days: map[string]bool{}}
			cells[k] = c
		}
		c.days[it.Taken.Format("2006-01-02")] = true
		c.lat += it.Latitude
		c.lon += it.Longitude
		c.n++
	}
	var best *cell
	var bestKey [2]int
	for k, c := range cells {
		// The same answer every time, also between two patches with as many days.
		if best == nil || len(c.days) > len(best.days) || (len(c.days) == len(best.days) && (k[0] < bestKey[0] || (k[0] == bestKey[0] && k[1] < bestKey[1]))) {
			best, bestKey = c, k
		}
	}
	if best == nil {
		return 0, 0, false
	}
	return best.lat / float64(best.n), best.lon / float64(best.n), true
}

// tripName names a trip by where most of its photos were taken: the town when
// most are from one, otherwise the country.
func tripName(places []string) string {
	if len(places) == 0 {
		return ""
	}
	count := func(of func(string) string) (string, int) {
		n := map[string]int{}
		best := ""
		for _, p := range places {
			k := of(p)
			if k == "" {
				continue
			}
			n[k]++
			if n[k] > n[best] || (n[k] == n[best] && k < best) || best == "" {
				best = k
			}
		}
		return best, n[best]
	}
	town, towns := count(func(p string) string { return p })
	if towns*10 >= len(places)*6 {
		return town
	}
	country, _ := count(func(p string) string {
		if i := strings.LastIndex(p, ", "); i >= 0 {
			return p[i+2:]
		}
		return p
	})
	return country
}

// tripsLocked works out the trips from the photos. It is done once for each
// state of the library and kept.
func (s *Service) tripsLocked() []Trip {
	stamp := s.version*1_000_000 + len(s.spots)
	if s.tripsFor == stamp && s.trips != nil {
		return s.trips
	}
	s.tripsFor, s.trips = stamp, []Trip{}
	homeLat, homeLon, ok := homeOf(s.items)
	if !ok {
		return s.trips
	}
	order := make([]int, 0, len(s.items))
	for i, it := range s.items {
		if !it.Video && it.Taken.Year() >= 1990 {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := s.items[order[a]], s.items[order[b]]
		if !x.Taken.Equal(y.Taken) {
			return x.Taken.Before(y.Taken)
		}
		return x.Key < y.Key
	})

	var trips []Trip
	var run []Item     // the photos of the trip that is being gathered
	var pending []Item // photos without a place since the last one taken away
	var lastAway time.Time
	finish := func() {
		pending = nil
		if len(run) < tripPhotos {
			run = nil
			return
		}
		t := Trip{Start: run[0].Taken, End: run[len(run)-1].Taken, Count: len(run), keys: map[string]bool{}}
		t.ID = t.Start.Format("2006-01-02")
		y1, m1, d1 := t.Start.Date()
		y2, m2, d2 := t.End.Date()
		t.Days = int(time.Date(y2, m2, d2, 0, 0, 0, 0, time.UTC).Sub(time.Date(y1, m1, d1, 0, 0, 0, 0, time.UTC)).Hours()/24) + 1
		var places []string
		located := 0
		for _, it := range run {
			t.keys[it.Key] = true
			if it.Located {
				t.Latitude += it.Latitude
				t.Longitude += it.Longitude
				located++
			}
			if it.Place != "" {
				places = append(places, it.Place)
			}
		}
		t.Latitude /= float64(located)
		t.Longitude /= float64(located)
		if t.Name = tripName(places); t.Name != "" {
			t.Named = true
		} else {
			t.Name = coordinates(t.Latitude, t.Longitude)
		}
		// Its picture: a favourite if there is one, otherwise one from the middle.
		cover := run[len(run)/2]
		for _, it := range run {
			if s.favourites[it.Key] {
				cover = it
				break
			}
		}
		t.Key, t.Version = cover.Key, cover.Version
		trips = append(trips, t)
		run = nil
	}
	for _, i := range order {
		it := s.items[i]
		away := it.Located && distanceKm(homeLat, homeLon, it.Latitude, it.Longitude) > awayKm
		switch {
		case away:
			if len(run) > 0 && it.Taken.Sub(lastAway) > tripGap {
				finish()
			}
			// What was taken in between without saying where belongs to the trip.
			run = append(append(run, pending...), it)
			pending, lastAway = nil, it.Taken
		case it.Located:
			// Back home: the trip is over.
			if len(run) > 0 {
				finish()
			}
			pending = nil
		case len(run) > 0:
			pending = append(pending, it)
		}
	}
	finish()
	// The latest first.
	sort.SliceStable(trips, func(a, b int) bool { return trips[a].Start.After(trips[b].Start) })
	// Two trips that began on the same day would share a name in requests.
	seen := map[string]bool{}
	out := trips[:0]
	for _, t := range trips {
		if !seen[t.ID] {
			seen[t.ID] = true
			out = append(out, t)
		}
	}
	if len(out) > mostTrips {
		out = out[:mostTrips]
	}
	s.trips = out
	return s.trips
}

// Trips returns the journeys the photos tell of, the latest first.
func (s *Service) Trips() []Trip {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Trip{}, s.tripsLocked()...)
}

// SlidesOfTrip returns the photos of one trip, in the order they were taken.
func (s *Service) SlidesOfTrip(id string, limit int) (version int, slides []Slide) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var trip *Trip
	for i, t := range s.tripsLocked() {
		if t.ID == id {
			trip = &s.trips[i]
		}
	}
	if trip == nil {
		return s.version, nil
	}
	var order []int
	for i, it := range s.items {
		if trip.keys[it.Key] {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := s.items[order[a]], s.items[order[b]]
		if !x.Taken.Equal(y.Taken) {
			return x.Taken.Before(y.Taken)
		}
		return x.Key < y.Key
	})
	if limit <= 0 || limit > len(order) {
		limit = len(order)
	}
	return s.version, s.slidesLocked(order, limit)
}
