package videos

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

// Synology is the shared folders of a Synology NAS, read through File
// Station's web API with the same account that reads the photos. A video is
// not copied to the Pi: the player is given an address on the Pi itself,
// through which PiDash passes the file on as the player asks for it.
type Synology struct {
	base       string
	user, pass string
	// roots are the shared folders to offer ("/video"); empty offers every
	// shared folder the account may read.
	roots []string
	// self says where PiDash is reached on the Pi itself, for the player.
	self func() string
	// secret makes the addresses of the videos: the same for the same
	// video every time, so that the player remembers where it stopped.
	secret []byte
	hc     *http.Client
	// stream has no time limit: a film takes hours.
	stream *http.Client

	mu  sync.Mutex
	sid string
}

// SynologyOptions configure the connection to the NAS.
type SynologyOptions struct {
	// URL is the address of DSM, e.g. "http://192.168.1.20:5000".
	URL, Username, Password string
	InsecureTLS             bool
	// Folders are the shared folders to offer; empty offers them all.
	Folders []string
	// Self says where PiDash listens on the Pi itself: "http://127.0.0.1:8080".
	Self func() string
	// SecretPath is a file that keeps the secret from one start to the next.
	SecretPath string
}

// NewSynology returns the NAS as a source of videos.
func NewSynology(o SynologyOptions) *Synology {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.InsecureTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit opt-in for a NAS on the home network
	}
	tr.ResponseHeaderTimeout = 30 * time.Second
	s := &Synology{
		base: strings.TrimRight(o.URL, "/"), user: o.Username, pass: o.Password, self: o.Self,
		hc:     &http.Client{Timeout: 30 * time.Second, Transport: tr},
		stream: &http.Client{Transport: tr},
	}
	for _, f := range o.Folders {
		if f = cleanNAS(f); f != "/" {
			s.roots = append(s.roots, f)
		}
	}
	s.secret = loadSecret(o.SecretPath)
	return s
}

// loadSecret reads the secret, making one the first time.
func loadSecret(file string) []byte {
	if file != "" {
		if data, err := os.ReadFile(file); err == nil && len(data) >= 32 {
			return data
		}
	}
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	if file != "" {
		_ = os.WriteFile(file, secret, 0o600)
	}
	return secret
}

func cleanNAS(p string) string { return path.Clean("/" + strings.TrimSpace(p)) }

// allowed reports whether a path on the NAS is inside what was set up.
func (s *Synology) allowed(p string) bool {
	if p == "/" || p != cleanNAS(p) {
		return false
	}
	if len(s.roots) == 0 {
		return true
	}
	for _, r := range s.roots {
		if p == r || strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

type synoReply struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   struct {
		Code int `json:"code"`
	} `json:"error"`
}

type synoError int

func (e synoError) Error() string {
	switch int(e) {
	case 105, 407:
		return "the NAS account is not allowed to read this folder"
	case 408:
		return ErrNotFound.Error()
	case 400, 401, 402, 403, 404, 406:
		return "the NAS did not accept the login; check the account under Background photos in the settings"
	}
	return fmt.Sprintf("the NAS returned error %d", int(e))
}

func (s *Synology) endpoint() string { return s.base + "/webapi/entry.cgi" }

func (s *Synology) login(ctx context.Context) (string, error) {
	form := url.Values{
		"api": {"SYNO.API.Auth"}, "version": {"6"}, "method": {"login"}, "session": {"FileStation"},
		"account": {s.user}, "passwd": {s.pass}, "format": {"sid"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint(), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.hc.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fmt.Errorf("cannot reach the NAS at %s: %w", s.base, err)
	}
	defer resp.Body.Close()
	var reply synoReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&reply); err != nil {
		return "", fmt.Errorf("%s does not look like a Synology NAS", s.base)
	}
	if !reply.Success {
		return "", synoError(reply.Error.Code)
	}
	var data struct {
		SID string `json:"sid"`
	}
	if err := json.Unmarshal(reply.Data, &data); err != nil || data.SID == "" {
		return "", errors.New("NAS login returned no session")
	}
	return data.SID, nil
}

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

func expired(err error) bool {
	var se synoError
	return errors.As(err, &se) && (se == 106 || se == 107 || se == 119)
}

// call asks File Station something, signing in again once if the session ran out.
func (s *Synology) call(ctx context.Context, params url.Values, out any) error {
	for attempt := 0; ; attempt++ {
		sid, err := s.session(ctx, attempt > 0)
		if err != nil {
			return err
		}
		q := url.Values{"_sid": {sid}}
		for k, v := range params {
			q[k] = v
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint()+"?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		resp, err := s.hc.Do(req)
		if err != nil {
			var ue *url.Error
			if errors.As(err, &ue) {
				err = ue.Err
			}
			return fmt.Errorf("cannot reach the NAS at %s: %w", s.base, err)
		}
		var reply synoReply
		err = json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&reply)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("the NAS gave an answer that could not be read (HTTP %d)", resp.StatusCode)
		}
		if !reply.Success {
			err := synoError(reply.Error.Code)
			if expired(err) && attempt == 0 {
				continue
			}
			if reply.Error.Code == 408 {
				return ErrNotFound
			}
			return err
		}
		return json.Unmarshal(reply.Data, out)
	}
}

