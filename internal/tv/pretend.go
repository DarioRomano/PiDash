package tv

import (
	"context"
	"strings"
	"sync"

	"github.com/DarioRomano/PiDash/internal/cec"
)

// Pretend is a TV for development: it is always there, does what it is
// told, and remembers it.
type Pretend struct {
	mu    sync.Mutex
	power cec.Power
	did   []string
	// elsewhere is true after it was told to show another device.
	elsewhere bool
}

// Elsewhere reports whether the TV shows something other than the Pi.
func (p *Pretend) Elsewhere() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.elsewhere
}

func (p *Pretend) note(what string) {
	p.mu.Lock()
	p.did = append(p.did, what)
	p.mu.Unlock()
}

// Did says what the TV was told, one thing a line.
func (p *Pretend) Did() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.did, "\n")
}

// Available implements TV.
func (p *Pretend) Available() bool { return true }

// Standby implements TV.
func (p *Pretend) Standby(context.Context) error {
	p.note("standby")
	p.mu.Lock()
	p.power = cec.PowerStandby
	p.mu.Unlock()
	return nil
}

// Wake implements TV.
func (p *Pretend) Wake(context.Context) error {
	p.note("wake")
	p.mu.Lock()
	p.power, p.elsewhere = cec.PowerOn, false
	p.mu.Unlock()
	return nil
}

// PowerStatus implements TV.
func (p *Pretend) PowerStatus(context.Context) (cec.Power, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.power == "" {
		return cec.PowerOn, nil
	}
	return p.power, nil
}

// Show implements InputSwitcher.
func (p *Pretend) Show(_ context.Context, address string) error {
	p.note("show " + address)
	p.mu.Lock()
	p.elsewhere = true
	p.mu.Unlock()
	return nil
}

// Others implements InputSwitcher.
func (p *Pretend) Others(context.Context) ([]cec.Device, error) {
	return []cec.Device{{Address: "2.0.0.0", Name: "PlayStation 5"}, {Address: "3.0.0.0", Name: ""}}, nil
}
