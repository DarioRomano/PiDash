// Package input gives the phone remote a virtual mouse and keyboard, and
// listens to the TV remote directly so a long press of Back can close an app
// that has the screen.
package input

import (
	"fmt"
	"strconv"
	"strings"
)

// Linux input event types and codes (from linux/input-event-codes.h).
const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02

	relX      = 0x00
	relY      = 0x01
	relHWheel = 0x06
	relWheel  = 0x08

	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112

	keyEsc       = 1
	keyBackspace = 14
	keyTab       = 15
	keyEnter     = 28
	keySpace     = 57
	keyHome      = 102
	keyUp        = 103
	keyLeft      = 105
	keyRight     = 106
	keyDown      = 108
	keyCompose   = 127
	keyBack      = 158
	keyPlayPause = 164
	keyExit      = 174
)

// keyNames are the keys that can be named in the config as app exit keys.
// Colour and navigation keys are what TV remotes send over HDMI-CEC.
var keyNames = map[string]uint16{
	"KEY_ESC": keyEsc, "KEY_BACKSPACE": keyBackspace, "KEY_HOME": keyHome,
	"KEY_POWER": 116, "KEY_STOP": 128, "KEY_MENU": 139, "KEY_BACK": keyBack,
	"KEY_STOPCD": 166, "KEY_HOMEPAGE": 172, "KEY_EXIT": keyExit, "KEY_CLOSE": 206,
	"KEY_F1": 59, "KEY_F2": 60, "KEY_F3": 61, "KEY_F4": 62, "KEY_F5": 63, "KEY_F6": 64,
	"KEY_F7": 65, "KEY_F8": 66, "KEY_F9": 67, "KEY_F10": 68, "KEY_F11": 87, "KEY_F12": 88,
	"KEY_INFO": 358, "KEY_RED": 398, "KEY_GREEN": 399, "KEY_YELLOW": 400, "KEY_BLUE": 401,
}

// ParseKey turns a key name such as "KEY_EXIT", or a numeric key code, into
// a Linux key code.
func ParseKey(s string) (uint16, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if code, ok := keyNames[s]; ok {
		return code, nil
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 && n < 0x300 {
		return uint16(n), nil
	}
	return 0, fmt.Errorf("unknown key %q (use a name like KEY_EXIT or a numeric key code)", s)
}

// actionKeys maps dashboard actions to the key the phone remote presses on the
// virtual keyboard while an app has the screen.
var actionKeys = map[string]uint16{
	"up": keyUp, "down": keyDown, "left": keyLeft, "right": keyRight,
	"select": keyEnter, "back": keyEsc, "home": keyHome, "menu": keyCompose, "playpause": keyPlayPause,
	"1": 2, "2": 3, "3": 4, "4": 5, "5": 6, "6": 7, "7": 8, "8": 9, "9": 10, "0": 11,
}

// namedKeys are the extra keys offered next to the phone's touchpad.
var namedKeys = map[string]uint16{
	"enter": keyEnter, "backspace": keyBackspace, "tab": keyTab, "escape": keyEsc, "space": keySpace,
}

// allKeys lists every key the virtual keyboard can press.
func allKeys() []uint16 {
	seen := map[uint16]bool{}
	var out []uint16
	for _, m := range []map[string]uint16{actionKeys, namedKeys} {
		for _, c := range m {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}
