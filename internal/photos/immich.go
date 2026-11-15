package photos

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Immich reads photos from an Immich server: everything, the favourites,
// an album, or the pictures of a person. It signs in with an API key, which
// is made in Immich under Account Settings, API Keys; reading is all it does.
type Immich struct {
	base string
	key  string
	http *http.Client
}

// ImmichOptions are the server's address and the key to it.
type ImmichOptions struct {
	// URL is the address Immich is opened with in a browser.
	URL    string
	APIKey string
	// InsecureTLS accepts a certificate the Pi cannot check (self-signed).
	InsecureTLS bool
}

// NewImmich returns a source for the server at o.URL.
func NewImmich(o ImmichOptions) *Immich {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.InsecureTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // asked for, for a NAS with its own certificate
	}
	base := strings.TrimRight(strings.TrimSpace(o.URL), "/")
	// The address as copied from the browser may end in the page shown there.
	base = strings.TrimSuffix(base, "/api")
	return &Immich{base: base, key: strings.TrimSpace(o.APIKey), http: &http.Client{Transport: tr, Timeout: 60 * time.Second}}
}

// The folders of an Immich server, as the picker shows them.
const (
	immichAll        = "all"
	immichFavourites = "favourites"
	immichAlbums     = "albums"
	immichPeople     = "people"
	immichAlbum      = "album:"
	immichPerson     = "person:"
)

// Label implements Source.
func (m *Immich) Label() string { return "Immich" }

// HasPreviews implements Source: Immich keeps a smaller copy of every photo.
func (m *Immich) HasPreviews() bool { return true }

// DefaultSelection implements Source: all photos, until something is chosen.
func (m *Immich) DefaultSelection() Selection {
	return Selection{Folders: []Folder{{ID: immichAll, Name: "All photos", Path: "All photos", Leaf: true}}}
}

// do asks the server something and returns its answer.
func (m *Immich) do(ctx context.Context, method, path string, body any, limit int64) ([]byte, string, error) {
	var in io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		in = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.base+path, in)
	if err != nil {
		return nil, "", fmt.Errorf("the address of the Immich server is not one: %w", err)
	}
	req.Header.Set("x-api-key", m.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, "", cleanNetErr(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, "", cleanNetErr(err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, "", errors.New("the Immich server does not accept the API key, or the key may not read this")
	case resp.StatusCode == http.StatusNotFound:
		return nil, "", fmt.Errorf("the Immich server does not know %s: is the address right, and Immich recent enough?", path)
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return nil, "", fmt.Errorf("the Immich server answered HTTP %d", resp.StatusCode)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func (m *Immich) call(ctx context.Context, method, path string, body, out any) error {
	data, _, err := m.do(ctx, method, path, body, 64<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("the answer did not come from an Immich server: is the address right?")
	}
	return nil
}

// Check signs in and says who the key belongs to: for the settings page.
func (m *Immich) Check(ctx context.Context) (string, error) {
	if m.base == "" || m.key == "" {
		return "", errors.New("the address and the API key are both needed")
	}
	if u, err := url.Parse(m.base); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("the address must start with http:// or https://")
	}
	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := m.call(ctx, http.MethodGet, "/api/users/me", nil, &me); err != nil {
		return "", err
	}
	if me.Name != "" {
		return me.Name, nil
	}
	return me.Email, nil
}

// Folders implements Source.
func (m *Immich) Folders(ctx context.Context, parent string) ([]Folder, error) {
	switch parent {
	case "":
		return []Folder{
			{ID: immichAll, Name: "All photos", Path: "All photos", Leaf: true},
			{ID: immichFavourites, Name: "Favourites", Path: "Favourites", Leaf: true},
			{ID: immichAlbums, Name: "Albums", Path: "Albums"},
			{ID: immichPeople, Name: "People", Path: "People"},
		}, nil
	case immichAlbums:
		var albums []struct {
			ID    string `json:"id"`
			Name  string `json:"albumName"`
			Count int    `json:"assetCount"`
		}
		if err := m.call(ctx, http.MethodGet, "/api/albums", nil, &albums); err != nil {
			return nil, err
		}
		out := make([]Folder, 0, len(albums))
		for _, a := range albums {
			if a.ID == "" {
				continue
			}
			name := strings.TrimSpace(a.Name)
			if name == "" {
				name = "Untitled"
			}
			out = append(out, Folder{ID: immichAlbum + a.ID, Name: name, Path: "Albums / " + name, Leaf: true, Detail: strconv.Itoa(a.Count) + " items"})
		}
		sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
		return out, nil
	case immichPeople:
		var people struct {
			People []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Hidden bool   `json:"isHidden"`
			} `json:"people"`
		}
		if err := m.call(ctx, http.MethodGet, "/api/people?size=500", nil, &people); err != nil {
			return nil, err
		}
		var out []Folder
		for _, p := range people.People {
			// Only those who were given a name: the rest are faces.
			if name := strings.TrimSpace(p.Name); name != "" && p.ID != "" && !p.Hidden {
				out = append(out, Folder{ID: immichPerson + p.ID, Name: name, Path: "People / " + name, Leaf: true})
			}
		}
		sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
		return out, nil
	}
	return nil, nil // an album or a person has nothing inside but its photos
}

// immichAsset is a photo as Immich describes it.
type immichAsset struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Name      string `json:"originalFileName"`
	Local     string `json:"localDateTime"`
	Created   string `json:"fileCreatedAt"`
	Checksum  string `json:"checksum"`
	Trashed   bool   `json:"isTrashed"`
	Archived  bool   `json:"isArchived"`
	Visible   string `json:"visibility"`
	Favourite bool   `json:"isFavorite"`
	Exif      *struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
		City      string   `json:"city"`
		Country   string   `json:"country"`
		Size      int64    `json:"fileSizeInByte"`
	} `json:"exifInfo"`
}