type synoFile struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	IsDir      bool   `json:"isdir"`
	Additional struct {
		Size int64 `json:"size"`
	} `json:"additional"`
}

// List implements Source.
func (s *Synology) List(ctx context.Context, folder string) (title, parent string, top bool, entries []Entry, err error) {
	if folder == "" {
		switch len(s.roots) {
		case 1:
			folder = s.roots[0]
		case 0:
			var data struct {
				Shares []synoFile `json:"shares"`
			}
			if err := s.call(ctx, url.Values{"api": {"SYNO.FileStation.List"}, "version": {"2"}, "method": {"list_share"}}, &data); err != nil {
				return "", "", false, nil, err
			}
			for _, sh := range data.Shares {
				if !skipped(sh.Name) {
					entries = append(entries, Entry{Name: sh.Name, Path: cleanNAS(sh.Path), Folder: true})
				}
			}
			arrange(entries)
			return "", "", true, entries, nil
		default:
			for _, r := range s.roots {
				entries = append(entries, Entry{Name: path.Base(r), Path: r, Folder: true})
			}
			return "", "", true, entries, nil
		}
	}
	if !s.allowed(folder) {
		return "", "", false, nil, ErrNotFound
	}
	const page = 2000
	for offset := 0; offset < 20000; offset += page {
		var data struct {
			Total int        `json:"total"`
			Files []synoFile `json:"files"`
		}
		err := s.call(ctx, url.Values{
			"api": {"SYNO.FileStation.List"}, "version": {"2"}, "method": {"list"}, "folder_path": {folder},
			"additional": {`["size"]`}, "offset": {fmt.Sprint(offset)}, "limit": {fmt.Sprint(page)},
		}, &data)
		if err != nil {
			return "", "", false, nil, err
		}
		for _, f := range data.Files {
			at := path.Join(folder, f.Name)
			switch {
			case skipped(f.Name):
			case f.IsDir:
				entries = append(entries, Entry{Name: f.Name, Path: at, Folder: true})
			case IsVideo(f.Name):
				entries = append(entries, Entry{Name: f.Name, Path: at, Size: f.Additional.Size})
			case isSubtitle(f.Name):
				entries = append(entries, Entry{Name: f.Name, Path: at, subtitle: true})
			}
		}
		if len(data.Files) < page || offset+len(data.Files) >= data.Total {
			break
		}
	}
	arrange(entries)
	title = path.Base(folder)
	// The folder above: one further up, or the top where this is where it starts.
	up := path.Dir(folder)
	switch {
	case len(s.roots) == 1 && folder == s.roots[0]:
		return title, "", true, entries, nil
	case up == "/" || !s.allowed(up):
		return title, "", false, entries, nil
	}
	return title, up, false, entries, nil
}

// Above implements Source.
func (s *Synology) Above(file string) string { return path.Dir(file) }

// token signs a path on the NAS, so that only addresses PiDash made itself
// are passed on.
func (s *Synology) token(p string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(p))
	return hex.EncodeToString(mac.Sum(nil)[:16])
}

// Target implements Source: an address on the Pi itself that passes the file on.
func (s *Synology) Target(_ context.Context, file string) (string, error) {
	if !s.allowed(file) {
		return "", ErrNotFound
	}
	// The same address for the same video every time: the player remembers
	// by it where the video stopped. The ending tells it what the file is.
	return s.self() + "/api/videos/stream/" + s.token(file) + "/" + base64.RawURLEncoding.EncodeToString([]byte(file)) + strings.ToLower(path.Ext(file)), nil
}

// Streamer is a source whose videos reach the player through PiDash.
type Streamer interface {
	Stream(w http.ResponseWriter, r *http.Request, token, name string)
}

// Stream passes a file of the NAS on to the player, the part of it that was asked for.
func (s *Synology) Stream(w http.ResponseWriter, r *http.Request, token, name string) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSuffix(name, path.Ext(name)))
	file := string(raw)
	if err != nil || !s.allowed(file) || !hmac.Equal([]byte(token), []byte(s.token(file))) {
		http.Error(w, ErrNotFound.Error(), http.StatusNotFound)
		return
	}
	for attempt := 0; ; attempt++ {
		sid, err := s.session(r.Context(), attempt > 0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		list, _ := json.Marshal([]string{file})
		q := url.Values{"api": {"SYNO.FileStation.Download"}, "version": {"2"}, "method": {"download"}, "path": {string(list)}, "mode": {`"open"`}, "_sid": {sid}}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, s.endpoint()+"?"+q.Encode(), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, h := range []string{"Range", "If-Range"} {
			if v := r.Header.Get(h); v != "" {
				req.Header.Set(h, v)
			}
		}
		resp, err := s.stream.Do(req)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) || r.Context().Err() == nil {
				http.Error(w, "cannot reach the NAS", http.StatusBadGateway)
			}
			return
		}
		// A refusal comes as a small piece of JSON, not as the file.
		if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") || resp.StatusCode >= 400 {
			var reply synoReply
			_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&reply)
			resp.Body.Close()
			if expired(synoError(reply.Error.Code)) && attempt == 0 {
				continue
			}
			http.Error(w, synoError(reply.Error.Code).Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for _, h := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified", "Etag"} {
			if v := resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if r.Method != http.MethodHead {
			_, _ = io.Copy(w, resp.Body)
		}
		return
	}
}
