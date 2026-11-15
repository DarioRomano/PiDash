package sound

import (
	"context"
	"sync"
)

// Mock is a pretend sound service with two outputs, for development (-mock)
// and the tests of the pages.
type Mock struct {
	mu       sync.Mutex
	outputs  []Output
	watchers map[int]func()
	nextID   int
	// Tested counts the test sounds played, by output.
	Tested map[string]int
}

// NewMock returns a sound service with the TV and a Bluetooth speaker.
func NewMock() *Mock {
	return &Mock{Tested: map[string]int{}, outputs: []Output{
		{ID: "hdmi", Name: "TV (HDMI)", Kind: "hdmi", Active: true, Volume: 100},
		Speaker,
	}}
}

// Status implements Manager.
func (m *Mock) Status(context.Context) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Status{Available: true, Outputs: append([]Output{}, m.outputs...)}
}

func (m *Mock) change(id string, fn func(o *Output)) error {
	m.mu.Lock()
	for i := range m.outputs {
		if m.outputs[i].ID == id {
			fn(&m.outputs[i])
			m.mu.Unlock()
			m.notify()
			return nil
		}
	}
	m.mu.Unlock()
	return ErrUnknown
}

// notify tells the watchers that something changed.
func (m *Mock) notify() {
	m.mu.Lock()
	var all []func()
	for _, fn := range m.watchers {
		all = append(all, fn)
	}
	m.mu.Unlock()
	for _, fn := range all {
		fn()
	}
}

// Watch implements Manager.
func (m *Mock) Watch(ctx context.Context, changed func()) {
	m.mu.Lock()
	if m.watchers == nil {
		m.watchers = map[int]func(){}
	}
	id := m.nextID
	m.nextID++
	m.watchers[id] = changed
	m.mu.Unlock()
	<-ctx.Done()
	m.mu.Lock()
	delete(m.watchers, id)
	m.mu.Unlock()
}

// Speaker is the Bluetooth speaker of the pretend sound service.
var Speaker = Output{ID: "speaker", Name: "Living room speaker", Kind: "bluetooth", Volume: 40}

// Connect makes an output appear, as when headphones are switched on.
func (m *Mock) Connect(o Output) {
	m.mu.Lock()
	for _, have := range m.outputs {
		if have.ID == o.ID {
			m.mu.Unlock()
			return
		}
	}
	o.Active = false
	m.outputs = append(m.outputs, o)
	m.mu.Unlock()
	m.notify()
}

// Disconnect makes an output disappear. Like the real sound service, the
// pretend one falls back to its first output if the one in use went away.
func (m *Mock) Disconnect(id string) {
	m.mu.Lock()
	for i, o := range m.outputs {
		if o.ID == id {
			m.outputs = append(m.outputs[:i], m.outputs[i+1:]...)
			if o.Active && len(m.outputs) > 0 {
				m.outputs[0].Active = true
			}
			break
		}
	}
	m.mu.Unlock()
	m.notify()
}

// Use implements Manager.
func (m *Mock) Use(_ context.Context, id string) error {
	if err := m.change(id, func(*Output) {}); err != nil {
		return err
	}
	m.mu.Lock()
	for i := range m.outputs {
		m.outputs[i].Active = m.outputs[i].ID == id
	}
	m.mu.Unlock()
	m.notify()
	return nil
}

// SetVolume implements Manager.
func (m *Mock) SetVolume(_ context.Context, id string, percent int) error {
	return m.change(id, func(o *Output) { o.Volume = max(0, min(100, percent)) })
}

// SetMuted implements Manager.
func (m *Mock) SetMuted(_ context.Context, id string, muted bool) error {
	return m.change(id, func(o *Output) { o.Muted = muted })
}

// Test implements Manager.
func (m *Mock) Test(_ context.Context, id string) error {
	return m.change(id, func(*Output) { m.Tested[id]++ })
}