// Items implements Source.
func (m *Immich) Items(ctx context.Context, folder Folder) ([]Item, error) {
	ask := map[string]any{"type": "IMAGE", "withExif": true, "size": 1000}
	switch {
	case folder.ID == immichAll:
	case folder.ID == immichFavourites:
		ask["isFavorite"] = true
	case strings.HasPrefix(folder.ID, immichAlbum):
		ask["albumIds"] = []string{strings.TrimPrefix(folder.ID, immichAlbum)}
	case strings.HasPrefix(folder.ID, immichPerson):
		ask["personIds"] = []string{strings.TrimPrefix(folder.ID, immichPerson)}
	default:
		return nil, nil // "Albums" and "People" hold folders, not photos
	}
	var out []Item
	// A thousand at a time; a library of half a million is where it stops.
	for page := 1; page <= 500; page++ {
		ask["page"] = page
		var answer struct {
			Assets struct {
				Items []immichAsset `json:"items"`
				// The next page's number, as text; null after the last.
				Next *string `json:"nextPage"`
			} `json:"assets"`
		}
		if err := m.call(ctx, http.MethodPost, "/api/search/metadata", ask, &answer); err != nil {
			return nil, err
		}
		for _, a := range answer.Assets.Items {
			if a.ID == "" || a.Type != "IMAGE" || a.Trashed || a.Archived || (a.Visible != "" && a.Visible != "timeline") {
				continue
			}
			item := Item{Key: "i-" + a.ID, Name: a.Name, Folder: folder.Path, Version: a.Checksum, ID: a.ID}
			// The time on the camera's clock, which Immich writes as if it were UTC.
			if t, err := time.Parse(time.RFC3339, a.Local); err == nil {
				item.Taken, item.WallClock = t.UTC(), true
			} else if t, err := time.Parse(time.RFC3339, a.Created); err == nil {
				item.Taken = t.UTC()
			}
			if e := a.Exif; e != nil {
				item.Size = e.Size
				if e.Latitude != nil && e.Longitude != nil && !(*e.Latitude == 0 && *e.Longitude == 0) {
					item.Located, item.Latitude, item.Longitude = true, *e.Latitude, *e.Longitude
				}
				var where []string
				for _, part := range []string{e.City, e.Country} {
					if part = strings.TrimSpace(part); part != "" {
						where = append(where, part)
					}
				}
				item.Place = strings.Join(where, ", ")
			}
			out = append(out, item)
		}
		if answer.Assets.Next == nil || *answer.Assets.Next == "" || len(answer.Assets.Items) == 0 {
			break
		}
	}
	return out, nil
}

// Fetch implements Source. The smaller copy Immich keeps is a JPEG or WebP
// that stands upright; the original is used only when it is a kind of
// picture PiDash can read, which a phone's HEIC is not.
func (m *Immich) Fetch(ctx context.Context, item Item, original bool) ([]byte, bool, error) {
	id := url.PathEscape(item.ID)
	if original && decodable(item.Name) {
		data, kind, err := m.do(ctx, http.MethodGet, "/api/assets/"+id+"/original", nil, 100<<20)
		if err == nil && strings.HasPrefix(kind, "image/") {
			return data, false, nil
		}
	}
	data, _, err := m.do(ctx, http.MethodGet, "/api/assets/"+id+"/thumbnail?size=preview", nil, 40<<20)
	return data, true, err
}

// Peek implements Peeker: the small copy tells a photo's shape.
func (m *Immich) Peek(ctx context.Context, item Item) ([]byte, bool, error) {
	data, _, err := m.do(ctx, http.MethodGet, "/api/assets/"+url.PathEscape(item.ID)+"/thumbnail?size=thumbnail", nil, 8<<20)
	return data, true, err
}
