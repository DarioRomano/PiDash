package input

import (
	"sync"
	"time"
)

// holdDetector turns raw key events into "this key was held long enough".
// It is fed by every input device, so keys are tracked per device.
type holdDetector struct {
	keys map[uint16]bool
	hold time.Duration
	fire func()

	mu     sync.Mutex
	timers map[holdKey]*time.Timer
}

type holdKey struct {
	device string
	code   uint16
}

func newHoldDetector(keys []uint16, hold time.Duration, fire func()) *holdDetector {
	set := map[uint16]bool{}
	for _, k := range keys {
		set[k] = true
	}
	return &holdDetector{keys: set, hold: hold, fire: fire, timers: map[holdKey]*time.Timer{}}
}

// event handles one key event: value 1 is press, 0 release, 2 auto-repeat.
func (h *holdDetector) event(device string, code uint16, value int32) {
	if !h.keys[code] {
		return
	}
	k := holdKey{device, code}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch value {
	case 1:
		if h.hold <= 0 {
			go h.fire() // a dedicated exit key: acts at once
			return
		}
		if t := h.timers[k]; t != nil {
			t.Stop()
		}
		h.timers[k] = time.AfterFunc(h.hold, func() {
			h.mu.Lock()
			delete(h.timers, k)
			h.mu.Unlock()
			h.fire()
		})
	case 0:
		if t := h.timers[k]; t != nil {
			t.Stop()
			delete(h.timers, k)
		}
	}
}

// forget drops the pending holds of a device that went away.
func (h *holdDetector) forget(device string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, t := range h.timers {
		if k.device == device {
			t.Stop()
			delete(h.timers, k)
		}
	}
}
