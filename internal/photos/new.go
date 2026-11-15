package photos

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// --- Photos that are new --------------------------------------------------------------
//
// When pictures are added to a chosen folder or album on the NAS, the
// slideshow picks them up at its next reading, among thousands of others.
// Here they are noticed: each complete reading is compared with the one
// before, and what was not there is "new" for a week, or until it was looked
// at. The first reading, and the first after other folders were chosen, is
// what everything else is compared with: nothing is new then.

// newFor is how long a photo counts as new.
const newFor = 7 * 24 * time.Hour

// newMost is how many photos are remembered as new at most. A whole library
// arriving at once is not news about photos.
const newMost = 2000

type newState struct {
	Known []string             `json:"known"`
	Fresh map[string]time.Time `json:"fresh"`
}

func (s *Service) newPath() string {
	if s.opt.StatePath == "" {
		return ""
	}
	return strings.TrimSuffix(s.opt.StatePath, filepath.Ext(s.opt.StatePath)) + "-new.json"
}

func (s *Service) loadNew() {
	s.fresh = map[string]time.Time{}
	if p := s.newPath(); p != "" {
		var st newState
		if data, err := os.ReadFile(p); err == nil && json.Unmarshal(data, &st) == nil {
			// Nothing known means nothing was read yet: the next reading
			// is then what later ones are compared with.
			if len(st.Known) > 0 {
				s.known = make(map[string]bool, len(st.Known))
			}
			for _, k := range st.Known {
				s.known[k] = true
			}
			if st.Fresh != nil {
				s.fresh = st.Fresh
			}
		}
	}
}

func (s *Service) saveNewLocked() {
	p := s.newPath()
	if p == "" {
		return
	}
	st := newState{Known: make([]string, 0, len(s.known)), Fresh: s.fresh}
	for k := range s.known {
		st.Known = append(st.Known, k)
	}
	sort.Strings(st.Known)
	data, err := json.Marshal(st)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	if os.WriteFile(p+".tmp", data, 0o600) == nil {
		_ = os.Rename(p+".tmp", p)
	}
}

func (s *Service) now() time.Time {
	if s.opt.Now != nil {
		return s.opt.Now()
	}
	return time.Now()
}

// noteNewLocked compares a complete reading with the one before.
func (s *Service) noteNewLocked(items []Item) {
	now := s.now()
	first := s.known == nil
	known := make(map[string]bool, len(items))
	added := 0
	for _, it := range items {
		known[it.Key] = true
		if !first && !s.known[it.Key] {
			if _, already := s.fresh[it.Key]; !already {
				s.fresh[it.Key] = now
				added++
			}
		}
	}
	changed := first || added > 0 || len(known) != len(s.known)
	// Gone again, grown old, or too many to be news.
	for k, at := range s.fresh {
		if !known[k] || now.Sub(at) > newFor {
			delete(s.fresh, k)
			changed = true
		}
	}
	if len(s.fresh) > newMost {
		s.fresh = map[string]time.Time{}
	}
	s.known = known
	if changed {
		s.saveNewLocked()
	}
}

// newLocked counts the new photos that are in the slideshow, and names the
// folder or album that has most of them.
func (s *Service) newLocked() (int, string) {
	if len(s.fresh) == 0 {
		return 0, ""
	}
	now := s.now()
	in := map[string]int{}
	n := 0
	for k, at := range s.fresh {
		if it, ok := s.byKey[k]; ok && now.Sub(at) <= newFor {
			n++
			in[it.Folder]++
		}
	}
	best := ""
	for folder, count := range in {
		if count > in[best] || (count == in[best] && folder < best) {
			best = folder
		}
	}
	return n, best
}

// NewSlides returns the new photos, the newest picture first, as slides.
func (s *Service) NewSlides(limit int) (version int, slides []Slide) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var order []int
	for i, it := range s.items {
		if _, ok := s.fresh[it.Key]; ok {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return s.items[order[a]].Taken.After(s.items[order[b]].Taken) })
	if limit <= 0 || limit > len(order) {
		limit = len(order)
	}
	return s.version, s.slidesLocked(order, limit)
}

// SeenNew says that the new photos were looked at: they are new no longer.
func (s *Service) SeenNew() {
	s.mu.Lock()
	if len(s.fresh) == 0 {
		s.mu.Unlock()
		return
	}
	s.fresh = map[string]time.Time{}
	s.saveNewLocked()
	s.mu.Unlock()
	s.notify()
}

// MarkNew makes up to n photos count as new, as if they had just arrived on
// the NAS. It is for the tests of the pages: sample photos never change.
func (s *Service) MarkNew(n int) {
	s.mu.Lock()
	for _, it := range s.items {
		if n <= 0 {
			break
		}
		s.fresh[it.Key] = s.now()
		n--
	}
	s.mu.Unlock()
	s.notify()
}
