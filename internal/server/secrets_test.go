package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/settings"
)

// fromElsewhere is a request as another device on the home network makes
// it: a program, without a browser's headers and without the PIN.
func fromElsewhere(s *Server, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "192.168.1.30:51000"
	req.Host = "192.168.1.50:8080" // the Pi, by its address
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	s.guarded(s.mux).ServeHTTP(rec, req)
	return rec
}

// keys records what credentials a server was sent.
type keys struct {
	mu  sync.Mutex
	got []string
}

func (k *keys) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k.mu.Lock()
		k.got = append(k.got, r.Header.Get("x-api-key")+r.Header.Get("Authorization"))
		k.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"Owner"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (k *keys) seen() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string{}, k.got...)
}

// A key, password or token left empty on the settings page means "the one
// stored": for the server it was stored for, and no other. Otherwise any
// device on the network could have the Pi send it to a server of its own.
func TestStoredSecretsGoOnlyWhereTheyWereStoredFor(t *testing.T) {
	s, _, _ := newSettingsServer(t, "")
	var immich, home, other keys
	immichSrv, homeSrv, trap := immich.server(t), home.server(t), other.server(t)
	if err := s.settings.Save(settings.Update{
		Photos: &settings.Photos{Source: "immich", Immich: &settings.Immich{URL: immichSrv.URL, APIKey: "IMMICH-KEY"}},
		Home:   &settings.Home{URL: homeSrv.URL, Token: "HA-TOKEN", Entities: []string{}},
	}); err != nil {
		t.Fatal(err)
	}

	// The same server, typed a little differently: the stored key is used.
	rec := fromElsewhere(s, "POST", "/api/settings/test-immich", `{"url":"`+strings.ToUpper(immichSrv.URL)+`/api/"}`)
	if rec.Code != http.StatusOK || strings.Join(immich.seen(), ",") != "IMMICH-KEY" {
		t.Fatalf("own server: %d %q, it was sent %q", rec.Code, rec.Body.String(), immich.seen())
	}
	// Another server: nothing is sent to it. (The NAS and the shared folder
	// follow the same rule; see the settings package.)
	for _, c := range []struct{ path, body string }{
		{"/api/settings/test-immich", `{"url":"` + trap.URL + `"}`},
		{"/api/settings/home-entities", `{"url":"` + trap.URL + `"}`},
	} {
		fromElsewhere(s, "POST", c.path, c.body)
	}
	if got := other.seen(); len(got) != 0 {
		t.Fatalf("a server of someone else's was sent %q", got)
	}

	// Saving does the same: a new address keeps no stored key or token.
	err := s.settings.Save(settings.Update{
		Photos: &settings.Photos{Source: "immich", Immich: &settings.Immich{URL: trap.URL}},
		Home:   &settings.Home{URL: trap.URL, Entities: []string{}},
	})
	if err == nil {
		t.Fatal("saved a new Immich address without its key")
	}
	if err := s.settings.Save(settings.Update{Home: &settings.Home{URL: trap.URL, Entities: []string{}}}); err == nil {
		if s.settings.HomeToken(trap.URL) != "" {
			t.Fatal("the stored Home Assistant token was kept for another address")
		}
	}
	if s.settings.ImmichKey(immichSrv.URL) != "IMMICH-KEY" {
		t.Error("the key for the owner's own server is gone")
	}
}

// The backup holds the passwords. Without a PIN, another device on the
// network has it only with a code that the TV shows, and only the pages on
// the Pi are told that code.
func TestABackupForAnotherDeviceNeedsTheCodeOnTheTV(t *testing.T) {
	s, srv, _ := newSettingsServer(t, "")
	if err := s.settings.Save(settings.Update{Home: &settings.Home{URL: "http://ha.local:8123", Token: "HA-TOKEN", Entities: []string{}}}); err != nil {
		t.Fatal(err)
	}
	backup := func(code string) *httptest.ResponseRecorder {
		return fromElsewhere(s, "POST", "/api/settings/backup", "{}", tvCodeHeader, code)
	}

	// Nobody is looking at the TV: no code can be shown.
	if rec := backup(""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without the dashboard open on the TV: %d %s", rec.Code, rec.Body)
	}
	tvPage, phonePage := s.hub.subscribeFrom(true), s.hub.subscribeFrom(false)
	codeOn := func(ch chan event) string {
		for {
			select {
			case ev := <-ch:
				if ev.name == "code" {
					var c struct{ Code string }
					_ = json.Unmarshal(ev.data, &c)
					return c.Code
				}
			case <-time.After(200 * time.Millisecond):
				return ""
			}
		}
	}

	rec := backup("")
	if rec.Code != http.StatusPreconditionRequired || strings.Contains(rec.Body.String(), "HA-TOKEN") {
		t.Fatalf("asked without the code: %d %s", rec.Code, rec.Body)
	}
	code := codeOn(tvPage)
	if len(code) != 6 {
		t.Fatalf("the TV was shown %q", code)
	}
	if got := codeOn(phonePage); got != "" {
		t.Fatalf("another device listening was told the code: %q", got)
	}
	if rec := backup("000000x"); rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("a wrong code: %d", rec.Code)
	}
	if rec := backup(code); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "HA-TOKEN") {
		t.Fatalf("with the code: %d %s", rec.Code, rec.Body)
	}
	// A code works once.
	if rec := backup(code); rec.Code != http.StatusPreconditionRequired {
		t.Fatalf("the same code again: %d", rec.Code)
	}

	// Guessing: after five wrong codes there is none for a while.
	codeOn(tvPage)
	for i := 0; i < tvCodeTries; i++ {
		backup("12345" + string(rune('a'+i)))
	}
	if rec := backup(""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong codes: %d", tvCodeTries, rec.Code)
	}

	// On the Pi itself (the settings on the TV) no code is asked for.
	res, err := http.Post(srv.URL+"/api/settings/backup", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("on the Pi itself: %d", res.StatusCode)
	}
}

// With a PIN, the PIN guards the backup, and no code is needed.
func TestWithAPINTheBackupNeedsThePIN(t *testing.T) {
	s, _, _ := newSettingsServer(t, "4711")
	if rec := fromElsewhere(s, "POST", "/api/settings/backup", "{}"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without the PIN: %d", rec.Code)
	}
	if rec := fromElsewhere(s, "POST", "/api/settings/backup", "{}", pinHeader, "4711"); rec.Code != http.StatusOK {
		t.Fatalf("with the PIN: %d %s", rec.Code, rec.Body)
	}
}
