package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func guardedServer(dev bool) http.Handler {
	s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil)), ownNames: namesOfThisPi([]string{"tv.example.org"}), dev: dev}
	return s.guarded(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "done") }))
}

func TestOtherWebsitesCannotGiveOrders(t *testing.T) {
	h := guardedServer(false)
	try := func(method, host string, headers map[string]string) int {
		req := httptest.NewRequest(method, "http://"+host+"/api/power/off", nil)
		req.Host = host
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, c := range []struct {
		what    string
		method  string
		host    string
		headers map[string]string
		want    int
	}{
		{"PiDash's own page", "POST", "192.168.1.50:8080", map[string]string{"Origin": "http://192.168.1.50:8080", "Sec-Fetch-Site": "same-origin"}, 200},
		{"the dashboard on the TV", "POST", "127.0.0.1:8080", map[string]string{"Origin": "http://127.0.0.1:8080"}, 200},
		{"by the Pi's name", "POST", "raspberrypi.local:8080", map[string]string{"Origin": "http://raspberrypi.local:8080"}, 200},
		{"a script or the installer: no browser headers", "POST", "192.168.1.50:8080", nil, 200},
		{"a web page somewhere else", "POST", "192.168.1.50:8080", map[string]string{"Origin": "https://evil.example"}, 403},
		{"a web page that hides where it is from", "POST", "192.168.1.50:8080", map[string]string{"Origin": "null"}, 403},
		{"a form on another site, in a browser that sends no Origin", "POST", "192.168.1.50:8080", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"another program on the Pi's own address", "POST", "192.168.1.50:8080", map[string]string{"Origin": "http://192.168.1.50:3000", "Sec-Fetch-Site": "same-site"}, 403},
		{"a website app on the Pi itself", "PUT", "127.0.0.1:8080", map[string]string{"Origin": "https://www.youtube.com", "Sec-Fetch-Site": "cross-site"}, 403},
		{"the development server, outside development", "POST", "127.0.0.1:8080", map[string]string{"Origin": "http://localhost:5173"}, 403},
		// Reading is the browser's to guard: it does not hand a page what
		// another site answers. Nothing is changed by reading.
		{"reading from elsewhere", "GET", "192.168.1.50:8080", map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}, 200},
		// A name of the attacker's own, pointed at the Pi's address.
		{"a name from the internet pointed at the Pi", "GET", "pi.evil.example:8080", nil, 403},
		{"the same, with a matching origin", "POST", "pi.evil.example:8080", map[string]string{"Origin": "http://pi.evil.example:8080"}, 403},
		{"a name allowed in the settings", "POST", "tv.example.org", map[string]string{"Origin": "http://tv.example.org"}, 200},
		{"a short name", "GET", "pi5:8080", nil, 200},
		{"a home router's name", "GET", "pi5.fritz.box:8080", nil, 200},
		{"an IPv6 address", "GET", "[fe80::1]:8080", nil, 200},
		{"no name at all", "GET", "", nil, 403},
	} {
		if got := try(c.method, c.host, c.headers); got != c.want {
			t.Errorf("%s: %d, want %d", c.what, got, c.want)
		}
	}
	// While developing, the pages may come from a server on this machine.
	req := httptest.NewRequest("POST", "http://127.0.0.1:8080/api/apps/start", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	guardedServer(true).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("the development server while developing: %d", rec.Code)
	}
}

func TestPagesSayWhatTheyMayLoad(t *testing.T) {
	rec := httptest.NewRecorder()
	guardedServer(false).ServeHTTP(rec, httptest.NewRequest("GET", "http://127.0.0.1:8080/", nil))
	h := rec.Header()
	policy := h.Get("Content-Security-Policy")
	for _, part := range []string{"script-src 'self'", "object-src 'none'", "frame-ancestors 'none'", "connect-src 'self'"} {
		if !strings.Contains(policy, part) {
			t.Errorf("the content policy lacks %q: %s", part, policy)
		}
	}
	if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("headers = %v", h)
	}
}

func TestWrongPINsLockForLongerEachTime(t *testing.T) {
	g := &pinGuard{pin: "4821"}
	guess := func(pin string) int {
		req := httptest.NewRequest("POST", "/api/type", nil)
		if pin != "" {
			req.Header.Set(pinHeader, pin)
		}
		rec := httptest.NewRecorder()
		g.allow(rec, req)
		return rec.Code
	}
	lockedFor := func() time.Duration {
		for i := 0; i < pinMaxFailures; i++ {
			guess("0000")
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		d := time.Until(g.lockedUntil).Round(time.Minute)
		g.lockedUntil = time.Time{} // the wait is over
		return d
	}
	var waits []time.Duration
	for i := 0; i < 8; i++ {
		waits = append(waits, lockedFor())
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("lockouts = %v, want %v", waits, want)
		}
	}
	// Asking without a PIN is not a guess, and the right PIN starts afresh.
	for i := 0; i < 20; i++ {
		guess("")
	}
	if code := guess("4821"); code != 200 {
		t.Fatalf("the right PIN: %d", code)
	}
	if got := lockedFor(); got != time.Minute {
		t.Errorf("after the right PIN the first lockout is %v again, want a minute", got)
	}
}
