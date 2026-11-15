package photos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Art is paintings instead of photos: works in the public domain from the
// Art Institute of Chicago, which publishes its collection, with pictures,
// for anyone to use. Each work is an Item whose Name is its title, whose
// Folder is the artist and whose Place is when it was made.
type Art struct {
	// API is where the museum is asked; Agent names this program to it.
	API   string
	Agent string
	HTTP  *http.Client
	// Most is how many works are taken into the slideshow.
	Most int
}

// ArtAPI is the collection of the Art Institute of Chicago.
const ArtAPI = "https://api.artic.edu/api/v1"

// artImages is where its pictures are, unless the museum's answer says otherwise.
const artImages = "https://www.artic.edu/iiif/2"

// NewArt returns the museum as a source of pictures.
func NewArt(agent string) *Art {
	return &Art{API: ArtAPI, Agent: agent, HTTP: &http.Client{Timeout: 40 * time.Second}, Most: 400}
}

var artFolder = Folder{ID: "paintings", Name: "Paintings", Path: "Paintings", Leaf: true}

// Label implements Source.
func (a *Art) Label() string { return "Art Institute of Chicago" }

// HasPreviews implements Source.
func (a *Art) HasPreviews() bool { return false }

// DefaultSelection implements Source: the paintings, without anything to choose.
func (a *Art) DefaultSelection() Selection { return Selection{Folders: []Folder{artFolder}} }

// Folders implements Source.
func (a *Art) Folders(_ context.Context, parent string) ([]Folder, error) {
	if parent == "" {
		return []Folder{artFolder}, nil
	}
	return nil, nil
}

func (a *Art) get(ctx context.Context, address string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, 0, err
	}
	// The museum asks programs to say who they are, in this header.
	req.Header.Set("AIC-User-Agent", a.Agent)
	req.Header.Set("User-Agent", a.Agent)
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, 0, errors.New("the museum could not be reached")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	return data, resp.StatusCode, err
}

type artPage struct {
	Pagination struct {
		TotalPages int `json:"total_pages"`
	} `json:"pagination"`
	Data []struct {
		ID      int64  `json:"id"`
		Title   string `json:"title"`
		Artist  string `json:"artist_title"`
		Date    string `json:"date_display"`
		ImageID string `json:"image_id"`
		Kind    string `json:"artwork_type_title"`
		Public  *bool  `json:"is_public_domain"`
	} `json:"data"`
	Config struct {
		IIIF string `json:"iiif_url"`
	} `json:"config"`
}

