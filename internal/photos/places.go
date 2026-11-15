package photos

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Place is somewhere photos were taken: a town the source names, or, where
// it names none, a patch of the map some ten kilometres across.
type Place struct {
	// ID names the place in a request for its photos.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Named is false where Name is only the coordinates.
	Named bool `json:"named"`
	Count int  `json:"count"`
	// Latitude and Longitude are the middle of the photos taken there.
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	// First and Last are the years of the oldest and the newest photo.
	First int `json:"first"`
	Last  int `json:"last"`
	// Key and Version name one photo from there, to show as its picture.
	Key     string `json:"key"`
	Version string `json:"v"`
}

// mostPlaces is how many places are listed: the ones with the most photos.
const mostPlaces = 300

// placeID says which place a photo belongs to; "" for one that does not say
// where it was taken.
func placeID(it Item) string {
	if !it.Located {
		return ""
	}
	if it.Place != "" {
		return "n:" + it.Place
	}
	// A tenth of a degree is about eleven kilometres from north to south.
	return fmt.Sprintf("c:%.1f,%.1f", math.Round(it.Latitude*10)/10, math.Round(it.Longitude*10)/10)
}

// Coordinates writes a spot the way a map's margin does: "38.7° N, 9.4° W".
func coordinates(lat, lon float64) string {
	part := func(v float64, pos, neg string) string {
		if v < 0 {
			return fmt.Sprintf("%.1f° %s", -v, neg)
		}
		return fmt.Sprintf("%.1f° %s", v, pos)
	}
	return part(lat, "N", "S") + ", " + part(lon, "E", "W")
}

// Places returns where the photos were taken, the places with the most
// photos first.
func (s *Service) Places() []Place {
	s.mu.Lock()
	defer s.mu.Unlock()
	byID := map[string]*Place{}
	newest := map[string]time.Time{}
	var out []*Place
	for _, it := range s.items {
		id := placeID(it)
		if id == "" || it.Video {
			continue
		}
		p := byID[id]
		if p == nil {
			p = &Place{ID: id, Name: it.Place, Named: it.Place != ""}
			byID[id] = p
			out = append(out, p)
		}
		p.Count++
		p.Latitude += it.Latitude
		p.Longitude += it.Longitude
		if year := it.Taken.Year(); year >= 1990 {
			if p.First == 0 || year < p.First {
				p.First = year
			}
			if year > p.Last {
				p.Last = year
			}
		}
		// The newest photo is the place's picture.
		if p.Key == "" || it.Taken.After(newest[id]) {
			p.Key, p.Version, newest[id] = it.Key, it.Version, it.Taken
		}
	}
	places := make([]Place, 0, len(out))
	for _, p := range out {
		p.Latitude /= float64(p.Count)
		p.Longitude /= float64(p.Count)
		if !p.Named {
			p.Name = coordinates(p.Latitude, p.Longitude)
		}
		places = append(places, *p)
	}
	sort.SliceStable(places, func(a, b int) bool {
		if places[a].Count != places[b].Count {
			return places[a].Count > places[b].Count
		}
		return places[a].Name < places[b].Name
	})
	if len(places) > mostPlaces {
		places = places[:mostPlaces]
	}
	return places
}

// SlidesAt returns the photos taken at one place, the newest first.
func (s *Service) SlidesAt(place string, limit int) (version int, slides []Slide) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var order []int
	for i, it := range s.items {
		if place != "" && placeID(it) == place {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := s.items[order[a]], s.items[order[b]]
		if !x.Taken.Equal(y.Taken) {
			return x.Taken.After(y.Taken)
		}
		return x.Key < y.Key
	})
	if limit <= 0 || limit > len(order) {
		limit = len(order)
	}
	return s.version, s.slidesLocked(order, limit)
}

// --- Where a folder's photos were taken ---------------------------------------------------
// Synology Photos says where each photo was taken as it lists them. A folder
// on disk does not: there the place is in each file, and is read once, in
// the background, and remembered.

// spot is what a file said about where it was taken; Located false means it
// was looked at and said nothing.
type spot struct {
	Located   bool    `json:"l,omitempty"`
	Latitude  float64 `json:"a,omitempty"`
	Longitude float64 `json:"o,omitempty"`
}

func (s *Service) spotsPath() string {
	if s.opt.CacheDir == "" {
		return ""
	}
	return filepath.Join(s.opt.CacheDir, "places.json")
}

func (s *Service) loadSpots() {
	s.spots = map[string]spot{}
	if p := s.spotsPath(); p != "" && s.opt.Places {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &s.spots)
		}
	}
}

// saveSpots stores what is known, leaving out photos that are gone.
func (s *Service) saveSpots() {
	p := s.spotsPath()
	if p == "" {
		return
	}
	s.mu.Lock()
	if len(s.items) > 0 {
		current := make(map[string]spot, len(s.items))
		for _, it := range s.items {
			if sp, ok := s.spots[shapeKey(it)]; ok {
				current[shapeKey(it)] = sp
			}
		}
		s.spots = current
	}
	data, err := json.Marshal(s.spots)
	s.mu.Unlock()
	if err != nil {
		return
	}
	if err := os.WriteFile(p+".tmp", data, 0o600); err == nil {
		_ = os.Rename(p+".tmp", p)
	}
}

// withSpotsLocked gives photos the place their file is known to hold.
func (s *Service) withSpotsLocked(items []Item) {
	if len(s.spots) == 0 {
		return
	}
	for i := range items {
		if items[i].Located {
			continue
		}
		if sp, ok := s.spots[shapeKey(items[i])]; ok && sp.Located {
			items[i].Located, items[i].Latitude, items[i].Longitude = true, sp.Latitude, sp.Longitude
		}
	}
}

// scanSpots looks into every photo whose place is not known yet. It reads
// only the start of each file, so a large library takes minutes, once.
func (s *Service) scanSpots(ctx context.Context) {
	src, ok := s.opt.Source.(Detailer)
	if !ok || !s.opt.Places {
		return
	}
	s.mu.Lock()
	var todo []Item
	for _, it := range s.items {
		if _, known := s.spots[shapeKey(it)]; !known && !it.Located && !it.Video {
			todo = append(todo, it)
		}
	}
	s.mu.Unlock()
	if len(todo) == 0 {
		return
	}
	start := time.Now()
	publish := func() {
		s.saveSpots()
		s.mu.Lock()
		s.withSpotsLocked(s.items)
		for _, it := range s.items {
			s.byKey[it.Key] = it
		}
		s.mu.Unlock()
	}
	looked, found, failed := 0, 0, 0
	for _, it := range todo {
		if ctx.Err() != nil {
			return
		}
		extra, err := src.Details(ctx, it)
		if err != nil {
			// The folder is probably away; the next index tries again.
			if failed++; failed >= 20 {
				break
			}
			continue
		}
		failed = 0
		s.mu.Lock()
		s.spots[shapeKey(it)] = spot{Located: extra.Located, Latitude: extra.Latitude, Longitude: extra.Longitude}
		s.mu.Unlock()
		if extra.Located {
			found++
		}
		if looked++; looked%500 == 0 {
			publish()
		}
	}
	publish()
	s.opt.Log.Info("looked at where photos were taken", "photos", looked, "with a place", found, "took", time.Since(start).Round(time.Millisecond))
}
