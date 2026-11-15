package photos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A pretend Immich server with a library of 1,203 photos (so that it takes
// more than one page), two albums and three people.
func pretendImmich(t *testing.T) (*Immich, *[]string) {
	t.Helper()
	var asked []string
	asset := func(i int, extra string) string {
		return fmt.Sprintf(`{"id":"a-%d","type":"IMAGE","originalFileName":"IMG_%04d.jpg","localDateTime":"2023-07-14T16:23:11.000Z","fileCreatedAt":"2023-07-14T14:23:11.000Z","checksum":"sum%d","isTrashed":false,"visibility":"timeline"%s}`, i, i, i, extra)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "the-key" {
			http.Error(w, `{"message":"Invalid API key"}`, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		asked = append(asked, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/users/me":
			fmt.Fprint(w, `{"id":"u1","name":"Dario","email":"d@example.org"}`)
		case r.URL.Path == "/api/albums":
			fmt.Fprint(w, `[{"id":"al-2","albumName":"Lisbon","assetCount":40},{"id":"al-1","albumName":"Iceland","assetCount":212},{"id":"al-3","albumName":"","assetCount":1}]`)
		case r.URL.Path == "/api/people":
			fmt.Fprint(w, `{"people":[{"id":"p-1","name":"Anna","isHidden":false},{"id":"p-2","name":"","isHidden":false},{"id":"p-3","name":"Ben","isHidden":true}],"total":3}`)
		case r.URL.Path == "/api/search/metadata":
			var ask struct {
				Page      int      `json:"page"`
				Size      int      `json:"size"`
				Type      string   `json:"type"`
				WithExif  bool     `json:"withExif"`
				Favourite bool     `json:"isFavorite"`
				Albums    []string `json:"albumIds"`
				People    []string `json:"personIds"`
			}
			if json.Unmarshal(body, &ask) != nil || ask.Type != "IMAGE" || !ask.WithExif || ask.Size != 1000 {
				t.Errorf("asked for photos with %s", body)
			}
			var items []string
			next := "null"
			switch {
			case len(ask.Albums) == 1 && ask.Albums[0] == "al-2":
				items = []string{
					asset(1, `,"exifInfo":{"latitude":38.7223,"longitude":-9.1393,"city":"Lisbon","country":"Portugal","fileSizeInByte":3200000}`),
					asset(2, `,"exifInfo":{"latitude":null,"longitude":null,"city":null,"country":null}`),
					// Not for the slideshow: in the bin, put away, a film, a phone's own format kept hidden.
					strings.Replace(asset(3, ""), `"isTrashed":false`, `"isTrashed":true`, 1),
					strings.Replace(asset(4, ""), `"visibility":"timeline"`, `"visibility":"archive"`, 1),
					strings.Replace(asset(5, ""), `"type":"IMAGE"`, `"type":"VIDEO"`, 1),
				}
			case ask.Favourite:
				items = []string{asset(7, "")}
			case len(ask.People) == 1:
				items = []string{asset(8, ""), asset(9, "")}
			default:
				// Everything: two pages.
				from, to := 0, 1000
				if ask.Page == 2 {
					from, to = 1000, 1203
				} else {
					next = `"2"`
				}
				for i := from; i < to; i++ {
					items = append(items, asset(i, ""))
				}
			}
			fmt.Fprintf(w, `{"albums":{"items":[]},"assets":{"total":%d,"count":%d,"items":[%s],"nextPage":%s}}`, len(items), len(items), strings.Join(items, ","), next)
		case strings.HasSuffix(r.URL.Path, "/original"):
			w.Header().Set("Content-Type", "image/jpeg")
			fmt.Fprint(w, "the original of "+r.URL.Path)
		case strings.HasSuffix(r.URL.Path, "/thumbnail"):
			w.Header().Set("Content-Type", "image/jpeg")
			fmt.Fprint(w, "the "+r.URL.Query().Get("size")+" of "+r.URL.Path)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	// The address as someone copies it from a browser.
	return NewImmich(ImmichOptions{URL: srv.URL + "/", APIKey: " the-key "}), &asked
}

func TestImmichFoldersAlbumsAndPeople(t *testing.T) {
	m, _ := pretendImmich(t)
	ctx := context.Background()
	if who, err := m.Check(ctx); err != nil || who != "Dario" {
		t.Fatalf("signing in: %q %v", who, err)
	}
	top, err := m.Folders(ctx, "")
	if err != nil || len(top) != 4 || top[0].ID != "all" || !top[0].Leaf || top[2].ID != "albums" || top[2].Leaf {
		t.Fatalf("top level = %+v, %v", top, err)
	}
	albums, err := m.Folders(ctx, "albums")
	if err != nil || len(albums) != 3 {
		t.Fatalf("albums = %+v, %v", albums, err)
	}
	// By name; one without a name is still offered.
	if albums[0].Name != "Iceland" || albums[0].ID != "album:al-1" || albums[0].Detail != "212 items" || albums[0].Path != "Albums / Iceland" || !albums[0].Leaf || albums[2].Name != "Untitled" {
		t.Errorf("albums = %+v", albums)
	}
	// People who were given a name, and are not hidden.
	people, err := m.Folders(ctx, "people")
	if err != nil || len(people) != 1 || people[0].Name != "Anna" || people[0].ID != "person:p-1" {
		t.Errorf("people = %+v, %v", people, err)
	}
	if def := m.DefaultSelection(); len(def.Folders) != 1 || def.Folders[0].ID != "all" {
		t.Errorf("before anything is chosen: %+v", def)
	}

	// A wrong key is said in words.
	wrong := NewImmich(ImmichOptions{URL: m.base, APIKey: "nope"})
	if _, err := wrong.Check(ctx); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("a wrong key: %v", err)
	}
	// And so is an address that is not Immich's.
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "<html>a router</html>") }))
	defer other.Close()
	if _, err := NewImmich(ImmichOptions{URL: other.URL, APIKey: "k"}).Check(ctx); err == nil || !strings.Contains(err.Error(), "not come from an Immich server") {
		t.Errorf("another server: %v", err)
	}
	if _, err := NewImmich(ImmichOptions{URL: "nas.local:2283", APIKey: "k"}).Check(ctx); err == nil || !strings.Contains(err.Error(), "http://") {
		t.Errorf("an address without http: %v", err)
	}
}

