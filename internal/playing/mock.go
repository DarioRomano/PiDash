package playing

import (
	"context"
	"sync"
)

// Mock is a pretend player for development (-mock) and the tests of the pages.
type Mock struct {
	OnChange func(*Track)

	mu      sync.Mutex
	current *Track
	// Pressed lists the buttons pressed so far.
	Pressed []string
}

// Set makes the pretend player play a track, or nothing.
func (m *Mock) Set(t *Track) {
	m.mu.Lock()
	m.current = t
	fn := m.OnChange
	m.mu.Unlock()
	if fn != nil {
		fn(m.Current())
	}
}

// Current returns what is playing, or nil.
func (m *Mock) Current() *Track {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil {
		return nil
	}
	t := *m.current
	return &t
}

// Control presses a button: play/pause really pauses, the others are noted.
func (m *Mock) Control(_ context.Context, button string) error {
	m.mu.Lock()
	if m.current == nil {
		m.mu.Unlock()
		return ErrNothing
	}
	m.Pressed = append(m.Pressed, button)
	if button == "playpause" {
		m.current.Playing = !m.current.Playing
	}
	fn := m.OnChange
	m.mu.Unlock()
	if fn != nil {
		fn(m.Current())
	}
	return nil
}

// ArtFile returns nothing: the pretend player has no covers on disk.
func (m *Mock) ArtFile() (string, bool) { return "", false }
