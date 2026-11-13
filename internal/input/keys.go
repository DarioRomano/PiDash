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
	keyMenu      = 139
	keySetup     = 141
	keyBack      = 158
	keyPlayPause = 164
	keyHomepage  = 172
	keyExit      = 174
	keyPlayCD    = 200
	keyPauseCD   = 201
	keyPlay      = 207
	keyOK        = 352
	keySelect    = 353
	keyInfo      = 358
	keyChannelUp = 402
	keyChannelDn = 403
	keyCtxMenu   = 438
	keyNumeric0  = 512 // KEY_NUMERIC_0 … KEY_NUMERIC_9 follow
)

// DefaultRemoteActions maps the keys a TV remote sends over HDMI-CEC to
// dashboard actions. Only keys a browser does not understand by itself need to
// be here: the kernel reports the remote's OK button as KEY_OK and its
// Back/Exit button as KEY_EXIT, which are not keyboard keys, so without this
// they do nothing. Arrows arrive as ordinary arrow keys and need no help.
func DefaultRemoteActions() map[uint16]string {
	m := map[uint16]string{
		keyOK: "select", keySelect: "select",
		keyExit: "back", keyBack: "back",
		// The arrows would reach the browser as ordinary keys, but only while
		// its window has the keyboard focus, which it can lose (another window
		// opening, the TV being switched off and on). Read here as well, they
		// work regardless; the page drops the copy it gets twice.
		keyUp: "up", keyDown: "down", keyLeft: "left", keyRight: "right",
		keyMenu: "menu", keySetup: "menu", keyCtxMenu: "menu",
		keyHomepage: "home",
		keyPlayCD:   "playpause", keyPauseCD: "playpause", keyPlayPause: "playpause", keyPlay: "playpause",
		keyInfo:      "help",
		keyChannelUp: "right", keyChannelDn: "left",
	}
	for d := 0; d <= 9; d++ {
		m[uint16(keyNumeric0+d)] = string(rune('0' + d))
	}
	return m
}

// DefaultExitKeys leave an app when held: the remote's Back/Exit button and a
// keyboard's Esc.
func DefaultExitKeys() []uint16 { return []uint16{keyExit, keyBack, keyEsc} }

// appKeys translates remote keys an app would not understand into the keyboard
// key that means the same: OK becomes Enter, Exit becomes Escape, and the
// remote's number buttons become the number row.
var appKeys = func() map[uint16]uint16 {
	m := map[uint16]uint16{keyOK: keyEnter, keySelect: keyEnter, keyExit: keyEsc}
	for d := 1; d <= 9; d++ {
		m[uint16(keyNumeric0+d)] = uint16(1 + d) // KEY_1 is 2
	}
	m[keyNumeric0] = 11 // KEY_0
	return m
}()

// keyNames are the keys that can be named in the config as app exit keys.
// Colour and navigation keys are what TV remotes send over HDMI-CEC.
var keyNames = map[string]uint16{
	"KEY_ESC": keyEsc, "KEY_BACKSPACE": keyBackspace, "KEY_HOME": keyHome,
	"KEY_POWER": 116, "KEY_STOP": 128, "KEY_MENU": 139, "KEY_BACK": keyBack,
	"KEY_STOPCD": 166, "KEY_HOMEPAGE": 172, "KEY_EXIT": keyExit, "KEY_CLOSE": 206,
	"KEY_F1": 59, "KEY_F2": 60, "KEY_F3": 61, "KEY_F4": 62, "KEY_F5": 63, "KEY_F6": 64,
	"KEY_F7": 65, "KEY_F8": 66, "KEY_F9": 67, "KEY_F10": 68, "KEY_F11": 87, "KEY_F12": 88,
	"KEY_INFO": 358, "KEY_RED": 398, "KEY_GREEN": 399, "KEY_YELLOW": 400, "KEY_BLUE": 401,
	"KEY_OK": keyOK, "KEY_SELECT": keySelect, "KEY_SETUP": keySetup, "KEY_CONTEXT_MENU": keyCtxMenu,
	"KEY_PLAYCD": keyPlayCD, "KEY_PAUSECD": keyPauseCD, "KEY_PLAYPAUSE": keyPlayPause, "KEY_PLAY": keyPlay,
	"KEY_CHANNELUP": keyChannelUp, "KEY_CHANNELDOWN": keyChannelDn, "KEY_ENTER": keyEnter,
	"KEY_REWIND": 168, "KEY_FASTFORWARD": 208, "KEY_RECORD": 167, "KEY_FAVORITES": 364, "KEY_TV": 377,
	"KEY_PREVIOUS": 412, "KEY_NEXT": 407, "KEY_CLEAR": 355, "KEY_SUBTITLE": 370, "KEY_EPG": 365,
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
	add := func(c uint16) {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, m := range []map[string]uint16{actionKeys, namedKeys} {
		for _, c := range m {
			add(c)
		}
	}
	for _, c := range appKeys {
		add(c)
	}
	return out
}
