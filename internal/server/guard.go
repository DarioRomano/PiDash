package server

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// PiDash answers anyone on the home network: that is how the phone remote
// and the settings page work. What it must not do is take orders from a web
// page. Without a guard, any page open in a browser at home, or inside a
// website app on the Pi, could quietly send it requests: start an app, switch
// the TV off, shut the Pi down.
//
// Two checks keep pages out. A request that changes something must come from
// PiDash's own pages, which the browser says in every such request. And the
// name PiDash is addressed by must be a home network's kind of name: a page
// on the internet can point a name of its own at the Pi's address and then
// talk to the Pi as if it were its own server, but it cannot do so under a
// name like "raspberrypi.local".

// Names a home network gives its machines, by their ending.
var homeSuffixes = []string{".local", ".lan", ".home", ".home.arpa", ".internal", ".intranet", ".localdomain", ".fritz.box", ".box", ".localhost"}

// allowedHost reports whether a request addressed to host (the Host header,
// with or without a port) may be answered.
func (s *Server) allowedHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	switch {
	case host == "":
		return false
	case net.ParseIP(host) != nil:
		return true // an address is nobody's name
	case !strings.Contains(host, "."):
		return true // "raspberrypi", "localhost": no such names on the internet
	}
	for _, suffix := range homeSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	for _, own := range s.ownNames {
		if host == own {
			return true
		}
	}
	return false
}

// sameSite reports whether a request that changes something was made by one
// of PiDash's own pages, or by a program rather than a page (the installer,
// a script): those send none of the headers a browser adds.
//
// With dev set, pages served by a development server on this machine count
// as PiDash's own.
func sameSite(r *http.Request, dev bool) bool {
	origin := r.Header.Get("Origin")
	if dev && origin != "" {
		if u, err := url.Parse(origin); err == nil {
			if ip := net.ParseIP(u.Hostname()); u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()) {
				return true
			}
		}
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host)
}

// contentPolicy tells the browser what PiDash's pages may load: their own
// scripts and nothing else that runs. Sound and video may come from anywhere,
// since radio stations do; pictures only from the Pi.
const contentPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; " +
	"media-src * blob:; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// guarded wraps the server's routes with the checks above.
func (s *Server) guarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowedHost(r.Host) {
			s.log.Warn("refused a request addressed to a name that is not this Pi's", "name", r.Host, "from", r.RemoteAddr)
			http.Error(w, "PiDash does not answer to this name. Open it by the Pi's address or its name on your home network; other names can be allowed with server.names in config.toml.", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && !sameSite(r, s.dev) {
			s.log.Warn("refused a request made by another website", "path", r.URL.Path, "origin", r.Header.Get("Origin"), "from", r.RemoteAddr)
			http.Error(w, "this request was made by another website, not by PiDash's own pages", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Content-Security-Policy", contentPolicy)
		next.ServeHTTP(w, r)
	})
}

// namesOfThisPi returns the names this machine goes by beyond the home
// network's usual ones: its full host name, and those from the settings.
func namesOfThisPi(extra []string) []string {
	var out []string
	if name, err := os.Hostname(); err == nil && name != "" {
		out = append(out, strings.ToLower(name))
	}
	for _, n := range extra {
		if n = strings.ToLower(strings.TrimSpace(n)); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// fromThisPi reports whether a request was made on the Pi itself, as the
// dashboard on the TV does.
func fromThisPi(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
