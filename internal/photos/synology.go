package photos

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Synology reads photos from Synology Photos (DSM 7) through its web API.
//
// Synology Photos has two libraries: each user's Personal Space and the common
// Shared Space. Both appear as top-level folders. Folder ids used here are
// "p:<id>" and "s:<id>"; "p:" and "s:" alone are the libraries' roots.
//
// Albums appear as a third top-level entry, "a:", with one entry "a:<id>" per
// album: the account's own albums and the ones other people shared with it.
type Synology struct {
	base       string
	user, pass string
	videos     bool
	hc         *http.Client

	mu       sync.Mutex
	sid      string
	roots    map[string]int // space -> root folder id
	albums   []album        // the last album list, kept briefly
	albumsAt time.Time

	// noAddress is set once the NAS has refused to list place names.
	noAddress atomic.Bool
	// noGPS is set once it has refused to list positions.
	noGPS atomic.Bool
}

// SynologyOptions configure the connection to the NAS.
type SynologyOptions struct {
	// URL is the address of DSM, e.g. "http://192.168.1.20:5000".
	URL      string
	Username string
	Password string
	// InsecureTLS accepts a self-signed HTTPS certificate.
	InsecureTLS bool
	// Videos lists video clips along with the photos.
	Videos bool
}

// NewSynology returns a Synology Photos source.
func NewSynology(o SynologyOptions) *Synology {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.InsecureTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit opt-in for a NAS on the home network
	}
	return &Synology{
		videos: o.Videos,
		base:   strings.TrimRight(o.URL, "/"),
		user:   o.Username,
		pass:   o.Password,
		hc:     &http.Client{Timeout: 90 * time.Second, Transport: tr},
		roots:  map[string]int{},
	}
}

const (
	spacePersonal = "p"
	spaceShared   = "s"
	// spaceAlbum is not a library: an album collects photos from either one.
	spaceAlbum = "a"
)

var spaceLabel = map[string]string{spacePersonal: "Personal Space", spaceShared: "Shared Space", spaceAlbum: "Albums"}

// apiName returns the API for a library: SYNO.Foto.* is the Personal Space,
// SYNO.FotoTeam.* the Shared Space.
func apiName(space, suffix string) string {
	if space == spaceShared {
		return "SYNO.FotoTeam." + suffix
	}
	return "SYNO.Foto." + suffix
}

// Label implements Source.
func (s *Synology) Label() string { return "Synology Photos" }

// HasPreviews implements Source: Synology keeps ready-made previews of every photo.
func (s *Synology) HasPreviews() bool { return true }

// DefaultSelection implements Source: nothing is shown until folders are picked.
func (s *Synology) DefaultSelection() Selection { return Selection{Recursive: true} }

type synoError struct {
	Code int
	API  string
}

func (e *synoError) Error() string {
	switch e.Code {
	case 105:
		return "the NAS account is not allowed to do this"
	case 106, 107, 119:
		return "the NAS session expired"
	}
	return fmt.Sprintf("the NAS returned error %d for %s", e.Code, e.API)
}

func loginError(code int) error {
	switch code {
	case 400:
		return errors.New("NAS login failed: wrong username or password")
	case 401:
		return errors.New("NAS login failed: the account is disabled")
	case 402:
		return errors.New("NAS login failed: the account has no permission to sign in")
	case 403, 404, 406:
		return errors.New("NAS login failed: the account uses 2-step verification, which PiDash cannot answer; use a separate NAS account without it")
	case 407:
		return errors.New("NAS login failed: this device was blocked after too many failed attempts")
	}
	return fmt.Errorf("NAS login failed (error %d)", code)
}

type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   struct {
		Code int `json:"code"`
	} `json:"error"`
}

func (s *Synology) endpoint() string { return s.base + "/webapi/entry.cgi" }

