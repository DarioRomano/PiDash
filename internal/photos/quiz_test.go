package photos

import (
	"context"
	"math/rand"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestTheQuizAsksAboutYearsAndPlaces(t *testing.T) {
	// The sample holidays are all from one year, in two places: nothing to ask.
	small := New(Options{Source: Mock{}, StatePath: filepath.Join(t.TempDir(), "a.json"), Log: quietLog})
	_ = small.SetSelection(Selection{Recursive: true, Folders: []Folder{{ID: "Holidays", Name: "Holidays", Path: "Holidays"}}})
	small.index(context.Background())
	if q := small.Quiz(10, nil, time.Now()); len(q) != 0 || small.Status().Quiz {
		t.Fatalf("a quiz about one year and two places: %+v", q)
	}

	// Photos from five years and five places.
	s := New(Options{Source: Mock{}, StatePath: filepath.Join(t.TempDir(), "b.json"), Log: quietLog})
	places := []string{"Rome, Italy", "Paris, France", "Oslo, Norway", "Vienna, Austria", "Porto, Portugal"}
	var items []Item
	for i := 0; i < 30; i++ {
		it := Item{Key: "k" + strconv.Itoa(i), Version: "1", Taken: time.Date(2019+i%5, 6, 1+i, 12, 0, 0, 0, time.UTC)}
		if i%3 != 0 {
			it.Place = places[i%5]
		}
		items = append(items, it)
	}
	items = append(items, Item{Key: "clip", Video: true, Place: "Rome, Italy", Taken: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)})
	s.mu.Lock()
	s.items = items
	s.mu.Unlock()
	if !s.Status().Quiz {
		t.Fatal("the status does not offer the quiz")
	}
	now := time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC)
	byKey := map[string]Item{}
	for _, it := range items {
		byKey[it.Key] = it
	}
	kinds := map[string]int{}
	for seed := int64(1); seed <= 20; seed++ {
		round := s.Quiz(10, rand.New(rand.NewSource(seed)), now)
		if len(round) != 10 {
			t.Fatalf("%d questions, want 10", len(round))
		}
		asked := map[string]bool{}
		for _, q := range round {
			it := byKey[q.Key]
			if asked[q.Key] || it.Video {
				t.Fatalf("asked twice, or about a clip: %+v", q)
			}
			asked[q.Key] = true
			kinds[q.Kind]++
			if len(q.Answers) != 4 || q.Right < 0 || q.Right > 3 {
				t.Fatalf("answers: %+v", q)
			}
			different := map[string]bool{}
			for _, a := range q.Answers {
				different[a] = true
			}
			if len(different) != 4 {
				t.Fatalf("the same answer twice: %+v", q)
			}
			switch q.Kind {
			case "year":
				if q.Answers[q.Right] != strconv.Itoa(it.Taken.Year()) {
					t.Fatalf("the right year of %s is not %s: %+v", it.Taken, q.Answers[q.Right], q)
				}
				for i, a := range q.Answers {
					y, _ := strconv.Atoi(a)
					if y > 2023 || (i > 0 && a <= q.Answers[i-1]) {
						t.Fatalf("years in the future or out of order: %+v", q)
					}
				}
			case "place":
				if it.Place == "" || q.Answers[q.Right] != it.Place {
					t.Fatalf("the right place: %+v (photo %+v)", q, it)
				}
			default:
				t.Fatalf("kind %q", q.Kind)
			}
		}
	}
	if kinds["year"] == 0 || kinds["place"] == 0 {
		t.Fatalf("only one kind of question was asked: %v", kinds)
	}
	// The same chance gives the same round.
	a := s.Quiz(5, rand.New(rand.NewSource(7)), now)
	b := s.Quiz(5, rand.New(rand.NewSource(7)), now)
	for i := range a {
		if a[i].Key != b[i].Key || a[i].Right != b[i].Right {
			t.Fatal("the same seed gave another round")
		}
	}
}
