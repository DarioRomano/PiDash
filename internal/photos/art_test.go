package photos

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// A pretend museum. With strict set it refuses the precise question, as a
// service might that reads it differently than expected.
func museum(t *testing.T, strict bool) (*Art, *[]string) {
	t.Helper()
	var asked []string
	var srv *httptest.Server
	picture := jpegBytes(uniform(1686, 1200, color.RGBA{120, 90, 60, 255}))
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.String())
		if r.Header.Get("AIC-User-Agent") != "PiDash/test" {
			t.Errorf("asked as %q", r.Header.Get("AIC-User-Agent"))
		}
		switch {
		case r.URL.Path == "/api/v1/artworks/search":
			q := r.URL.Query()
			exact := q.Get("query[bool][must][0][term][is_public_domain]") == "true"
			if strict && exact {
				http.Error(w, `{"status":400}`, http.StatusBadRequest)
				return
			}
			if !exact && q.Get("query[term][is_public_domain]") != "true" {
				t.Errorf("asked for works that may not be free to show: %s", r.URL.RawQuery)
			}
			yes, no := true, false
			type work = map[string]any
			page1 := []work{
				{"id": 1, "title": " Water Lilies ", "artist_title": "Claude Monet", "date_display": "1906", "image_id": "aaaaaaaa-1111", "artwork_type_title": "Painting", "is_public_domain": yes},
				{"id": 2, "title": "A Vase", "artist_title": "", "date_display": "c. 500 BCE", "image_id": "bbbbbbbb-2222", "artwork_type_title": "Vessel", "is_public_domain": yes},
				{"id": 3, "title": "Not free", "artist_title": "Someone", "image_id": "cccccccc-3333", "artwork_type_title": "Painting", "is_public_domain": no},
				{"id": 4, "title": "No picture", "artist_title": "Someone", "image_id": "", "artwork_type_title": "Painting", "is_public_domain": yes},
				{"id": 5, "title": "Untitled", "artist_title": "", "date_display": "", "image_id": "eeeeeeee-5555", "artwork_type_title": "Painting", "is_public_domain": yes},
				{"id": 6, "title": "Climbing", "artist_title": "X", "image_id": "../../secret", "artwork_type_title": "Painting", "is_public_domain": yes},
			}
			data := page1
			if q.Get("page") != "1" {
				data = []work{{"id": 1, "title": "Water Lilies again", "image_id": "aaaaaaaa-1111", "artwork_type_title": "Painting"}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"pagination": map[string]int{"total_pages": 1}, "data": data,
				"config": map[string]string{"iiif_url": srv.URL + "/iiif/2"},
			})
		case strings.HasPrefix(r.URL.Path, "/iiif/2/eeeeeeee-5555/full/1686,"):
			http.Error(w, "too large for this work", http.StatusForbidden)
		case strings.HasPrefix(r.URL.Path, "/iiif/2/") && strings.HasSuffix(r.URL.Path, "/0/default.jpg"):
			_, _ = w.Write(picture)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	a := NewArt("PiDash/test")
	a.API = srv.URL + "/api/v1"
	return a, &asked
}

func TestPaintingsFromTheMuseum(t *testing.T) {
	for _, strict := range []bool{false, true} {
		a, asked := museum(t, strict)
		ctx := context.Background()
		items, err := a.Items(ctx, artFolder)
		if err != nil {
			t.Fatalf("strict %v: %v", strict, err)
		}
		var got []string
		for _, it := range items {
			got = append(got, fmt.Sprintf("%s|%s|%s|%s", it.Key, it.Name, it.Folder, it.Place))
		}
		// Only paintings that are free to show and have a picture; none twice.
		if strings.Join(got, "\n") != "art-1|Water Lilies|Claude Monet|1906\nart-5|Untitled|Artist unknown|" {
			t.Fatalf("strict %v: works =\n%s", strict, strings.Join(got, "\n"))
		}
		if strict && len(*asked) != 2 {
			t.Errorf("after a refusal the plain question is asked once: %v", *asked)
		}
		// The picture, at the larger size; where the museum has none that large, the smaller.
		for _, it := range items {
			data, upright, err := a.Fetch(ctx, it, true)
			if err != nil || !upright || len(data) < 1000 {
				t.Fatalf("picture of %s: %d bytes, %v", it.Name, len(data), err)
			}
		}
		if last := (*asked)[len(*asked)-1]; !strings.Contains(last, "/eeeeeeee-5555/full/843,/0/default.jpg") {
			t.Errorf("the smaller size was not tried: %s", last)
		}
		if _, _, err := a.Fetch(ctx, Item{ID: "art-1"}, true); err == nil {
			t.Error("a work without an address was fetched")
		}
	}
}

func TestPaintingsAsASlideshow(t *testing.T) {
	a, _ := museum(t, false)
	s := New(Options{Source: a, ShowWhole: true, StatePath: filepath.Join(t.TempDir(), "art.json"), CacheDir: t.TempDir(), Log: quietLog})
	s.index(context.Background())
	st := s.Status()
	if st.Count != 2 || st.Source != "Art Institute of Chicago" || st.Motion != "off" || st.Error != "" {
		t.Fatalf("status = %+v", st)
	}
	// The whole painting stays in view, though nothing moves.
	if s.Variant() != "whole" {
		t.Errorf("variant = %q", s.Variant())
	}
	_, slides := s.Slides(10)
	if len(slides) != 2 {
		t.Fatalf("%d slides", len(slides))
	}
	if _, err := s.Image(context.Background(), slides[0].Key); err != nil {
		t.Fatalf("the picture for the TV: %v", err)
	}
	// The museum away: what was listed stays.
	a.API = "http://127.0.0.1:1/api/v1"
	s.index(context.Background())
	if st := s.Status(); st.Count != 2 || st.Error == "" {
		t.Fatalf("with the museum away: %+v", st)
	}
}

func TestSamplePaintings(t *testing.T) {
	s := New(Options{Source: ArtMock{}, ShowWhole: true, StatePath: filepath.Join(t.TempDir(), "art.json"), CacheDir: t.TempDir(), Log: quietLog})
	s.index(context.Background())
	_, slides := s.Slides(10)
	if len(slides) != 6 || slides[0].Items[0].Folder == "" || !strings.HasPrefix(slides[0].Key, "art-") {
		t.Fatalf("slides = %+v", slides)
	}
	if _, err := s.Image(context.Background(), slides[0].Key); err != nil {
		t.Fatal(err)
	}
}