func TestImmichPhotos(t *testing.T) {
	m, asked := pretendImmich(t)
	ctx := context.Background()

	lisbon, err := m.Items(ctx, Folder{ID: "album:al-2", Path: "Albums / Lisbon"})
	if err != nil || len(lisbon) != 2 {
		t.Fatalf("the album's photos = %+v, %v", lisbon, err)
	}
	a := lisbon[0]
	want := time.Date(2023, 7, 14, 16, 23, 11, 0, time.UTC)
	if a.Key != "i-a-1" || a.ID != "a-1" || a.Name != "IMG_0001.jpg" || a.Folder != "Albums / Lisbon" || !a.Taken.Equal(want) || !a.WallClock ||
		a.Place != "Lisbon, Portugal" || !a.Located || a.Latitude != 38.7223 || a.Longitude != -9.1393 || a.Size != 3200000 || a.Version != "sum1" || a.Video {
		t.Errorf("a photo with a place = %+v", a)
	}
	if b := lisbon[1]; b.Located || b.Place != "" {
		t.Errorf("a photo without a place = %+v", b)
	}

	// Everything comes a thousand at a time.
	*asked = nil
	all, err := m.Items(ctx, Folder{ID: "all", Path: "All photos"})
	if err != nil || len(all) != 1203 || len(*asked) != 2 || all[1202].ID != "a-1202" {
		t.Fatalf("all photos: %d, %v, asked %d times", len(all), err, len(*asked))
	}
	if fav, err := m.Items(ctx, Folder{ID: "favourites"}); err != nil || len(fav) != 1 {
		t.Errorf("favourites = %+v, %v", fav, err)
	}
	if anna, err := m.Items(ctx, Folder{ID: "person:p-1"}); err != nil || len(anna) != 2 || !strings.Contains((*asked)[len(*asked)-1], `"personIds":["p-1"]`) {
		t.Errorf("a person's photos = %+v, %v", anna, err)
	}
	// "Albums" holds albums, not photos.
	if none, err := m.Items(ctx, Folder{ID: "albums"}); err != nil || len(none) != 0 {
		t.Errorf("the albums folder itself: %+v, %v", none, err)
	}

	// The smaller copy stands upright; the original is as the camera wrote it.
	data, upright, err := m.Fetch(ctx, a, false)
	if err != nil || string(data) != "the preview of /api/assets/a-1/thumbnail" || !upright {
		t.Errorf("the smaller copy: %q %v %v", data, upright, err)
	}
	data, upright, err = m.Fetch(ctx, a, true)
	if err != nil || string(data) != "the original of /api/assets/a-1/original" || upright {
		t.Errorf("the original: %q %v %v", data, upright, err)
	}
	// A phone's HEIC cannot be read here: its smaller copy can.
	heic := a
	heic.Name = "IMG_0001.HEIC"
	if data, upright, err = m.Fetch(ctx, heic, true); err != nil || !strings.HasPrefix(string(data), "the preview") || !upright {
		t.Errorf("an original that cannot be read: %q %v %v", data, upright, err)
	}
	if data, _, err = m.Peek(ctx, a); err != nil || !strings.HasPrefix(string(data), "the thumbnail") {
		t.Errorf("a look at its shape: %q %v", data, err)
	}
}
