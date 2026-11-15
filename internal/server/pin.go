package server

import (
	"crypto/subtle"
	"net/http"
	"sync"
	"time"
)

// pinGuard protects the phone's mouse and keyboard with an optional PIN.
// After a few wrong attempts it refuses everything for a while, so the PIN
// cannot be found by trying them all.
type pinGuard struct {
	pin string

	mu          sync.Mutex
	failures    int
	lockouts    int // in a row, without a right PIN in between
	lockedUntil time.Time
}

const (
	pinHeader      = "X-PiDash-PIN"
	pinMaxFailures = 5
	pinLockout     = time.Minute
	// Each lockout in a row lasts twice as long as the one before, up to
	// this: trying all four-digit PINs then takes months instead of a day.
	pinLockoutMax = time.Hour
)

// allow reports whether the request may proceed, and answers it otherwise:
// 401 when the PIN is missing or wrong, 429 while locked out.
func (g *pinGuard) allow(w http.ResponseWriter, r *http.Request) bool {
	if g.pin == "" {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if time.Now().Before(g.lockedUntil) {
		http.Error(w, "too many wrong PINs; try again in a minute", http.StatusTooManyRequests)
		return false
	}
	got := r.Header.Get(pinHeader)
	if subtle.ConstantTimeCompare([]byte(got), []byte(g.pin)) == 1 {
		g.failures, g.lockouts = 0, 0
		return true
	}
	// A request without a PIN is the phone asking for the first time, not a guess.
	if got != "" {
		g.failures++
		if g.failures >= pinMaxFailures {
			g.failures = 0
			wait := pinLockout << min(g.lockouts, 10)
			if wait > pinLockoutMax {
				wait = pinLockoutMax
			}
			g.lockouts++
			g.lockedUntil = time.Now().Add(wait)
		}
	}
	http.Error(w, "PIN required", http.StatusUnauthorized)
	return false
}
