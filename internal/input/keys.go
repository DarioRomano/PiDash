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
	evAbs = 0x03

	// Axes of a game controller: the left stick and the direction pad.
	absX     = 0x00
	absY     = 0x01
	absHat0X = 0x10
	absHat0Y = 0x11

	// The skip buttons of a remote.
	keyNextSong     = 163
	keyPreviousSong = 165
	keyNext         = 407
	keyPrevious     = 412

	// Buttons of a game controller, named by where they sit: south is the
	// bottom one of the four (A on an Xbox pad, cross on a PlayStation pad).
	btnSouth     = 0x130
	btnEast      = 0x131
	btnTL        = 0x136
	btnTR        = 0x137
	btnPadSelect = 0x13a
	btnStart     = 0x13b
	btnMode      = 0x13c // the button with the logo
	btnDpadUp    = 0x220
	btnDpadDown  = 0x221
	btnDpadLeft  = 0x222
	btnDpadRight = 0x223

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
		// The skip buttons: the next piece of music, or the next photo.
		keyNextSong: "next", keyPreviousSong: "previous", keyNext: "next", keyPrevious: "previous",
		keyInfo:      "help",
		keyChannelUp: "right", keyChannelDn: "left",
		// A game controller: the bottom button chooses, the right one goes
		// back, Start opens the app row, the shoulder buttons change the
		// photo. Its stick and direction pad arrive as arrows (see stick).
		btnSouth: "select", btnEast: "back", btnStart: "menu", btnMode: "home",
		btnTL: "left", btnTR: "right",
		btnDpadUp: "up", btnDpadDown: "down", btnDpadLeft: "left", btnDpadRight: "right",
	}
	for d := 0; d <= 9; d++ {
		m[uint16(keyNumeric0+d)] = string(rune('0' + d))
	}
	return m
}

// DefaultExitKeys leave an app when held: the remote's Back/Exit button and a
// keyboard's Esc.
// The logo button of a game controller is among them: held, it leaves a game
// or an emulator, which have no Back button of their own.
func DefaultExitKeys() []uint16 { return []uint16{keyExit, keyBack, keyEsc, btnMode} }

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

// padKeys are the keyboard keys a game controller presses inside a website:
// a website knows nothing of controllers, but it knows arrows, Enter and
// Escape. The stick and the direction pad arrive here as arrow keys already.
var padKeys = map[uint16]uint16{
	keyUp: keyUp, keyDown: keyDown, keyLeft: keyLeft, keyRight: keyRight,
	btnDpadUp: keyUp, btnDpadDown: keyDown, btnDpadLeft: keyLeft, btnDpadRight: keyRight,
	btnSouth: keyEnter, btnEast: keyEsc, btnStart: keyPlayPause,
}

// volumeStep is +1 for a controller's "up" and -1 for its "down", by the
// direction pad or the stick; 0 for everything else.
func volumeStep(code uint16) int {
	switch padKeys[code] {
	case keyUp:
		return 1
	case keyDown:
		return -1
	}
	return 0
}

// isArrow reports whether a key is one of the four arrows.
func isArrow(code uint16) bool {
	return code == keyUp || code == keyDown || code == keyLeft || code == keyRight
}

// isArrowAction reports whether a dashboard action is a direction.
func isArrowAction(action string) bool {
	return action == "up" || action == "down" || action == "left" || action == "right"
}

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
	"KEY_PREVIOUS": keyPrevious, "KEY_NEXT": keyNext, "KEY_NEXTSONG": keyNextSong, "KEY_PREVIOUSSONG": keyPreviousSong, "KEY_CLEAR": 355, "KEY_SUBTITLE": 370, "KEY_EPG": 365,
	"KEY_UP": keyUp, "KEY_DOWN": keyDown, "KEY_LEFT": keyLeft, "KEY_RIGHT": keyRight,
	"BTN_SOUTH": btnSouth, "BTN_A": btnSouth, "BTN_EAST": btnEast, "BTN_B": btnEast,
	"BTN_NORTH": 0x133, "BTN_X": 0x133, "BTN_WEST": 0x134, "BTN_Y": 0x134,
	"BTN_TL": btnTL, "BTN_TR": btnTR, "BTN_TL2": 0x138, "BTN_TR2": 0x139,
	"BTN_SELECT": btnPadSelect, "BTN_START": btnStart, "BTN_MODE": btnMode,
	"BTN_THUMBL": 0x13d, "BTN_THUMBR": 0x13e,
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

// KeyName is the reverse of ParseKey: the kernel's name for a key code, or
// "key 123" for one without a name here.
func KeyName(code uint16) string {
	best := ""
	for name, c := range keyNames {
		// Some buttons have two names (BTN_A and BTN_SOUTH): always the same one.
		if c == code && (best == "" || name < best) {
			best = name
		}
	}
	if best == "" {
		return fmt.Sprintf("key %d", code)
	}
	return best
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