func (s *Synology) login(ctx context.Context) (string, error) {
	form := url.Values{
		"api": {"SYNO.API.Auth"}, "version": {"6"}, "method": {"login"},
		"account": {s.user}, "passwd": {s.pass}, "format": {"sid"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot reach the NAS at %s: %w", s.base, cleanNetErr(err))
	}
	defer resp.Body.Close()
	var env envelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&env); err != nil {
		return "", fmt.Errorf("%s does not look like a Synology NAS (unexpected login response, HTTP %d)", s.base, resp.StatusCode)
	}
	if !env.Success {
		return "", loginError(env.Error.Code)
	}
	var data struct {
		SID string `json:"sid"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil || data.SID == "" {
		return "", errors.New("NAS login returned no session")
	}
	return data.SID, nil
}

// cleanNetErr drops the URL (which would repeat the address) from a net error.
func cleanNetErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// session returns a valid session id, signing in if needed.
func (s *Synology) session(ctx context.Context, renew bool) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sid != "" && !renew {
		return s.sid, nil
	}
	sid, err := s.login(ctx)
	if err != nil {
		s.sid = ""
		return "", err
	}
	s.sid = sid
	return sid, nil
}

func isSessionError(err error) bool {
	var se *synoError
	return errors.As(err, &se) && (se.Code == 106 || se.Code == 107 || se.Code == 119)
}

// do performs an API request, signing in again once if the session expired.
// It returns the raw response for binary endpoints.
func (s *Synology) do(ctx context.Context, params url.Values, maxBytes int64) (body []byte, contentType string, err error) {
	for attempt := 0; ; attempt++ {
		sid, err := s.session(ctx, attempt > 0)
		if err != nil {
			return nil, "", err
		}
		form := url.Values{}
		for k, v := range params {
			form[k] = v
		}
		form.Set("_sid", sid)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(), strings.NewReader(form.Encode()))
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := s.hc.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("cannot reach the NAS: %w", cleanNetErr(err))
		}
		body, err = io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		resp.Body.Close()
		if err != nil {
			return nil, "", err
		}
		if int64(len(body)) > maxBytes {
			return nil, "", errTooLarge
		}
		contentType = resp.Header.Get("Content-Type")

		// Errors always come back as a small JSON document, even from the
		// endpoints that normally return a file.
		if strings.Contains(contentType, "json") || (len(body) > 0 && body[0] == '{' && len(body) < 4096) {
			var env envelope
			if json.Unmarshal(body, &env) == nil && !env.Success {
				apiErr := &synoError{Code: env.Error.Code, API: params.Get("api")}
				if isSessionError(apiErr) && attempt == 0 {
					continue
				}
				return nil, "", apiErr
			}
		}
		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("the NAS answered HTTP %d for %s", resp.StatusCode, params.Get("api"))
		}
		return body, contentType, nil
	}
}

// call performs a JSON API request and decodes its data into out.
func (s *Synology) call(ctx context.Context, params url.Values, out any) error {
	body, _, err := s.do(ctx, params, 32<<20)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("unexpected answer from the NAS for %s", params.Get("api"))
	}
	if !env.Success {
		return &synoError{Code: env.Error.Code, API: params.Get("api")}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func splitID(id string) (space string, n int, isRoot bool, err error) {
	space, rest, ok := strings.Cut(id, ":")
	if !ok || (space != spacePersonal && space != spaceShared && space != spaceAlbum) {
		return "", 0, false, fmt.Errorf("bad folder id %q", id)
	}
	if rest == "" {
		return space, 0, true, nil
	}
	n, err = strconv.Atoi(rest)
	if err != nil {
		return "", 0, false, fmt.Errorf("bad folder id %q", id)
	}
	return space, n, false, nil
}

// rootID returns the id of a library's top folder.
func (s *Synology) rootID(ctx context.Context, space string) (int, error) {
	s.mu.Lock()
	id, ok := s.roots[space]
	s.mu.Unlock()
	if ok {
		return id, nil
	}
	var data struct {
		Folder struct {
			ID int `json:"id"`
		} `json:"folder"`
	}
	err := s.call(ctx, url.Values{"api": {apiName(space, "Browse.Folder")}, "version": {"1"}, "method": {"get"}}, &data)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.roots[space] = data.Folder.ID
	s.mu.Unlock()
	return data.Folder.ID, nil
}

// Folders implements Source.
func (s *Synology) Folders(ctx context.Context, parent string) ([]Folder, error) {
	if parent == "" {
		// The two libraries. The Shared Space is optional on the NAS, so only
		// list the ones that answer; if neither does, report why.
		var out []Folder
		var firstErr error
		for _, space := range []string{spacePersonal, spaceShared} {
			if _, err := s.rootID(ctx, space); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			out = append(out, Folder{ID: space + ":", Name: spaceLabel[space], Path: spaceLabel[space]})
		}
		if len(out) == 0 {
			// An account made only to receive a shared album may have no
			// library of its own; it is still usable if its albums can be read.
			if _, err := s.listAlbums(ctx, 0); err != nil {
				return nil, firstErr
			}
		}
		out = append(out, Folder{ID: spaceAlbum + ":", Name: spaceLabel[spaceAlbum], Path: spaceLabel[spaceAlbum]})
		return out, nil
	}

	space, id, isRoot, err := splitID(parent)
	if err != nil {
		return nil, err
	}
	if space == spaceAlbum {
		if !isRoot {
			return nil, nil // an album has nothing inside but photos
		}
		return s.albumFolders(ctx)
	}
	if isRoot {
		if id, err = s.rootID(ctx, space); err != nil {
			return nil, err
		}
	}
	var out []Folder
	const page = 500
	for offset := 0; offset < maxFolders; offset += page {
		var data struct {
			List []struct {
				ID   int    `json:"id"`
				Name string `json:"name"` // full path inside the library, e.g. "/2024/Holiday"
			} `json:"list"`
		}
		err := s.call(ctx, url.Values{
			"api": {apiName(space, "Browse.Folder")}, "version": {"1"}, "method": {"list"},
			"id": {strconv.Itoa(id)}, "offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(page)},
			"sort_by": {"filename"}, "sort_direction": {"asc"},
		}, &data)
		if err != nil {
			return out, err
		}
		for _, f := range data.List {
			full := "/" + strings.Trim(f.Name, "/")
			out = append(out, Folder{
				ID:   space + ":" + strconv.Itoa(f.ID),
				Name: path.Base(full),
				Path: spaceLabel[space] + strings.ReplaceAll(full, "/", " / "),
			})
		}
		if len(data.List) < page {
			break
		}
	}
	return out, nil
}

// Items implements Source.
func (s *Synology) Items(ctx context.Context, folder Folder) ([]Item, error) {
	space, id, isRoot, err := splitID(folder.ID)
	if err != nil {
		return nil, err
	}
	if space == spaceAlbum {
		if isRoot {
			return nil, nil // the list of albums itself holds no photos
		}
		return s.albumItems(ctx, id, folder)
	}
	if isRoot {
		if id, err = s.rootID(ctx, space); err != nil {
			return nil, err
		}
	}
	return s.listItems(ctx, apiName(space, "Browse.Item"), url.Values{"folder_id": {strconv.Itoa(id)}}, folder.Path,
		func(rawItem) (string, string) { return space, "" })
}

// rawItem is a photo as the NAS lists it.
type rawItem struct {
	ID       int    `json:"id"`
	Filename string `json:"filename"`
	Time     int64  `json:"time"`
	Type     string `json:"type"`
	Filesize int64  `json:"filesize"`
	// OwnerUserID is 0 for photos in the Shared Space.
	OwnerUserID *int `json:"owner_user_id"`
	Additional  struct {
		Thumbnail struct {
			CacheKey string `json:"cache_key"`
			UnitID   int    `json:"unit_id"`
		} `json:"thumbnail"`
		// Address is where the photo was taken, when it carries a position;
		// GPS the position itself.
		Address map[string]any `json:"address"`
		GPS     *struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"gps"`
	} `json:"additional"`
}

