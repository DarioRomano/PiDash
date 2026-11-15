package photos

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// --- Photos the person likes best -------------------------------------------------
//
// A favourite comes round more often than other photos, and the slideshow
// can be told to show nothing else.

// favouriteEvery is how often a favourite is put in between the other
// photos: about one picture in eight.
const favouriteEvery = 8

type favouriteState struct {
	Keys []string `json:"keys"`
	// Only is whether the slideshow shows favourites and nothing else.
	Only bool `json:"only"`
}

func (s *Service) favouritesPath() string {
	if s.opt.StatePath == "" {
		return ""
	}
	return strings.TrimSuffix(s.opt.StatePath, filepath.Ext(s.opt.StatePath)) + "-favourites.json"
}

func (s *Service) loadFavourites() {
	s.favourites = map[string]bool{}
	if p := s.favouritesPath(); p != "" {
		var st favouriteState
		if data, err := os.ReadFile(p); err == nil && json.Unmarshal(data, &st) == nil {
			for _, k := range st.Keys {
				s.favourites[k] = true
			}
			s.onlyFavourites = st.Only
		}
	}
}

func (s *Service) favouriteKeysLocked() []string {
	keys := make([]string, 0, len(s.favourites))
	for k := range s.favourites {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *Service) saveFavouritesLocked() error {
	p := s.favouritesPath()
	if p == "" {
		return nil
	}
	data, err := json.Marshal(favouriteState{Keys: s.favouriteKeysLocked(), Only: s.onlyFavourites})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// favouritesLocked counts the favourites that are in the slideshow now. One
// whose album is no longer chosen is remembered, but not counted.
func (s *Service) favouritesLocked() int {
	n := 0
	for k := range s.favourites {
		if _, ok := s.byKey[k]; ok {
			n++
		}
	}
	return n
}

// Favourite marks photos as favourites, or takes the mark away. A key may
// name two photos shown side by side ("a+b"). Unknown keys are ignored.
func (s *Service) Favourite(on bool, keys ...string) error {
	s.mu.Lock()
	changed := false
	for _, key := range keys {
		for _, k := range strings.Split(key, pairSep) {
			if _, ok := s.byKey[k]; ok && s.favourites[k] != on {
				if on {
					s.favourites[k] = true
				} else {
					delete(s.favourites, k)
				}
				changed = true
			}
		}
	}
	if !changed {
		s.mu.Unlock()
		return nil
	}
	if s.onlyFavourites && s.favouritesLocked() == 0 {
		// The last one is gone: back to all photos, or the screen stays empty.
		s.onlyFavourites = false
		s.version++
	}
	err := s.saveFavouritesLocked()
	s.mu.Unlock()
	s.notify()
	return err
}

// OnlyFavourites makes the slideshow show favourites and nothing else, or
// everything again.
func (s *Service) OnlyFavourites(on bool) error {
	s.mu.Lock()
	if on && s.favouritesLocked() == 0 {
		on = false
	}
	if s.onlyFavourites == on {
		s.mu.Unlock()
		return nil
	}
	s.onlyFavourites = on
	s.version++ // the dashboard fetches its photos again
	err := s.saveFavouritesLocked()
	s.mu.Unlock()
	s.notify()
	return err
}

// favouriteOrderLocked returns the favourites, by their position in s.items,
// in random order.
func (s *Service) favouriteOrderLocked() []int {
	var order []int
	for i, it := range s.items {
		if s.favourites[it.Key] {
			order = append(order, i)
		}
	}
	rand.Shuffle(len(order), func(a, b int) { order[a], order[b] = order[b], order[a] })
	return order
}

// withFavouritesLocked puts a favourite in between the slides now and then,
// so that they are seen more often than their share of a big library.
func (s *Service) withFavouritesLocked(slides []Slide, limit int) []Slide {
	order := s.favouriteOrderLocked()
	// Nothing to do without favourites, and when most photos are favourites.
	if len(order) == 0 || len(order)*2 >= len(s.items) || len(slides) < favouriteEvery {
		return slides
	}
	extra := s.slidesLocked(order, len(order))
	out := make([]Slide, 0, len(slides)+len(slides)/favouriteEvery+1)
	next := 0
	for i, slide := range slides {
		out = append(out, slide)
		if (i+1)%favouriteEvery != favouriteEvery/2 {
			continue
		}
		// Not the same picture twice in a row.
		for tries := 0; tries < len(extra); tries++ {
			candidate := extra[next%len(extra)]
			next++
			if candidate.Key == slide.Key || (i+1 < len(slides) && slides[i+1].Key == candidate.Key) {
				continue
			}
			out = append(out, candidate)
			break
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
