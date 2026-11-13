package sound

import (
	"context"
	"sync"
)

// Mock is a pretend sound service with two outputs, for development (-mock)
// and the tests of the pages.
type Mock struct {
	mu      sync.Mutex
	outputs []Output
	// Tested counts the test sounds played, by output.
	Tested map[string]int
}

// NewMock returns a sound service with the TV and a Bluetooth speaker.
func NewMock() *Mock {
	return &Mock{Tested: map[string]int{}, outputs: []Output{
		{ID: "hdmi", Name: "TV (HDMI)", Kind: "hdmi", Active: true, Volume: 100},
		{ID: "speaker", Name: "Living room speaker", Kind: "bluetooth", Volume: 40},
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
	defer m.mu.Unlock()
	for i := range m.outputs {
		if m.outputs[i].ID == id {
			fn(&m.outputs[i])
			return nil
		}
	}
	return ErrUnknown
}

// Use implements Manager.
func (m *Mock) Use(_ context.Context, id string) error {
	if err := m.change(id, func(*Output) {}); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.outputs {
		m.outputs[i].Active = m.outputs[i].ID == id
	}
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