// placeName turns the NAS's address of a photo into "Town, Country".
func placeName(address map[string]any) string {
	field := func(names ...string) string {
		for _, n := range names {
			if v, ok := address[n].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	town, country := field("city", "town", "village", "county", "state"), field("country")
	switch {
	case town != "" && country != "":
		return town + ", " + country
	case town != "":
		return town
	}
	return country
}

// listItems reads every page of a photo list. where selects the list (a folder
// or an album); place tells, for each photo, which library's API serves it and
// what must follow the unit id in Item.ID.
func (s *Synology) listItems(ctx context.Context, api string, where url.Values, label string, place func(rawItem) (space, suffix string)) ([]Item, error) {
	var out []Item
	const page = 500
	for offset := 0; offset < maxItems; offset += page {
		var data struct {
			List []rawItem `json:"list"`
		}
		list := func(additional string) error {
			params := url.Values{
				"api": {api}, "version": {"1"}, "method": {"list"},
				"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(page)},
				"additional": {additional},
			}
			for k, v := range where {
				params[k] = v
			}
			data.List = nil
			return s.call(ctx, params, &data)
		}
		// Places are a bonus: a NAS that does not offer positions, or place
		// names, for this list is asked without, from then on.
		err := errors.New("without places")
		for _, try := range []struct {
			additional string
			gave       *atomic.Bool
		}{{`["thumbnail","address","gps"]`, &s.noGPS}, {`["thumbnail","address"]`, &s.noAddress}} {
			if try.gave.Load() {
				continue
			}
			if err = list(try.additional); err == nil {
				break
			}
			var apiErr *synoError
			if !errors.As(err, &apiErr) || isSessionError(err) {
				return out, err
			}
			try.gave.Store(true)
		}
		if err != nil {
			if err = list(`["thumbnail"]`); err != nil {
				return out, err
			}
		}
		for _, it := range data.List {
			// Photos and "live" photos have a still image. Videos are listed
			// only when they are wanted.
			isVideo := it.Type == "video"
			if it.Type != "photo" && it.Type != "live" && !(isVideo && s.videos) {
				continue
			}
			unit := it.Additional.Thumbnail.UnitID
			if unit == 0 {
				unit = it.ID
			}
			name := it.Filename
			if it.Type == "live" {
				name += ".live" // the full file may be a video container: always use the preview
			}
			space, suffix := place(it)
			var lat, lon float64
			if g := it.Additional.GPS; g != nil && !(g.Latitude == 0 && g.Longitude == 0) {
				lat, lon = g.Latitude, g.Longitude
			}
			out = append(out, Item{
				Located: lat != 0 || lon != 0, Latitude: lat, Longitude: lon,
				// The NAS keeps the time on the camera's clock, as if it were UTC.
				WallClock: true,
				Key:       space + "-" + strconv.Itoa(it.ID),
				Name:      name,
				Folder:    label,
				Taken:     time.Unix(it.Time, 0).UTC(),
				Place:     placeName(it.Additional.Address),
				Video:     isVideo,
				Size:      it.Filesize,
				Version:   it.Additional.Thumbnail.CacheKey,
				ID:        space + ":" + strconv.Itoa(unit) + suffix,
			})
		}
		if len(data.List) < page {
			break
		}
	}
	return out, nil
}

// album is an album as the NAS lists it.
type album struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ItemCount int    `json:"item_count"`
	// Passphrase is set on albums that are shared. Photos of an album someone
	// else shared with this account can only be read by quoting it.
	Passphrase string `json:"passphrase"`
}

func (s *Synology) albumPages(ctx context.Context, api, method, category string) ([]album, error) {
	var out []album
	const page = 500
	for offset := 0; offset < maxFolders; offset += page {
		var data struct {
			List []album `json:"list"`
		}
		params := url.Values{
			"api": {api}, "version": {"1"}, "method": {method},
			"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(page)},
		}
		if category != "" {
			params.Set("category", category)
		}
		if err := s.call(ctx, params, &data); err != nil {
			return nil, err
		}
		out = append(out, data.List...)
		if len(data.List) < page {
			break
		}
	}
	return out, nil
}

// listAlbums returns the account's own albums and the albums shared with it.
// A list younger than maxAge is reused.
func (s *Synology) listAlbums(ctx context.Context, maxAge time.Duration) ([]album, error) {
	s.mu.Lock()
	cached, at := s.albums, s.albumsAt
	s.mu.Unlock()
	if maxAge > 0 && !at.IsZero() && time.Since(at) < maxAge {
		return cached, nil
	}

	// One request covers both kinds on current versions of Synology Photos.
	list, err := s.albumPages(ctx, "SYNO.Foto.Browse.Album", "list", "normal_share_with_me")
	var apiErr *synoError
	if err != nil && errors.As(err, &apiErr) && !isSessionError(err) {
		// Otherwise ask for the two kinds separately. Shared albums are a
		// bonus here: failing to list them does not hide the account's own.
		var own, shared []album
		if own, err = s.albumPages(ctx, "SYNO.Foto.Browse.Album", "list", ""); err == nil {
			shared, _ = s.albumPages(ctx, "SYNO.Foto.Sharing.Misc", "list_shared_with_me_album", "")
			list = append(own, shared...)
		}
	}
	if err != nil {
		return nil, err
	}

	seen := map[int]bool{}
	unique := list[:0]
	for _, a := range list {
		if !seen[a.ID] {
			seen[a.ID] = true
			unique = append(unique, a)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool { return strings.ToLower(unique[i].Name) < strings.ToLower(unique[j].Name) })

	s.mu.Lock()
	s.albums, s.albumsAt = unique, time.Now()
	s.mu.Unlock()
	return unique, nil
}

func (s *Synology) albumFolders(ctx context.Context) ([]Folder, error) {
	albums, err := s.listAlbums(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Folder, 0, len(albums))
	for _, a := range albums {
		detail := strconv.Itoa(a.ItemCount) + " items"
		if a.ItemCount == 1 {
			detail = "1 item"
		}
		out = append(out, Folder{
			ID:     spaceAlbum + ":" + strconv.Itoa(a.ID),
			Name:   a.Name,
			Path:   spaceLabel[spaceAlbum] + " / " + a.Name,
			Leaf:   true,
			Detail: detail,
		})
	}
	return out, nil
}

// albumItems lists the photos of one album. The album is looked up by its id
// each time, because its passphrase changes when sharing is switched off and on.
func (s *Synology) albumItems(ctx context.Context, id int, folder Folder) ([]Item, error) {
	albums, err := s.listAlbums(ctx, time.Minute)
	if err != nil {
		return nil, err
	}
	var found *album
	for i := range albums {
		if albums[i].ID == id {
			found = &albums[i]
			break
		}
	}
	if found == nil {
		return nil, errors.New("the album no longer exists, or is no longer shared with this NAS account")
	}

	where := url.Values{"album_id": {strconv.Itoa(id)}}
	if found.Passphrase != "" {
		where = url.Values{"passphrase": {strconv.Quote(found.Passphrase)}}
	}
	suffix := ":" + found.Passphrase // marks the photo as coming from an album
	return s.listItems(ctx, "SYNO.Foto.Browse.Item", where, folder.Path, func(it rawItem) (string, string) {
		// An album mixes photos from both libraries.
		if it.OwnerUserID != nil && *it.OwnerUserID == 0 {
			return spaceShared, suffix
		}
		return spacePersonal, suffix
	})
}

// Fetch implements Source. The preview is Synology's largest ready-made
// thumbnail (1280 pixels on the long side), already rotated upright.
func (s *Synology) Fetch(ctx context.Context, item Item, original bool) ([]byte, bool, error) {
	if original {
		return s.get(ctx, item, sizeOriginal)
	}
	return s.get(ctx, item, sizePreview)
}

// Peek implements Peeker with the smallest preview, a few kilobytes.
func (s *Synology) Peek(ctx context.Context, item Item) ([]byte, bool, error) {
	return s.get(ctx, item, sizeSmall)
}

func (s *Synology) get(ctx context.Context, item Item, size string) ([]byte, bool, error) {
	// Item.ID is "<library>:<unit>", followed for a photo found in an album
	// by ":<the album's passphrase, if it has one>".
	parts := strings.SplitN(item.ID, ":", 3)
	if len(parts) < 2 || (parts[0] != spacePersonal && parts[0] != spaceShared) {
		return nil, false, fmt.Errorf("bad photo id %q", item.Key)
	}
	unit, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, false, fmt.Errorf("bad photo id %q", item.Key)
	}
	space, fromAlbum, passphrase := parts[0], len(parts) == 3, ""
	if fromAlbum {
		passphrase = parts[2]
	}

	body, err := s.fetch(ctx, space, unit, item.Version, passphrase, size)
	var apiErr *synoError
	if err != nil && fromAlbum && errors.As(err, &apiErr) {
		// Which library a photo in an album belongs to is read from its owner,
		// which not every version reports; the other library is worth one try.
		other := spacePersonal
		if space == spacePersonal {
			other = spaceShared
		}
		if alt, altErr := s.fetch(ctx, other, unit, item.Version, passphrase, size); altErr == nil {
			body, err = alt, nil
		}
	}
	if err != nil {
		return nil, false, err
	}
	return body, size != sizeOriginal, nil
}

// What to fetch of a photo: the full file, or one of the previews the NAS
// keeps (1280 and 240 pixels on the long side), which are already upright.
const (
	sizeOriginal = ""
	sizePreview  = "xl"
	sizeSmall    = "sm"
)

func (s *Synology) fetch(ctx context.Context, space string, unit int, cacheKey, passphrase, size string) ([]byte, error) {
	var params url.Values
	var limit int64
	if size == sizeOriginal {
		params = url.Values{
			"api": {apiName(space, "Download")}, "version": {"1"}, "method": {"download"},
			"unit_id": {"[" + strconv.Itoa(unit) + "]"},
		}
		limit = 100 << 20
	} else {
		params = url.Values{
			"api": {apiName(space, "Thumbnail")}, "version": {"1"}, "method": {"get"},
			"mode": {"download"}, "type": {"unit"}, "size": {size},
			"id": {strconv.Itoa(unit)}, "cache_key": {cacheKey},
		}
		limit = 32 << 20
	}
	if passphrase != "" {
		params.Set("passphrase", strconv.Quote(passphrase))
	}
	body, contentType, err := s.do(ctx, params, limit)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || strings.Contains(contentType, "json") || strings.Contains(contentType, "html") {
		return nil, errors.New("the NAS did not return an image")
	}
	return body, nil
}
