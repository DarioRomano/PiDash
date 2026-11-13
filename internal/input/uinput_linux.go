package input

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// deviceName identifies the virtual device, also so the remote watcher can
// ignore the keys PiDash presses itself.
const deviceName = "PiDash phone remote"

const uinputPath = "/dev/uinput"

// ioctl numbers from linux/uinput.h.
const (
	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
	uiSetEvBit   = 0x40045564
	uiSetKeyBit  = 0x40045565
	uiSetRelBit  = 0x40045566
)

// timevalSize is the size of the timestamp at the start of every input event:
// 16 bytes on 64-bit systems, 8 on 32-bit ones.
const timevalSize = int(unsafe.Sizeof(syscall.Timeval{}))

// encodeEvent builds one struct input_event. The kernel fills in the time.
func encodeEvent(typ, code uint16, value int32) []byte {
	b := make([]byte, timevalSize+8)
	binary.LittleEndian.PutUint16(b[timevalSize:], typ)
	binary.LittleEndian.PutUint16(b[timevalSize+2:], code)
	binary.LittleEndian.PutUint32(b[timevalSize+4:], uint32(value))
	return b
}

// virtualDevice is a mouse and a small keyboard created through /dev/uinput.
// To the desktop it looks like real hardware, so it works in every app, on
// Wayland and X11 alike, and in apps running outside the desktop.
type virtualDevice struct {
	mu sync.Mutex
	fd int
}

// uinputProblem explains why the virtual device cannot be used, or returns nil.
func uinputProblem() error {
	err := syscall.Access(uinputPath, 2) // W_OK
	switch {
	case err == nil:
		return nil
	case errors.Is(err, syscall.ENOENT):
		return errors.New("this system has no /dev/uinput, so keys cannot be passed on to apps")
	case errors.Is(err, syscall.EACCES):
		return errors.New("PiDash is not allowed to create a virtual keyboard yet: run the installer again (bash ~/.local/share/pidash/update.sh)")
	}
	return fmt.Errorf("cannot use /dev/uinput: %w", err)
}

func ioctl(fd int, req, arg uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, arg); errno != 0 {
		return errno
	}
	return nil
}

func openVirtualDevice() (*virtualDevice, error) {
	if err := uinputProblem(); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(uinputPath, syscall.O_WRONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", uinputPath, err)
	}
	fail := func(what string, err error) (*virtualDevice, error) {
		syscall.Close(fd)
		return nil, fmt.Errorf("set up virtual mouse (%s): %w", what, err)
	}

	for _, ev := range []uintptr{evKey, evRel} {
		if err := ioctl(fd, uiSetEvBit, ev); err != nil {
			return fail("event types", err)
		}
	}
	for _, rel := range []uintptr{relX, relY, relWheel, relHWheel} {
		if err := ioctl(fd, uiSetRelBit, rel); err != nil {
			return fail("axes", err)
		}
	}
	keys := append([]uint16{btnLeft, btnRight, btnMiddle}, allKeys()...)
	for _, k := range keys {
		if err := ioctl(fd, uiSetKeyBit, uintptr(k)); err != nil {
			return fail("keys", err)
		}
	}

	// struct uinput_user_dev: name[80], input_id{bustype, vendor, product,
	// version}, ff_effects_max, then four int32[64] arrays for absolute axes.
	const userDevSize = 80 + 8 + 4 + 4*64*4
	dev := make([]byte, userDevSize)
	copy(dev, deviceName)
	binary.LittleEndian.PutUint16(dev[80:], 0x06)   // BUS_VIRTUAL
	binary.LittleEndian.PutUint16(dev[82:], 0x1d50) // an open-hardware vendor id
	binary.LittleEndian.PutUint16(dev[84:], 0x7064)
	binary.LittleEndian.PutUint16(dev[86:], 1)
	if _, err := syscall.Write(fd, dev); err != nil {
		return fail("describe device", err)
	}
	if err := ioctl(fd, uiDevCreate, 0); err != nil {
		return fail("create device", err)
	}
	return &virtualDevice{fd: fd}, nil
}

func (d *virtualDevice) write(events ...[]byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var buf []byte
	for _, e := range events {
		buf = append(buf, e...)
	}
	buf = append(buf, encodeEvent(evSyn, 0, 0)...)
	_, err := syscall.Write(d.fd, buf)
	return err
}

func (d *virtualDevice) move(dx, dy int) error {
	var ev [][]byte
	if dx != 0 {
		ev = append(ev, encodeEvent(evRel, relX, int32(dx)))
	}
	if dy != 0 {
		ev = append(ev, encodeEvent(evRel, relY, int32(dy)))
	}
	if len(ev) == 0 {
		return nil
	}
	return d.write(ev...)
}

