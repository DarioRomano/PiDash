package photos

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNewPhotosAreNoticed(t *testing.T) {
	state := filepath.Join(t.TempDir(), "photos.json")
	clock := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	s := New(Options{Source: Mock{}, StatePath: state, Now: func() time.Time { return clock }, Log: quietLog})
	read := func(sv *Service, names ...string) {
		var items []Item
		for i, n := range names {
			folder := "Holidays"
			if n[0] == 'f' {
				folder = "Family"
			}
			items = append(items, Item{Key: n, Folder: folder, Taken: time.Unix(int64(i)*1000, 0)})
		}
		sv.mu.Lock()
		sv.items = items
		sv.byKey = map[string]Item{}
		for _, it := range items {
			sv.byKey[it.Key] = it
		}
		sv.noteNewLocked(items)
		sv.mu.Unlock()
	}

	// The first reading is what later ones are compared with.
	read(s, "h1", "h2", "f1")
	if st := s.Status(); st.New != 0 {
		t.Fatalf("after the first reading %d photos are new", st.New)
	}
	// Three more turn up, two of them in Holidays.
	clock = clock.Add(6 * time.Hour)
	read(s, "h1", "h2", "f1", "h3", "h4", "f2")
	if st := s.Status(); st.New != 3 || st.NewIn != "Holidays" {
		t.Fatalf("new = %d in %q, want 3 in Holidays", st.New, st.NewIn)
	}
	_, slides := s.NewSlides(0)
	if len(slides) != 3 || slides[0].Key != "f2" {
		t.Errorf("new slides = %v, want three with the newest picture first", slides)
	}
	// Still new after a restart, and after another reading that finds the same.
	again := New(Options{Source: Mock{}, StatePath: state, Now: func() time.Time { return clock }, Log: quietLog})
	read(again, "h1", "h2", "f1", "h3", "h4", "f2")
	if st := again.Status(); st.New != 3 {
		t.Errorf("after a restart %d are new, want 3", st.New)
	}
	// A photo that is gone again is not new; after a week none is.
	read(s, "h1", "h2", "f1", "h3", "f2")
	if st := s.Status(); st.New != 2 {
		t.Errorf("new = %d after one was removed, want 2", st.New)
	}
	clock = clock.Add(8 * 24 * time.Hour)
	if st := s.Status(); st.New != 0 {
		t.Errorf("after a week %d are still new", st.New)
	}

	// Looked at: new no longer.
	clock = clock.Add(time.Hour)
	read(s, "h1", "h2", "f1", "h3", "f2", "h9")
	if s.Status().New != 1 {
		t.Fatal("a photo that arrived later should be new")
	}
	s.SeenNew()
	if s.Status().New != 0 {
		t.Error("new after they were looked at")
	}
	// Choosing other folders is not news either.
	read(s, "h1", "h2", "f1", "h3", "f2", "h9", "h10")
	if err := s.SetSelection(Selection{Folders: []Folder{{ID: "Family", Name: "Family", Path: "Family"}}}); err != nil {
		t.Fatal(err)
	}
	read(s, "f1", "f2", "f3", "f4")
	if st := s.Status(); st.New != 0 {
		t.Errorf("after choosing other folders %d photos are new", st.New)
	}
}

func TestAHiddenPhotoCanBeTakenBack(t *testing.T) {
	s, _ := library(t, 6)
	if err := s.Hide("paa+pab", "pac"); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Hidden != 3 {
		t.Fatalf("hidden = %d, want 3", st.Hidden)
	}
	if err := s.Unhide("paa+pab", "nothing"); err != nil {
		t.Fatal(err)
	}
	if st := s.Status(); st.Hidden != 1 || !s.hidden["pac"] {
		t.Errorf("hidden = %d, want only the third still hidden", st.Hidden)
	}
	// It comes back with the next reading, which was asked for.
	select {
	case <-s.reindex:
	default:
		t.Error("taking a photo back should have the folders read again")
	}
}
