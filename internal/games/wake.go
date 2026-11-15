package games

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"
)

// Wake makes sure the PC is awake before a game is started on it.
//
// Moonlight wakes a sleeping PC itself, but gives it half a minute, which a
// PC that was switched off does not manage. So PiDash knocks first: if the
// PC does not answer, it is sent the wake-up call and given up to wait to
// come up. woke reports whether it had to be woken.
//
// A PC whose address or network card Moonlight does not know cannot be
// knocked at or woken from here; that is left to Moonlight.
type Wake struct {
	// Up reports whether the PC answers; nil means a real knock on its door.
	Up func(ctx context.Context, h Host) bool
	// Send sends the wake-up call; nil means the real one.
	Send func(mac net.HardwareAddr, address string) error
	// Every is the time between two knocks; zero means two seconds.
	Every time.Duration
}

// ErrAsleep is returned when the PC did not come up in time.
var ErrAsleep = errors.New("the PC did not wake up. Is it switched on at the wall, connected by cable, and set up to be woken over the network?")

// Do wakes h if it is asleep and waits for it. onWaking is called once if
// the PC has to be woken, so that the person at the TV can be told.
func (w Wake) Do(ctx context.Context, h Host, wait time.Duration, onWaking func()) error {
	up, send, every := w.Up, w.Send, w.Every
	if up == nil {
		up = answers
	}
	if send == nil {
		send = magicPacket
	}
	if every <= 0 {
		every = 2 * time.Second
	}
	if h.Address == "" || up(ctx, h) {
		return nil
	}
	if len(h.MAC) != 6 {
		return nil // nothing to wake it with: Moonlight may know better
	}
	if onWaking != nil {
		onWaking()
	}
	deadline := time.Now().Add(wait)
	for {
		// The call is repeated: a PC in deep sleep can miss the first.
		_ = send(h.MAC, h.Address)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
		if up(ctx, h) {
			return nil
		}
		if time.Now().After(deadline) {
			return ErrAsleep
		}
	}
}

// answers knocks on the door of the PC's streaming service.
func answers(ctx context.Context, h Host) bool {
	d := net.Dialer{Timeout: time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(h.Address, strconv.Itoa(h.Port)))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// MagicPacket is the wake-up call for a network card: six bytes of 0xFF and
// then the card's address sixteen times.
func MagicPacket(mac net.HardwareAddr) []byte {
	packet := make([]byte, 0, 102)
	for i := 0; i < 6; i++ {
		packet = append(packet, 0xff)
	}
	for i := 0; i < 16; i++ {
		packet = append(packet, mac...)
	}
	return packet
}

// magicPacket sends the wake-up call to everyone in the home network, which
// is how a card that is asleep and has no address hears it, and to the
// address the PC last had.
func magicPacket(mac net.HardwareAddr, address string) error {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return err
	}
	defer conn.Close()
	packet := MagicPacket(mac)
	var firstErr error
	targets := []net.IP{net.IPv4bcast}
	if ip := net.ParseIP(address); ip != nil && ip.To4() != nil {
		targets = append(targets, ip)
	}
	for _, ip := range targets {
		for _, port := range []int{9, 7} {
			if _, err := conn.WriteTo(packet, &net.UDPAddr{IP: ip, Port: port}); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
