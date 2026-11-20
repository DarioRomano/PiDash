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
	mu sync.RWMutex
	// Each client, and whether it is a page on the Pi itself (the TV).
	clients map[chan event]bool
}

func newHub() *hub { return &hub{clients: map[chan event]bool{}} }

func (h *hub) subscribe() chan event { return h.subscribeFrom(false) }

// subscribeFrom adds a client; onThePi says it is a page on the Pi itself,
// which alone hears what is for the TV's eyes only (see toThePi).
func (h *hub) subscribeFrom(onThePi bool) chan event {
	ch := make(chan event, 32)
	h.mu.Lock()
	h.clients[ch] = onThePi
	h.mu.Unlock()
	return ch
}

// onThePi counts the clients that are pages on the Pi itself.
func (h *hub) onThePi() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for _, local := range h.clients {
		if local {
			n++
		}
	}
	return n
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
func (h *hub) broadcast(name string, v any) { h.send(name, v, false) }

// toThePi sends an event to the pages on the Pi itself only: what is meant
// for whoever looks at the TV, and for no other device on the network.
func (h *hub) toThePi(name string, v any) { h.send(name, v, true) }

func (h *hub) send(name string, v any, onlyThePi bool) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	ev := event{name: name, data: data}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch, local := range h.clients {
		if onlyThePi && !local {
			continue
		}
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
