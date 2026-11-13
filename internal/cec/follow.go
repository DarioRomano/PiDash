package cec

import (
	"fmt"
	"sync/atomic"
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
			return nil, ""
		}
		r.active.Store(true)
		return [][]byte{activeSource}, "the TV switched to the Pi; confirmed it as the active source"
	case opActiveSource:
		if param(2) != r.phys {
			r.active.Store(false)
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

func describe(msg []byte) string {
	if len(msg) < 2 {
		return fmt.Sprintf("poll %x", msg)
	}
	return fmt.Sprintf("from %d to %d: opcode 0x%02x, data % x", msg[0]>>4, msg[0]&0x0f, msg[1], msg[2:])
}
