package server

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// The backup holds the passwords stored on the Pi. With a PIN, the PIN
// guards it. Without one, PiDash trusts the people at home, but not every
// device on the network: a phone or computer asking for the backup must
// type a code that appears on the TV, which only whoever is in the room can
// read. The code is sent to the pages on the Pi alone, never to the others.
type tvCode struct {
	mu       sync.Mutex
	code     string
	until    time.Time
	shownAt  time.Time
	failures int
	lockouts int
	locked   time.Time
	now      func() time.Time
}

const (
	tvCodeHeader = "X-PiDash-Code"
	// tvCodeLife is how long a code may be typed after it appeared.
	tvCodeLife = 5 * time.Minute
	// After this many wrong codes a new one is needed, and not at once:
	// a minute, then twice as long each time, up to an hour.
	tvCodeTries = 5
)

func (c *tvCode) time() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// accept reports whether got is the code on the TV. A code works once.
func (c *tvCode) accept(got string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.time()
	if got == "" || c.code == "" || now.After(c.until) || now.Before(c.locked) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(c.code)) == 1 {
		c.code, c.failures, c.lockouts = "", 0, 0
		return true
	}
	c.failures++
	if c.failures >= tvCodeTries {
		// This code has been guessed at enough: it is gone, and the next
		// comes after a pause.
		c.code, c.failures = "", 0
		c.locked = now.Add(min(time.Minute<<min(c.lockouts, 10), time.Hour))
		c.lockouts++
	}
	return false
}

// issue returns the code to show on the TV: the one that is valid, or a
// new one. show is false when it was shown a moment ago (asking again and
// again does not fill the TV with it); wait is how long until one can be
// had, after too many wrong codes.
func (c *tvCode) issue() (code string, show bool, wait time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.time()
	if now.Before(c.locked) {
		return "", false, c.locked.Sub(now)
	}
	if c.code == "" || now.After(c.until) {
		n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			return "", false, time.Minute
		}
		c.code, c.until, c.failures, c.shownAt = fmt.Sprintf("%06d", n.Int64()), now.Add(tvCodeLife), 0, time.Time{}
	}
	if now.Sub(c.shownAt) < 10*time.Second {
		return c.code, false, 0
	}
	c.shownAt = now
	return c.code, true, 0
}

// codeFromTheTV lets a request for something the code guards go ahead, or
// answers it: 428 with the code on its way to the TV. Requests from the Pi
// itself, and any request where a PIN guards instead, need no code.
func (s *Server) codeFromTheTV(w http.ResponseWriter, r *http.Request) bool {
	if s.pin.pin != "" || fromThisPi(r) || s.tvCode.accept(r.Header.Get(tvCodeHeader)) {
		return true
	}
	if s.hub.onThePi() == 0 {
		http.Error(w, "A code shown on the TV is needed, but the dashboard is not open on the TV. Switch the TV to the Pi and try again.", http.StatusServiceUnavailable)
		return false
	}
	code, show, wait := s.tvCode.issue()
	if wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		http.Error(w, "Too many wrong codes. Try again later.", http.StatusTooManyRequests)
		return false
	}
	if show {
		s.hub.toThePi("code", struct {
			Code string `json:"code"`
		}{code})
	}
	w.Header().Set("X-PiDash-Code-Required", "1")
	http.Error(w, "Type the code that is shown on the TV now.", http.StatusPreconditionRequired)
	return false
}
