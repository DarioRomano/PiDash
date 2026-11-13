package bluetooth

import (
	"context"
	"sync"
	"time"
)

// Mock is a pretend Bluetooth with a few devices, for development (-mock)
// and the tests of the pages.
type Mock struct {
	mu        sync.Mutex
	off       bool
	searching time.Time // when the search started; zero when not searching
	devices   []Device
	nearby    []Device
}

// NewMock returns a Bluetooth with a controller and a speaker paired, and a
// second controller and a keyboard waiting nearby.
func NewMock() *Mock {
	return &Mock{
		devices: []Device{
			{Address: "AA:00:00:00:00:01", Name: "Xbox Wireless Controller", Kind: "controller", Paired: true, Connected: true, Battery: percent(80)},
			{Address: "AA:00:00:00:00:02", Name: "Living room speaker", Kind: "audio", Paired: true},
		},
		nearby: []Device{
			{Address: "AA:00:00:00:00:03", Name: "8BitDo Pro 2", Kind: "controller", Signal: -48},
			{Address: "AA:00:00:00:00:04", Name: "Keyboard K380", Kind: "keyboard", Signal: -63},
		},
	}
}

// Status implements Manager.
func (m *Mock) Status(context.Context) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.searching.IsZero() && time.Since(m.searching) > time.Minute {
		m.searching = time.Time{}
	}
	st := Status{Available: true, On: !m.off, Searching: !m.searching.IsZero(), Devices: []Device{}}
	if m.off {
		return st
	}
	st.Devices = append(st.Devices, m.devices...)
	if st.Searching {
		st.Devices = append(st.Devices, m.nearby...)
	}
	arrange(st.Devices)
	return st
}

// Power implements Manager.
func (m *Mock) Power(_ context.Context, on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.off = !on
	if !on {
		m.searching = time.Time{}
		for i := range m.devices {
			m.devices[i].Connected = false
		}
	}
	return nil
}

// Search implements Manager.
func (m *Mock) Search(_ context.Context, on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.off {
		return explain(errNotReady)
	}
	m.searching = time.Time{}
	if on {
		m.searching = time.Now()
	}
	return nil
}

func (m *Mock) find(address string) *Device {
	for i := range m.devices {
		if m.devices[i].Address == address {
			return &m.devices[i]
		}
	}
	return nil
}

// Pair implements Manager.
func (m *Mock) Pair(_ context.Context, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, d := range m.nearby {
		if d.Address == address {
			d.Paired, d.Connected, d.Signal = true, true, 0
			m.devices = append(m.devices, d)
			m.nearby = append(m.nearby[:i], m.nearby[i+1:]...)
			return nil
		}
	}
	return ErrUnknown
}

// Connect implements Manager.
func (m *Mock) Connect(_ context.Context, address string) error { return m.set(address, true) }

// Disconnect implements Manager.
func (m *Mock) Disconnect(_ context.Context, address string) error { return m.set(address, false) }

func (m *Mock) set(address string, connected bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.find(address)
	if d == nil {
		return ErrUnknown
	}
	d.Connected = connected
	return nil
}

// Forget implements Manager.
func (m *Mock) Forget(_ context.Context, address string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, d := range m.devices {
		if d.Address == address {
			m.devices = append(m.devices[:i], m.devices[i+1:]...)
			d.Paired, d.Connected, d.Signal = false, false, -55
			m.nearby = append(m.nearby, d) // it is still in the room
			return nil
		}
	}
	return ErrUnknown
}

func percent(n int) *int { return &n }

// SetBattery changes the charge a device reports; for tests.
func (m *Mock) SetBattery(address string, pct int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d := m.find(address); d != nil {
		d.Battery = &pct
	}
}
