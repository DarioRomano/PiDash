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
	// OnElsewhere is called when elsewhere has changed; may be nil.
	OnElsewhere func()
}

// setElsewhere notes what the TV shows, and says so if that changed.
func (p *Pretend) setElsewhere(v bool) {
	p.mu.Lock()
	changed := p.elsewhere != v
	p.elsewhere = v
	fn := p.OnElsewhere
	p.mu.Unlock()
	if changed && fn != nil {
		fn()
	}
}

// Shown reports that the TV shows the Pi: a pretend TV always knows.
func (p *Pretend) Shown() bool { return !p.Elsewhere() }

// Here notes that the TV shows the Pi again, as a button of its remote
// that reaches the Pi tells.
func (p *Pretend) Here() { p.setElsewhere(false) }

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
	p.power = cec.PowerOn
	p.mu.Unlock()
	p.setElsewhere(false)
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
	p.setElsewhere(true)
	return nil
}

// Others implements InputSwitcher.
func (p *Pretend) Others(context.Context) ([]cec.Device, error) {
	return []cec.Device{{Address: "2.0.0.0", Name: "PlayStation 5"}, {Address: "3.0.0.0", Name: ""}}, nil
}