// Items implements Source: the museum's paintings that are free to show.
func (a *Art) Items(ctx context.Context, _ Folder) ([]Item, error) {
	const perPage = 100
	fields := "id,title,artist_title,date_display,image_id,artwork_type_title,is_public_domain"
	// Asked for precisely: paintings, in the public domain, with a picture.
	exact := url.Values{
		"query[bool][must][0][term][is_public_domain]":    {"true"},
		"query[bool][must][1][match][artwork_type_title]": {"Painting"},
		"query[bool][must][2][exists][field]":             {"image_id"},
		"fields":                                          {fields},
		"limit":                                           {strconv.Itoa(perPage)},
	}
	// And less precisely, in the form the museum's own examples use, should
	// it not take the first: what is no painting is then left out here.
	plain := url.Values{"query[term][is_public_domain]": {"true"}, "fields": {fields}, "limit": {strconv.Itoa(perPage)}}

	var out []Item
	seen := map[int64]bool{}
	query := exact
	var firstErr error
	// The museum hands out the first thousand results of a search.
	for page := 1; page <= 10 && len(out) < a.Most; page++ {
		query.Set("page", strconv.Itoa(page))
		data, code, err := a.get(ctx, a.API+"/artworks/search?"+query.Encode(), 8<<20)
		var got artPage
		if err == nil && code != http.StatusOK {
			err = fmt.Errorf("the museum answered HTTP %d", code)
		}
		if err == nil && json.Unmarshal(data, &got) != nil {
			err = errors.New("the museum gave an answer that could not be read")
		}
		if err != nil {
			if page == 1 && firstErr == nil && ctx.Err() == nil {
				// Once more, the plain way.
				firstErr, query, page = err, plain, 0
				continue
			}
			if len(out) > 0 {
				break // what was read is enough to show
			}
			return nil, err
		}
		images := strings.TrimRight(got.Config.IIIF, "/")
		// Pictures are fetched from where the museum says, over an encrypted
		// connection unless the museum itself was asked without one.
		plain := strings.HasPrefix(a.API, "http://")
		if u, perr := url.Parse(images); perr != nil || u.Host == "" || (u.Scheme != "https" && !(plain && u.Scheme == "http")) {
			images = artImages
		}
		for _, w := range got.Data {
			if w.ImageID == "" || w.ID == 0 || seen[w.ID] || strings.ContainsAny(w.ImageID, "/?#") {
				continue
			}
			if (w.Kind != "" && w.Kind != "Painting") || (w.Public != nil && !*w.Public) {
				continue
			}
			seen[w.ID] = true
			artist := strings.TrimSpace(w.Artist)
			if artist == "" {
				artist = "Artist unknown"
			}
			version := w.ImageID
			if len(version) > 8 {
				version = version[:8]
			}
			out = append(out, Item{
				Key: "art-" + strconv.FormatInt(w.ID, 10), Name: strings.TrimSpace(w.Title), Folder: artist, Place: strings.TrimSpace(w.Date),
				Version: version, ID: images + "/" + w.ImageID,
			})
			if len(out) >= a.Most {
				break
			}
		}
		if len(got.Data) < perPage || (got.Pagination.TotalPages > 0 && page >= got.Pagination.TotalPages) {
			break
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the museum listed no paintings")
	}
	return out, nil
}

// Fetch implements Source: the picture, at a size that fills a TV.
func (a *Art) Fetch(ctx context.Context, item Item, _ bool) ([]byte, bool, error) {
	if !strings.HasPrefix(item.ID, "http") {
		return nil, false, ErrNotFound
	}
	var last error
	// The larger size first; the museum does not keep it for every work.
	for _, width := range []string{"1686", "843"} {
		data, code, err := a.get(ctx, item.ID+"/full/"+width+",/0/default.jpg", 40<<20)
		if err == nil && code == http.StatusOK && len(data) > 0 {
			return data, true, nil
		}
		if err == nil {
			err = fmt.Errorf("the museum answered HTTP %d for a picture", code)
		}
		last = err
	}
	return nil, false, last
}

// ArtMock stands in for the museum during development: a handful of
// invented works whose pictures are generated.
type ArtMock struct{ Mock }

// Label implements Source.
func (ArtMock) Label() string { return "Sample paintings" }

// DefaultSelection implements Source.
func (ArtMock) DefaultSelection() Selection { return Selection{Folders: []Folder{artFolder}} }

// Folders implements Source.
func (ArtMock) Folders(_ context.Context, parent string) ([]Folder, error) {
	if parent == "" {
		return []Folder{artFolder}, nil
	}
	return nil, nil
}

// Items implements Source.
func (ArtMock) Items(context.Context, Folder) ([]Item, error) {
	works := [][3]string{
		{"Morning on the Lake", "Ada Brandt", "1887"}, {"Still Life with Lemons", "Pieter Vos", "c. 1650"},
		{"The Orchard", "Camille Aubert", "1901"}, {"Harbour at Dusk", "Johan Lind", "1874"},
		{"Portrait of a Reader", "Maria Conti", "1795"}, {"Winter Road", "Ivan Sorokin", "1912"},
	}
	var out []Item
	for i, w := range works {
		key := fmt.Sprintf("art-%d", 1000+i)
		out = append(out, Item{Key: key, Name: w[0], Folder: w[1], Place: w[2], Version: "1", ID: key})
	}
	return out, nil
}
