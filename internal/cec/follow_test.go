package cec

import (
	"bytes"
	"sync/atomic"
	"testing"
)

// The Pi is a playback device with address 4 on HDMI 1 (1.0.0.0); the TV is 0.
func newResponder() *responder {
	return &responder{phys: 0x1000, addr: 4, active: new(atomic.Bool)}
}

func answers(t *testing.T, r *responder, msg ...byte) [][]byte {
	t.Helper()
	out, _ := r.handle(msg)
	return out
}

func expect(t *testing.T, what string, got [][]byte, want ...byte) {
	t.Helper()
	if want == nil {
		if len(got) != 0 {
			t.Errorf("%s: sent % x, want nothing", what, got)
		}
		return
	}
	if len(got) != 1 || !bytes.Equal(got[0], want) {
		t.Errorf("%s: sent % x, want % x", what, got, want)
	}
}

var iAmActive = []byte{0x4f, 0x82, 0x10, 0x00} // from 4 to everyone: active source 1.0.0.0

func TestTheTVComesBackToThePi(t *testing.T) {
	r := newResponder()

	// The person opens one of the TV's own apps: the TV becomes the source.
	expect(t, "TV is the source", answers(t, r, 0x0f, 0x82, 0x00, 0x00))
	if r.active.Load() {
		t.Error("still active after the TV took over")
	}
	// Nobody asked the Pi, so it must not pull the TV back to itself.
	expect(t, "who is active, while the TV app runs", answers(t, r, 0x0f, 0x85))

	// Back to the Pi through the TV's device list...
	expect(t, "set stream path to the Pi", answers(t, r, 0x0f, 0x86, 0x10, 0x00), iAmActive...)
	if !r.active.Load() {
		t.Error("not active after the TV selected the Pi")
	}
	// ...or with the input button: routing change from HDMI 2 to HDMI 1.
	r.active.Store(false)
	expect(t, "routing change to the Pi", answers(t, r, 0x0f, 0x80, 0x20, 0x00, 0x10, 0x00), iAmActive...)

	// The TV wakes up and asks who was showing.
	expect(t, "who is active, while the Pi shows", answers(t, r, 0x0f, 0x85), iAmActive...)
}

func TestAnotherInputIsLeftAlone(t *testing.T) {
	r := newResponder()
	r.active.Store(true)
	expect(t, "set stream path to HDMI 2", answers(t, r, 0x0f, 0x86, 0x20, 0x00))
	if r.active.Load() {
		t.Error("still active after the TV selected another input")
	}
	r.active.Store(true)
	expect(t, "routing change away", answers(t, r, 0x0f, 0x80, 0x10, 0x00, 0x30, 0x00))
	expect(t, "another player announces itself", answers(t, r, 0x8f, 0x82, 0x20, 0x00))
	if r.active.Load() {
		t.Error("still active after routing moved away")
	}
	// Too short to carry an address: never treated as "the Pi".
	expect(t, "truncated set stream path", answers(t, r, 0x0f, 0x86, 0x10))
}

func TestQuestionsToThePi(t *testing.T) {
	r := newResponder()
	expect(t, "power status", answers(t, r, 0x04, 0x8f), 0x40, 0x90, 0x00)
	expect(t, "menu request", answers(t, r, 0x04, 0x8d, 0x02), 0x40, 0x8e, 0x00)
	// What PiDash does not understand is declined, as the kernel did before.
	expect(t, "deck status", answers(t, r, 0x04, 0x1a, 0x01), 0x40, 0x00, 0x1a, 0x00)
	expect(t, "vendor command", answers(t, r, 0x04, 0x89, 0x01, 0x02), 0x40, 0x00, 0x89, 0x00)

	// The kernel already dealt with these; a second answer would confuse the TV.
	for _, op := range []byte{0x44, 0x45, 0x46, 0x83, 0x8c, 0x9f, 0xa5, 0xff, 0x00, 0x36, 0x90} {
		expect(t, "kernel's business", answers(t, r, 0x04, op, 0x00))
	}
	// Not for the Pi, a broadcast, or a bare poll.
	expect(t, "for someone else", answers(t, r, 0x05, 0x8f))
	expect(t, "broadcast", answers(t, r, 0x0f, 0x32, 'e', 'n', 'g'))
	expect(t, "poll", answers(t, r, 0x04))
	expect(t, "from nobody", answers(t, r, 0xf4, 0x1a, 0x01))
}

func TestNothingIsSentBeforeThePiIsOnTheBus(t *testing.T) {
	r := newResponder()
	r.addr = laInvalid
	expect(t, "no address yet", answers(t, r, 0x0f, 0x86, 0x10, 0x00))
	r = newResponder()
	r.phys = physInvalid
	expect(t, "no TV connection", answers(t, r, 0x0f, 0x86, 0xff, 0xff))
}
