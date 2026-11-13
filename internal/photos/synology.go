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
	"strconv"
	"strings"
	"sync"
	"time"
)

// Synology reads photos from Synology Photos (DSM 7) through its web API.
//
// Synology Photos has two libraries: each user's Personal Space and the common
// Shared Space. Both appear as top-level folders. Folder ids used here are
// "p:<id>" and "s:<id>"; "p:" and "s:" alone are the libraries' roots.
type Synology struct {
	base       string
	user, pass string
	hc         *http.Client

	mu    sync.Mutex
	sid   string
	roots map[string]int // space -> root folder id
}

// SynologyOptions configure the connection to the NAS.
type SynologyOptions struct {
	// URL is the address of DSM, e.g. "http://192.168.1.20:5000".
	URL      string
	Username string
	Password string
	// InsecureTLS accepts a self-signed HTTPS certificate.
	InsecureTLS bool
}

// NewSynology returns a Synology Photos source.
func NewSynology(o SynologyOptions) *Synology {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.InsecureTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit opt-in for a NAS on the home network
	}
	return &Synology{
		base:  strings.TrimRight(o.URL, "/"),
		user:  o.Username,
		pass:  o.Password,
		hc:    &http.Client{Timeout: 90 * time.Second, Transport: tr},
		roots: map[string]int{},
	}
}

const (
	spacePersonal = "p"
	spaceShared   = "s"
)

var spaceLabel = map[string]string{spacePersonal: "Personal Space", spaceShared: "Shared Space"}

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
	if !ok || (space != spacePersonal && space != spaceShared) {
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

func (s *Synology) resolve(ctx context.Context, id string) (space string, folderID int, err error) {
	space, n, isRoot, err := splitID(id)
	if err != nil {
		return "", 0, err
	}
	if isRoot {
		n, err = s.rootID(ctx, space)
	}
	return space, n, err
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
			return nil, firstErr
		}
		return out, nil
	}

	space, id, err := s.resolve(ctx, parent)
	if err != nil {
		return nil, err
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
	space, id, err := s.resolve(ctx, folder.ID)
	if err != nil {
		return nil, err
	}
	var out []Item
	const page = 500
	for offset := 0; offset < maxItems; offset += page {
		var data struct {
			List []struct {
				ID         int    `json:"id"`
				Filename   string `json:"filename"`
				Time       int64  `json:"time"`
				Type       string `json:"type"`
				Additional struct {
					Thumbnail struct {
						CacheKey string `json:"cache_key"`
						UnitID   int    `json:"unit_id"`
					} `json:"thumbnail"`
				} `json:"additional"`
			} `json:"list"`
		}
		err := s.call(ctx, url.Values{
			"api": {apiName(space, "Browse.Item")}, "version": {"1"}, "method": {"list"},
			"folder_id": {strconv.Itoa(id)}, "offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(page)},
			"additional": {`["thumbnail"]`},
		}, &data)
		if err != nil {
			return out, err
		}
		for _, it := range data.List {
			// Photos and "live" photos have a still image; videos are skipped.
			if it.Type != "photo" && it.Type != "live" {
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
			out = append(out, Item{
				Key:     space + "-" + strconv.Itoa(it.ID),
				Name:    name,
				Folder:  folder.Path,
				Taken:   time.Unix(it.Time, 0).UTC(),
				Version: it.Additional.Thumbnail.CacheKey,
				ID:      space + ":" + strconv.Itoa(unit),
			})
		}
		if len(data.List) < page {
			break
		}
	}
	return out, nil
}

// Fetch implements Source. The preview is Synology's largest ready-made
// thumbnail (1280 pixels on the long side), already rotated upright.
func (s *Synology) Fetch(ctx context.Context, item Item, original bool) ([]byte, bool, error) {
	space, unit, _, err := splitID(item.ID)
	if err != nil {
		return nil, false, err
	}
	var params url.Values
	var limit int64
	if original {
		params = url.Values{
			"api": {apiName(space, "Download")}, "version": {"1"}, "method": {"download"},
			"unit_id": {"[" + strconv.Itoa(unit) + "]"},
		}
		limit = 100 << 20
	} else {
		params = url.Values{
			"api": {apiName(space, "Thumbnail")}, "version": {"1"}, "method": {"get"},
			"mode": {"download"}, "type": {"unit"}, "size": {"xl"},
			"id": {strconv.Itoa(unit)}, "cache_key": {item.Version},
		}
		limit = 32 << 20
	}
	body, contentType, err := s.do(ctx, params, limit)
	if err != nil {
		return nil, false, err
	}
	if len(body) == 0 || strings.Contains(contentType, "json") || strings.Contains(contentType, "html") {
		return nil, false, errors.New("the NAS did not return an image")
	}
	return body, !original, nil
}
