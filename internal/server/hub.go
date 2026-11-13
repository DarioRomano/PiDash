package server

import (
	"encoding/json"
	"sync"
)

// event is one Server-Sent Event: a name and a JSON payload.
type event struct {
	name string
	data []byte
}

// hub fans events out to every connected SSE client. Slow clients that cannot
// keep up have their oldest events dropped rather than blocking the publisher.
type hub struct {
	mu      sync.RWMutex
	clients map[chan event]struct{}
}

func newHub() *hub { return &hub{clients: map[chan event]struct{}{}} }

func (h *hub) subscribe() chan event {
	ch := make(chan event, 32)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan event) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *hub) count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// broadcast JSON-encodes v and sends it to all clients as the named event.
func (h *hub) broadcast(name string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	ev := event{name: name, data: data}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- ev:
		default:
			// Drop the oldest queued event to make room, then enqueue.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}
}
