package input

import (
	"sync"
	"time"
)

// holdDetector turns raw key events into "this key was held long enough".
// It is fed by every input device, so keys are tracked per device.
//
// Some TVs report a held remote button not as one long press but as a quick
// series of presses and releases. A release is therefore only believed after a
// short grace period without a new press, so both kinds of TV work.
type holdDetector struct {
	keys  map[uint16]bool
	hold  time.Duration
	grace time.Duration
	fire  func()

	mu    sync.Mutex
	holds map[holdKey]*holdState
}

type holdKey struct {
	device string
	code   uint16
}

type holdState struct {
	fireTimer    *time.Timer
	releaseTimer *time.Timer
}

// releaseGrace is how long a release must last before the hold is over.
const releaseGrace = 600 * time.Millisecond

func newHoldDetector(keys []uint16, hold time.Duration, fire func()) *holdDetector {
	set := map[uint16]bool{}
	for _, k := range keys {
		set[k] = true
	}
	grace := releaseGrace
	if hold < 3*grace {
		grace = hold / 3 // a tap must still end well before the hold time
	}
	return &holdDetector{keys: set, hold: hold, grace: grace, fire: fire, holds: map[holdKey]*holdState{}}
}

// defuse keeps a key that is being held from firing: the hold turned out to
// mean something else. The key has to be let go before it can fire again.
func (h *holdDetector) defuse(device string, code uint16) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st := h.holds[holdKey{device, code}]; st != nil && st.fireTimer != nil {
		st.fireTimer.Stop()
	}
}

// event handles one key event: value 1 is press, 0 release, 2 auto-repeat.
func (h *holdDetector) event(device string, code uint16, value int32) {
	if !h.keys[code] {
		return
	}
	k := holdKey{device, code}
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.holds[k]
	switch value {
	case 1, 2:
		if st != nil {
			// Still the same hold: a repeat, or a re-press within the grace period.
			if st.releaseTimer != nil {
				st.releaseTimer.Stop()
				st.releaseTimer = nil
			}
			return
		}
		if value == 2 {
			return // a repeat without a press we saw: ignore
		}
		st = &holdState{}
		h.holds[k] = st
		if h.hold <= 0 {
			go h.fire() // a dedicated exit key: acts at once
			return
		}
		// The state stays after firing, so one long hold fires exactly once,
		// however many presses the TV reports while the button stays down.
		st.fireTimer = time.AfterFunc(h.hold, h.fire)
	case 0:
		if st == nil {
			return
		}
		if st.releaseTimer != nil {
			st.releaseTimer.Stop()
		}
		st.releaseTimer = time.AfterFunc(h.grace, func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.holds[k] == st {
				if st.fireTimer != nil {
					st.fireTimer.Stop()
				}
				delete(h.holds, k)
			}
		})
	}
}

// forget drops the pending holds of a device that went away.
func (h *holdDetector) forget(device string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, st := range h.holds {
		if k.device == device {
			if st.fireTimer != nil {
				st.fireTimer.Stop()
			}
			if st.releaseTimer != nil {
				st.releaseTimer.Stop()
			}
			delete(h.holds, k)
		}
	}
}
