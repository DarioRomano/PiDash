package photos

import (
	"math/rand"
	"sort"
	"strconv"
	"time"
)

// Question is one round of the photo quiz: a photo, and four answers to
// "which year?" or "where?", one of them right.
type Question struct {
	Key     string `json:"key"`
	Version string `json:"v"`
	// Kind is "year" or "place".
	Kind    string   `json:"kind"`
	Answers []string `json:"answers"`
	// Right is the place of the right answer among Answers.
	Right int `json:"right"`
}

const (
	// quizYears is how many different years the photos must be from before
	// "which year?" is worth asking, and quizPlaces how many places for "where?".
	quizYears  = 3
	quizPlaces = 4
	// quizLeast is how many photos must be askable for there to be a quiz.
	quizLeast = 10
)

// quizStock is what the quiz can draw on.
type quizStock struct {
	years, places   []Item
	placeNames      []string
	first           int // the earliest year a photo is from
	askYear, askAny bool
}

func (s *Service) quizStockLocked() quizStock {
	var q quizStock
	years, places := map[int]bool{}, map[string]bool{}
	for _, it := range s.items {
		if it.Video {
			continue
		}
		if y := it.Taken.Year(); y >= 1990 {
			q.years = append(q.years, it)
			years[y] = true
			if q.first == 0 || y < q.first {
				q.first = y
			}
		}
		if it.Place != "" {
			q.places = append(q.places, it)
			if !places[it.Place] {
				places[it.Place] = true
				q.placeNames = append(q.placeNames, it.Place)
			}
		}
	}
	sort.Strings(q.placeNames)
	if len(years) < quizYears {
		q.years = nil
	}
	if len(places) < quizPlaces {
		q.places, q.placeNames = nil, nil
	}
	q.askYear = len(q.years) > 0
	seen := map[string]bool{}
	for _, it := range append(append([]Item{}, q.years...), q.places...) {
		seen[it.Key] = true
	}
	q.askAny = len(seen) >= quizLeast
	return q
}

// Quiz returns up to n questions about different photos. rnd chooses; nil
// uses the clock. now is the year no answer may lie after.
func (s *Service) Quiz(n int, rnd *rand.Rand, now time.Time) []Question {
	if rnd == nil {
		rnd = rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // a game, not a secret
	}
	s.mu.Lock()
	stock := s.quizStockLocked()
	s.mu.Unlock()
	if !stock.askAny {
		return []Question{}
	}
	// Every photo that can be asked about, once, in an order of chance; of a
	// photo that can be asked about either way, chance chooses which.
	type ask struct {
		item Item
		kind string
	}
	byKey := map[string]*ask{}
	var all []*ask
	add := func(items []Item, kind string) {
		for _, it := range items {
			if a := byKey[it.Key]; a != nil {
				if rnd.Intn(2) == 0 {
					a.kind = kind
				}
				continue
			}
			a := &ask{it, kind}
			byKey[it.Key] = a
			all = append(all, a)
		}
	}
	add(stock.years, "year")
	add(stock.places, "place")
	sort.Slice(all, func(a, b int) bool { return all[a].item.Key < all[b].item.Key })
	rnd.Shuffle(len(all), func(a, b int) { all[a], all[b] = all[b], all[a] })
	if n > len(all) {
		n = len(all)
	}
	out := make([]Question, 0, n)
	for _, a := range all[:n] {
		q := Question{Key: a.item.Key, Version: a.item.Version, Kind: a.kind}
		if a.kind == "year" {
			// Three years close by, none of them in the future.
			year := a.item.Taken.Year()
			var near []int
			for d := -3; d <= 3; d++ {
				if y := year + d; d != 0 && y <= now.Year() && y >= 1990 {
					near = append(near, y)
				}
			}
			rnd.Shuffle(len(near), func(i, j int) { near[i], near[j] = near[j], near[i] })
			years := append([]int{year}, near[:3]...)
			sort.Ints(years)
			for i, y := range years {
				q.Answers = append(q.Answers, strconv.Itoa(y))
				if y == year {
					q.Right = i
				}
			}
		} else {
			// Three other places the photos know.
			var others []string
			for _, p := range stock.placeNames {
				if p != a.item.Place {
					others = append(others, p)
				}
			}
			rnd.Shuffle(len(others), func(i, j int) { others[i], others[j] = others[j], others[i] })
			q.Answers = append([]string{a.item.Place}, others[:3]...)
			rnd.Shuffle(len(q.Answers), func(i, j int) { q.Answers[i], q.Answers[j] = q.Answers[j], q.Answers[i] })
			for i, p := range q.Answers {
				if p == a.item.Place {
					q.Right = i
				}
			}
		}
		out = append(out, q)
	}
	return out
}