func (d *virtualDevice) scroll(dx, dy int) error {
	var ev [][]byte
	if dy != 0 {
		ev = append(ev, encodeEvent(evRel, relWheel, int32(dy)))
	}
	if dx != 0 {
		ev = append(ev, encodeEvent(evRel, relHWheel, int32(dx)))
	}
	if len(ev) == 0 {
		return nil
	}
	return d.write(ev...)
}

func (d *virtualDevice) key(code uint16, down bool) error {
	v := int32(0)
	if down {
		v = 1
	}
	return d.write(encodeEvent(evKey, code, v))
}

func (d *virtualDevice) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	_ = ioctl(d.fd, uiDevDestroy, 0)
	syscall.Close(d.fd)
}

// --- Listening to real input devices (the TV remote) ---------------------------

// ioctl request builders from asm-generic/ioctl.h: read direction, type 'E'.
func eviocgname(length int) uintptr { return 2<<30 | uintptr(length)<<16 | 'E'<<8 | 0x06 }
func eviocgbit(ev, length int) uintptr {
	return 2<<30 | uintptr(length)<<16 | 'E'<<8 | uintptr(0x20+ev)
}

// deviceLabel returns the name an input device gives itself, such as the
// TV's name for a remote that arrives over HDMI.
func deviceLabel(f *os.File) string {
	raw, err := f.SyscallConn()
	if err != nil {
		return ""
	}
	var name [128]byte
	var nameErr error
	if raw.Control(func(fd uintptr) {
		nameErr = ioctl(int(fd), eviocgname(len(name)), uintptr(unsafe.Pointer(&name[0])))
	}) != nil || nameErr != nil {
		return ""
	}
	return cString(name[:])
}

// eviocgabs asks for the range of one axis (struct input_absinfo, 24 bytes).
func eviocgabs(axis int) uintptr { return 2<<30 | 24<<16 | 'E'<<8 | uintptr(0x40+axis) }

// padAxes reports whether the device is a game controller and, if so, the
// ranges of its left stick and direction pad. Touchpads and tablets have
// axes too, but none of them has a controller's buttons.
func padAxes(f *os.File) (axes map[uint16][2]int32, isPad bool) {
	raw, err := f.SyscallConn()
	if err != nil {
		return nil, false
	}
	out := map[uint16][2]int32{}
	_ = raw.Control(func(fd uintptr) {
		bits := make([]byte, 0x300/8)
		if ioctl(int(fd), eviocgbit(evKey, len(bits)), uintptr(unsafe.Pointer(&bits[0]))) != nil || bits[btnSouth/8]&(1<<(btnSouth%8)) == 0 {
			return
		}
		isPad = true
		for _, axis := range []uint16{absX, absY, absHat0X, absHat0Y} {
			var info [6]int32 // value, minimum, maximum, fuzz, flat, resolution
			if ioctl(int(fd), eviocgabs(int(axis)), uintptr(unsafe.Pointer(&info[0]))) == nil && info[2] > info[1] {
				out[axis] = [2]int32{info[1], info[2]}
			}
		}
	})
	return out, isPad
}

// openRemote opens an input device if it can send any of the wanted keys and
// is not PiDash's own virtual device.
func openRemote(path string, wanted []uint16) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	var name [128]byte
	bits := make([]byte, 0x300/8)
	var nameErr, bitsErr error
	raw, err := f.SyscallConn()
	if err == nil {
		err = raw.Control(func(fd uintptr) {
			nameErr = ioctl(int(fd), eviocgname(len(name)), uintptr(unsafe.Pointer(&name[0])))
			bitsErr = ioctl(int(fd), eviocgbit(evKey, len(bits)), uintptr(unsafe.Pointer(&bits[0])))
		})
	}
	if err != nil || bitsErr != nil {
		f.Close()
		return nil, errNotARemote
	}
	if nameErr == nil && cString(name[:]) == deviceName {
		f.Close()
		return nil, errNotARemote
	}
	for _, k := range wanted {
		if int(k/8) < len(bits) && bits[k/8]&(1<<(k%8)) != 0 {
			return f, nil
		}
	}
	f.Close()
	return nil, errNotARemote
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// eventSize is the size of one struct input_event.
const eventSize = timevalSize + 8

// decodeEvent reads the type, code and value from one input event.
func decodeEvent(b []byte) (typ, code uint16, value int32) {
	return binary.LittleEndian.Uint16(b[timevalSize:]),
		binary.LittleEndian.Uint16(b[timevalSize+2:]),
		int32(binary.LittleEndian.Uint32(b[timevalSize+4:]))
}
