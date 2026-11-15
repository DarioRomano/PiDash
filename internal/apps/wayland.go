package apps

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"
)

// Giving a window the keyboard on a Wayland desktop.
//
// Desktops built on wlroots, such as Raspberry Pi OS's labwc, let a program
// list the open windows and bring one to the front, through an extension
// called "foreign toplevel management". PiDash speaks it itself: there is a
// small tool for it (wlrctl), but Raspberry Pi OS does not have that to
// install. The conversation is short: ask what the desktop offers, ask for
// the windows, and tell the one that is wanted to take the keyboard.

// errNoWindowControl is returned on a desktop that does not offer the extension.
var errNoWindowControl = errors.New("this desktop does not let programs choose the window that has the keyboard")

const (
	wlManagerName = "zwlr_foreign_toplevel_manager_v1"
	wlSeatName    = "wl_seat"
	// The object every conversation starts with.
	wlDisplay = 1
	// A window's state "it has the keyboard".
	wlStateActivated = 2
)

// waylandSocket finds the desktop's socket from the session's environment.
func waylandSocket(env []string) (string, bool) {
	display, runtime := "", ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "WAYLAND_DISPLAY="); ok {
			display = v
		}
		if v, ok := strings.CutPrefix(kv, "XDG_RUNTIME_DIR="); ok {
			runtime = v
		}
	}
	switch {
	case display == "":
		return "", false
	case filepath.IsAbs(display):
		return display, true
	case runtime == "":
		return "", false
	}
	return filepath.Join(runtime, display), true
}

// wlConn is one conversation with the desktop.
type wlConn struct {
	c      net.Conn
	nextID uint32
}

// wlMessage is one message as it arrives: who it is from, what it says.
type wlMessage struct {
	object uint32
	opcode uint16
	body   []byte
}

func (w *wlConn) newID() uint32 {
	w.nextID++
	return w.nextID
}

// Numbers travel in the machine's own byte order; every machine PiDash runs
// on has the low byte first.
var wlOrder = binary.LittleEndian

func wlUint(v uint32) []byte { return wlOrder.AppendUint32(nil, v) }

// wlString is a text as the protocol writes it: its length with the closing
// zero, the text, and zeros up to a multiple of four.
func wlString(s string) []byte {
	out := wlUint(uint32(len(s) + 1))
	out = append(out, s...)
	out = append(out, 0)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	return out
}

func (w *wlConn) send(object uint32, opcode uint16, args ...[]byte) error {
	size := 8
	for _, a := range args {
		size += len(a)
	}
	msg := make([]byte, 0, size)
	msg = wlOrder.AppendUint32(msg, object)
	msg = wlOrder.AppendUint32(msg, uint32(size)<<16|uint32(opcode))
	for _, a := range args {
		msg = append(msg, a...)
	}
	_, err := w.c.Write(msg)
	return err
}

func (w *wlConn) read() (wlMessage, error) {
	var head [8]byte
	if _, err := io.ReadFull(w.c, head[:]); err != nil {
		return wlMessage{}, err
	}
	word := wlOrder.Uint32(head[4:])
	size := int(word >> 16)
	if size < 8 || size%4 != 0 {
		return wlMessage{}, fmt.Errorf("the desktop sent a message of %d bytes", size)
	}
	m := wlMessage{object: wlOrder.Uint32(head[:4]), opcode: uint16(word), body: make([]byte, size-8)}
	_, err := io.ReadFull(w.c, m.body)
	return m, err
}

// readString takes a text off the front of a message's body.
func readString(body []byte) (string, []byte, bool) {
	if len(body) < 4 {
		return "", nil, false
	}
	n := int(wlOrder.Uint32(body))
	padded := (n + 3) &^ 3
	if n < 1 || 4+padded > len(body) {
		return "", nil, false
	}
	return string(body[4 : 4+n-1]), body[4+padded:], true
}

