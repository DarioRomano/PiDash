package cec

import (
	"bytes"
	"sync/atomic"
	"testing"
)

// The Pi is a playback device with address 4 on HDMI 1 (1.0.0.0); the TV is 0.
func newResponder() *responder {
	return &responder{phys: 0x1000, addr: 4, active: new(atomic.Bool), seen: new(sight)}
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

func TestWhatTheTVSaysAboutItsPower(t *testing.T) {
	for name, c := range map[string]struct {
		msg             []byte
		standby, active bool
	}{
		"standby to everyone":      {[]byte{0x0f, 0x36}, true, false},
		"standby to the Pi":        {[]byte{0x04, 0x36}, true, false},
		"the TV selects an input":  {[]byte{0x0f, 0x86, 0x10, 0x00}, false, true},
		"a button of its remote":   {[]byte{0x04, 0x44, 0x00}, false, true},
		"another player's standby": {[]byte{0x8f, 0x36}, false, false},
		"a bare poll":              {[]byte{0x04}, false, false},
	} {
		if standby, active := tvSignal(c.msg); standby != c.standby || active != c.active {
			t.Errorf("%s: standby %v, active %v; want %v, %v", name, standby, active, c.standby, c.active)
		}
	}
}

func TestWhatTheTVSaysItShows(t *testing.T) {
	r := newResponder()
	elsewhere := func() bool { return r.seen.elsewhere.Load() }
	// Before the TV has said anything, nothing is assumed.
	if elsewhere() {
		t.Fatal("elsewhere before anything was heard")
	}
	for _, c := range []struct {
		what string
		msg  []byte
		want bool
	}{
		{"another input is chosen with the TV's remote", []byte{0x0f, 0x86, 0x20, 0x00}, true},
		{"the Pi's input is chosen", []byte{0x0f, 0x86, 0x10, 0x00}, false},
		{"the TV turns to its own apps, and says so", []byte{0x0f, 0x82, 0x00, 0x00}, true},
		{"a button of its remote arrives all the same", []byte{0x04, 0x44, 0x01}, false},
		{"the inputs are switched away from the Pi", []byte{0x0f, 0x80, 0x10, 0x00, 0x00, 0x00}, true},
		{"and back to it", []byte{0x0f, 0x80, 0x00, 0x00, 0x10, 0x00}, false},
		{"a console switches itself on and takes the screen", []byte{0x8f, 0x82, 0x30, 0x00}, true},
		{"a button meant for another device says nothing", []byte{0x05, 0x44, 0x01}, true},
		{"a message cut short says nothing", []byte{0x0f, 0x86, 0x10}, true},
		{"the TV goes off: what it shows next is not known", []byte{0x0f, 0x36}, false},
		{"a message cut short, again", []byte{0x0f, 0x82, 0x20}, false},
	} {
		r.handle(c.msg)
		if elsewhere() != c.want {
			t.Fatalf("%s: elsewhere = %v, want %v", c.what, elsewhere(), c.want)
		}
	}
	// When it last left and came back is kept, for the status page.
	if r.seen.left.Load() == 0 || r.seen.back.Load() == 0 {
		t.Errorf("times not kept: left %d, back %d", r.seen.left.Load(), r.seen.back.Load())
	}
	// A responder without anyone to tell still answers.
	bare := &responder{phys: 0x1000, addr: 4, active: new(atomic.Bool)}
	if out, _ := bare.handle([]byte{0x0f, 0x86, 0x10, 0x00}); len(out) != 1 {
		t.Errorf("without a place to note it: %v", out)
	}
	bare.handle([]byte{0x0f, 0x86, 0x20, 0x00})
}
