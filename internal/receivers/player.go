package receivers

import (
	"context"
	"errors"

	"github.com/DarioRomano/PiDash/internal/playing"
)

// Player is what the dashboard asks about music: this is the part of it
// that the players on the Pi itself answer.
type Player interface {
	Current() *playing.Track
	Control(ctx context.Context, button string) error
	ArtFile() (string, bool)
}

// ErrOnThePhone is returned for a button press on music that a phone plays:
// the phone has the buttons.
var ErrOnThePhone = errors.New("this plays from a phone; its buttons are there")

// Both answers for the players on the Pi and for what phones play on it.
// What a phone plays comes first: someone has just asked for it.
type Both struct {
	Speakers *Service
	Others   Player // may be nil
}

func (b Both) fromPhone() bool {
	t := b.Speakers.Current()
	return t != nil && (t.Playing || b.Others == nil || b.Others.Current() == nil)
}

// Current implements Player.
func (b Both) Current() *playing.Track {
	if b.fromPhone() {
		return b.Speakers.Current()
	}
	if b.Others == nil {
		return nil
	}
	return b.Others.Current()
}

// Control implements Player.
func (b Both) Control(ctx context.Context, button string) error {
	if b.fromPhone() {
		return ErrOnThePhone
	}
	if b.Others == nil {
		return playing.ErrNothing
	}
	return b.Others.Control(ctx, button)
}

// ArtFile implements Player.
func (b Both) ArtFile() (string, bool) {
	if b.fromPhone() {
		return b.Speakers.ArtFile()
	}
	if b.Others == nil {
		return "", false
	}
	return b.Others.ArtFile()
}