// roundTrip asks the desktop to say when it has dealt with everything sent
// so far, and hands every message that arrives until then to handle.
func (w *wlConn) roundTrip(handle func(wlMessage)) error {
	done := w.newID()
	if err := w.send(wlDisplay, 0, wlUint(done)); err != nil { // wl_display.sync
		return err
	}
	for {
		m, err := w.read()
		if err != nil {
			return err
		}
		switch {
		case m.object == done:
			return nil
		case m.object == wlDisplay && m.opcode == 0: // wl_display.error
			text := "unknown"
			if len(m.body) >= 8 {
				if s, _, ok := readString(m.body[8:]); ok {
					text = s
				}
			}
			return fmt.Errorf("the desktop refused: %s", text)
		case m.object == wlDisplay:
			// delete_id: an id is free again; none is used twice here.
		default:
			handle(m)
		}
	}
}

// wlWindow looks for the window with the given app id. It reports whether
// it exists and whether it has the keyboard; with give, a window that does
// not have it is told to take it.
func wlWindow(ctx context.Context, env []string, window string, give bool) (exists, hadIt bool, err error) {
	path, ok := waylandSocket(env)
	if !ok {
		return false, false, errors.New("no Wayland desktop in this session")
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return false, false, err
	}
	defer c.Close()
	deadline := time.Now().Add(3 * time.Second)
	if t, ok := ctx.Deadline(); ok && t.Before(deadline) {
		deadline = t
	}
	_ = c.SetDeadline(deadline)
	w := &wlConn{c: c, nextID: wlDisplay}

	// What does the desktop offer?
	registry := w.newID()
	if err := w.send(wlDisplay, 1, wlUint(registry)); err != nil { // wl_display.get_registry
		return false, false, err
	}
	type global struct{ name, version uint32 }
	var seat, manager *global
	err = w.roundTrip(func(m wlMessage) {
		if m.object != registry || m.opcode != 0 || len(m.body) < 4 { // wl_registry.global
			return
		}
		name := wlOrder.Uint32(m.body)
		iface, rest, ok := readString(m.body[4:])
		if !ok || len(rest) < 4 {
			return
		}
		g := &global{name, wlOrder.Uint32(rest)}
		switch {
		case iface == wlSeatName && seat == nil:
			seat = g
		case iface == wlManagerName:
			manager = g
		}
	})
	if err != nil {
		return false, false, err
	}
	if manager == nil || seat == nil {
		return false, false, errNoWindowControl
	}

	// The windows: the desktop names each one and says what state it is in.
	bind := func(g *global, iface string, version uint32) (uint32, error) {
		id := w.newID()
		return id, w.send(registry, 0, wlUint(g.name), wlString(iface), wlUint(version), wlUint(id)) // wl_registry.bind
	}
	seatID, err := bind(seat, wlSeatName, 1)
	if err != nil {
		return false, false, err
	}
	managerID, err := bind(manager, wlManagerName, min(manager.version, 3))
	if err != nil {
		return false, false, err
	}
	type toplevel struct {
		appID     string
		activated bool
		closed    bool
	}
	windows := map[uint32]*toplevel{}
	var order []uint32
	err = w.roundTrip(func(m wlMessage) {
		if m.object == managerID {
			if m.opcode == 0 && len(m.body) >= 4 { // toplevel: a window, as a new object
				id := wlOrder.Uint32(m.body)
				windows[id] = &toplevel{}
				order = append(order, id)
			}
			return
		}
		t := windows[m.object]
		if t == nil {
			return
		}
		switch m.opcode {
		case 1: // app_id
			if s, _, ok := readString(m.body); ok {
				t.appID = s
			}
		case 4: // state: a list of numbers
			t.activated = false
			if len(m.body) >= 4 {
				n := int(wlOrder.Uint32(m.body))
				for i := 0; i+4 <= n && 4+i+4 <= len(m.body); i += 4 {
					if wlOrder.Uint32(m.body[4+i:]) == wlStateActivated {
						t.activated = true
					}
				}
			}
		case 6: // closed
			t.closed = true
		}
	})
	if err != nil {
		return false, false, err
	}

	var first uint32
	for _, id := range order {
		t := windows[id]
		if t.closed || t.appID != window {
			continue
		}
		exists = true
		if t.activated {
			return true, true, nil
		}
		if first == 0 {
			first = id
		}
	}
	if !exists || !give {
		return exists, false, nil
	}
	if err := w.send(first, 4, wlUint(seatID)); err != nil { // toplevel.activate
		return true, false, err
	}
	// Wait until the desktop has heard it: closing at once could lose it.
	return true, false, w.roundTrip(func(wlMessage) {})
}
