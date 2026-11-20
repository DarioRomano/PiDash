package cec

import (
	"fmt"
	"sync/atomic"
	"time"
)

// A TV does not just send key presses to whatever is plugged in. It first asks
// the device a few questions, and sends the remote's keys only to the device
// that answered "I am what you are showing". The kernel answers the questions
// about identity (name, address, version) by itself. The ones below are left
// to a program, and without answers many TVs stop forwarding the remote after
// they were switched to another input, or to one of their own apps, and back.

// CEC opcodes used here, from linux/cec.h.
const (
	opFeatureAbort        = 0x00
	opGiveDeckStatus      = 0x1a
	opStandby             = 0x36
	opKeyPressed          = 0x44
	opRoutingChange       = 0x80
	opActiveSource        = 0x82
	opRequestActiveSource = 0x85
	opSetStreamPath       = 0x86
	opMenuRequest         = 0x8d
	opMenuStatus          = 0x8e
	opGivePowerStatus     = 0x8f
	opReportPowerStatus   = 0x90

	laBroadcast   = 0x0f
	laInvalid     = 0xff
	physInvalid   = 0xffff
	powerStatusOn = 0x00
	menuActivated = 0x00
	abortUnknown  = 0x00
)

// noAbort are messages that need no answer from a program: the kernel has
// answered or used them already, or they are answers and announcements.
var noAbort = map[byte]bool{
	opFeatureAbort: true, opStandby: true,
	0x44: true, 0x45: true, // remote-control key pressed, released: the kernel turns them into key events
	0x46: true, 0x83: true, 0x8c: true, 0x9f: true, 0xa5: true, 0xff: true, // answered by the kernel
	0x84: true, 0x87: true, 0xa6: true, // announcements: physical address, vendor, features
	0x32: true, 0x47: true, 0x72: true, 0x7a: true, 0x7e: true, opMenuStatus: true, opReportPowerStatus: true, 0x9e: true, // settings and answers
}

// responder decides what to say to the TV. It holds no file or device, so the
// rules can be tested without a TV.
type responder struct {
	// phys is the Pi's position on the TV's inputs, e.g. 0x1000 for HDMI 1;
	// addr its logical address on the bus. Both come from the adapter.
	phys uint16
	addr byte
	// active is true while the TV is showing the Pi's input, as far as the
	// messages on the bus tell.
	active *atomic.Bool
	// seen notes when the TV says it shows something else; may be nil.
	seen *sight
}

// sight is what the TV has said about what it shows. A TV says when it turns
// to another input, and most say when they turn to their own programme or
// one of their own apps; that it shows the Pi is known when it says so, and
// from every button of its remote that arrives, since those go to what is
// on screen. Before any of that was heard, nothing is known, and nothing is
// assumed.
type sight struct {
	elsewhere atomic.Bool
	// shown is true once it is known that the TV shows the Pi: it said so,
	// a button of its remote reached the Pi, or PiDash had it show the Pi.
	// It is not known after the TV went off, until one of those happens: a
	// TV switched on with its own remote may go straight to one of its own
	// apps without a word that reaches the Pi.
	shown atomic.Bool
	// When it last turned to something else, and when it last came back, in
	// seconds since 1970; zero for never.
	left, back atomic.Int64
	// changed is called when elsewhere has changed; may be nil.
	changed atomic.Pointer[func()]
}

func (s *sight) tell() {
	if fn := s.changed.Load(); fn != nil {
		(*fn)()
	}
}

// away notes that the TV shows something else than the Pi.
func (s *sight) away() {
	if s == nil {
		return
	}
	s.shown.Store(false)
	if !s.elsewhere.Swap(true) {
		s.left.Store(time.Now().Unix())
		s.tell()
	}
}

// here notes that the TV shows the Pi.
func (s *sight) here() {
	if s == nil {
		return
	}
	known := s.shown.Swap(true)
	if s.elsewhere.Swap(false) {
		s.back.Store(time.Now().Unix())
		s.tell()
	} else if !known {
		s.tell()
	}
}

// forget is for when the TV went off: what it shows next is not known.
func (s *sight) forget() {
	if s == nil {
		return
	}
	was := s.shown.Swap(false)
	if s.elsewhere.Swap(false) || was {
		s.tell()
	}
}

// handle takes one received message and returns the messages to send in
// answer, with a short note for the log when something notable happened.
func (r *responder) handle(msg []byte) (out [][]byte, note string) {
	if len(msg) < 2 || r.addr == laInvalid || r.phys == physInvalid {
		return nil, "" // a bare poll, or the adapter is not on the bus yet
	}
	from, to, op := msg[0]>>4, msg[0]&0x0f, msg[1]
	if from == r.addr {
		return nil, ""
	}
	param := func(i int) uint16 { // the 16-bit address starting at byte i
		if len(msg) < i+2 {
			return physInvalid
		}
		return uint16(msg[i])<<8 | uint16(msg[i+1])
	}
	activeSource := []byte{r.addr<<4 | laBroadcast, opActiveSource, byte(r.phys >> 8), byte(r.phys)}

	switch op {
	case opSetStreamPath, opRoutingChange:
		// The TV changed what it shows: by picking a device from its list, or
		// by switching inputs. If that is the Pi, say so; this is the answer
		// TVs wait for before they send the remote's keys here.
		target := param(2)
		if op == opRoutingChange {
			target = param(4) // the message carries the old address, then the new one
		}
		if target != r.phys {
			r.active.Store(false)
			if target != physInvalid {
				r.seen.away()
				return nil, "the TV switched to something else"
			}
			return nil, ""
		}
		r.active.Store(true)
		r.seen.here()
		return [][]byte{activeSource}, "the TV switched to the Pi; confirmed it as the active source"
	case opActiveSource:
		// Another device says it is what the TV shows; a TV says so of
		// itself (address 0.0.0.0) when it turns to its own programme or apps.
		if param(2) != r.phys {
			r.active.Store(false)
			if param(2) != physInvalid {
				r.seen.away()
				return nil, "the TV shows something else now"
			}
		}
		return nil, ""
	case opStandby:
		if from == 0 {
			r.seen.forget()
		}
		return nil, ""
	case opKeyPressed:
		// A button of the TV's remote, sent to the Pi: the TV sends those to
		// what it shows.
		if from == 0 && to == r.addr {
			r.seen.here()
		}
		return nil, ""
	case opRequestActiveSource:
		// "Who is showing?", asked for example when the TV wakes up.
		if r.active.Load() {
			return [][]byte{activeSource}, "the TV asked who is active; answered"
		}
		return nil, ""
	}

	if to == laBroadcast || to != r.addr {
		return nil, ""
	}
	reply := func(b ...byte) [][]byte { return [][]byte{append([]byte{r.addr<<4 | from}, b...)} }
	switch op {
	case opGivePowerStatus:
		return reply(opReportPowerStatus, powerStatusOn), ""
	case opMenuRequest:
		// Older TVs ask whether the device shows a menu before they route
		// the arrow keys to it. PiDash always does.
		return reply(opMenuStatus, menuActivated), ""
	}
	if noAbort[op] || from == laBroadcast {
		return nil, ""
	}
	// With a program listening the kernel no longer declines what nobody
	// understands; do it here, as the standard asks.
	return reply(opFeatureAbort, op, abortUnknown), ""
}

// tvSignal reads what a message says about the TV's power: standby when the
// TV announces it is switching off, active for anything else it sends, since
// a TV in standby is silent.
func tvSignal(msg []byte) (standby, active bool) {
	if len(msg) < 2 || msg[0]>>4 != 0 {
		return false, false // a bare poll, or not from the TV
	}
	if msg[1] == opStandby {
		return true, false
	}
	return false, true
}

func describe(msg []byte) string {
	if len(msg) < 2 {
		return fmt.Sprintf("poll %x", msg)
	}
	return fmt.Sprintf("from %d to %d: opcode 0x%02x, data % x", msg[0]>>4, msg[0]&0x0f, msg[1], msg[2:])
}
