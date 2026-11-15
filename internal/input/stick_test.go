package input

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGameControllerButtonsOnTheDashboard(t *testing.T) {
	h := newRemoteHarness(time.Hour)
	for _, b := range []uint16{btnSouth, btnEast, btnStart, btnMode, btnTL, btnTR, btnDpadUp, btnDpadDown, btnDpadLeft, btnDpadRight} {
		h.press(b)
	}
	h.press(0x133) // a button without a meaning on the dashboard

	actions, taps, exits := h.snapshot()
	want := "select,back,menu,home,left,right,up,down,left,right"
	if got := strings.Join(actions, ","); got != want {
		t.Errorf("dashboard actions = %s, want %s", got, want)
	}
	if len(taps) != 0 || exits != 0 {
		t.Errorf("nothing should be typed or closed: taps=%v exits=%d", taps, exits)
	}
}

func TestGameControllerIsLeftToTheGame(t *testing.T) {
	h := newRemoteHarness(time.Hour)
	h.appRunning = true
	// A game reads the controller itself: neither the dashboard nor the
	// virtual keyboard may react to its buttons.
	for _, b := range []uint16{btnSouth, btnEast, btnStart, btnTL, btnDpadUp} {
		h.press(b)
	}
	actions, taps, exits := h.snapshot()
	if len(actions) != 0 || len(taps) != 0 || exits != 0 {
		t.Errorf("actions=%v taps=%v exits=%d, want nothing", actions, taps, exits)
	}
}

func TestHoldingTheLogoButtonLeavesTheGame(t *testing.T) {
	h := newRemoteHarness(60 * time.Millisecond)
	h.appRunning = true
	h.key("pad", btnMode, 1)
	time.Sleep(200 * time.Millisecond)
	h.key("pad", btnMode, 0)
	if _, _, exits := h.snapshot(); exits != 1 {
		t.Errorf("app closed %d times, want once", exits)
	}

	// A short press inside the game is the game's business.
	h.press(btnMode)
	if _, _, exits := h.snapshot(); exits != 1 {
		t.Errorf("a short press must not close the game (closed %d times)", exits)
	}
}

func TestDirectionPadBecomesArrows(t *testing.T) {
	s := newStick(map[uint16][2]int32{absHat0X: {-1, 1}, absHat0Y: {-1, 1}})
	steps := []struct {
		axis           uint16
		value          int32
		release, press uint16
	}{
		{absHat0X, 1, 0, keyRight},
		{absHat0X, 1, 0, 0}, // reported again: still one press
		{absHat0X, 0, keyRight, 0},
		{absHat0X, -1, 0, keyLeft},
		{absHat0X, 1, keyLeft, keyRight}, // straight across
		{absHat0Y, -1, 0, keyUp},         // the other axis is independent
		{absHat0Y, 0, keyUp, 0},
		{absHat0Y, 1, 0, keyDown},
	}
	for i, st := range steps {
		release, press := s.move(st.axis, st.value)
		if release != st.release || press != st.press {
			t.Errorf("step %d: release=%d press=%d, want %d and %d", i, release, press, st.release, st.press)
		}
	}
}

func TestStickBecomesArrowsWithoutFlicker(t *testing.T) {
	// The two common ranges: signed 16 bit (Xbox pads) and 0…255 (PlayStation pads).
	for name, r := range map[string][2]int32{"signed": {-32768, 32767}, "byte": {0, 255}} {
		s := newStick(map[uint16][2]int32{absX: r, absY: r})
		at := func(pos float64) int32 { // pos is -1 … 1
			return int32(float64(r[0]+r[1])/2 + pos*float64(r[1]-r[0])/2)
		}
		steps := []struct {
			axis           uint16
			pos            float64
			release, press uint16
		}{
			{absX, 0.08, 0, 0},       // resting a little off centre
			{absX, 0.5, 0, 0},        // not far enough yet
			{absX, 0.7, 0, keyRight}, // pushed
			{absX, 0.55, 0, 0},       // wobbling at the edge: still held
			{absX, 0.95, 0, 0},
			{absX, 0.4, 0, 0}, // on the way back: still held
			{absX, 0.1, keyRight, 0},
			{absX, -0.9, 0, keyLeft},
			{absY, -0.9, 0, keyUp}, // up is the low end of the axis
			{absY, 0.9, keyUp, keyDown},
			{absY, 0, keyDown, 0},
		}
		for i, st := range steps {
			release, press := s.move(st.axis, at(st.pos))
			if release != st.release || press != st.press {
				t.Errorf("%s, step %d (%.2f): release=%d press=%d, want %d and %d", name, i, st.pos, release, press, st.release, st.press)
			}
		}
	}
}

func TestOtherDevicesHaveNoStick(t *testing.T) {
	var s *stick = newStick(nil)
	if s != nil {
		t.Fatal("a device without controller axes must not get a stick")
	}
	if release, press := s.move(absX, 30000); release != 0 || press != 0 {
		t.Errorf("a touchpad's position turned into keys: %d %d", release, press)
	}
	// An axis the controller did not announce (a trigger, the right stick) is ignored.
	pad := newStick(map[uint16][2]int32{absX: {0, 255}})
	if release, press := pad.move(0x05, 255); release != 0 || press != 0 {
		t.Errorf("an unknown axis turned into keys: %d %d", release, press)
	}
}

func TestControllerButtonsCanBeNamedInTheConfig(t *testing.T) {
	for name, want := range map[string]uint16{"BTN_SOUTH": btnSouth, "BTN_A": btnSouth, "BTN_MODE": btnMode, "BTN_START": btnStart, "KEY_UP": keyUp} {
		got, err := ParseKey(name)
		if err != nil || got != want {
			t.Errorf("ParseKey(%s) = %d, %v; want %d", name, got, err, want)
		}
	}
}

func TestSwitchingAControllerOnAsksForTheTV(t *testing.T) {
	h := newRemoteHarness(time.Hour)
	var woken, padActions []string
	h.OnPadWake = func(name string) { woken = append(woken, name) }
	h.OnPadAction = func(a string) { padActions = append(padActions, a) }
	h.listening("/dev/input/event7", "Xbox Wireless Controller", true, true)
	h.listening("/dev/input/event2", "LG TV", false, true)

	// The logo button says "I want to play": on the dashboard and in a game alike.
	h.key("/dev/input/event7", btnMode, 1)
	h.key("/dev/input/event7", btnMode, 0)
	h.appRunning = true
	h.key("/dev/input/event7", btnMode, 1)
	h.key("/dev/input/event7", btnMode, 0)
	h.appRunning = false
	if strings.Join(woken, "|") != "Xbox Wireless Controller|Xbox Wireless Controller" {
		t.Errorf("asked to wake the TV for %q, want twice for the controller", woken)
	}

	// Other buttons do not, and neither does the same key on something that
	// is not a controller.
	h.key("/dev/input/event7", btnSouth, 1)
	h.key("/dev/input/event2", btnMode, 1)
	if len(woken) != 2 {
		t.Errorf("woken = %q; only a controller's logo button may ask for the TV", woken)
	}

	// A controller's presses are reported apart from the remote's, because
	// only the remote's prove that the TV is on.
	actions, _, _ := h.snapshot()
	if got := strings.Join(padActions, ","); got != "home,select" {
		t.Errorf("controller actions = %s, want home,select", got)
	}
	if got := strings.Join(actions, ","); got != "home" {
		t.Errorf("remote actions = %s, want home (the press on the TV's device)", got)
	}

	// What the status page shows.
	if got := strings.Join(h.Devices(), "|"); got != "LG TV|Xbox Wireless Controller" {
		t.Errorf("devices = %s", got)
	}
	last, ok := h.Last()
	if !ok || last.Device != "LG TV" || last.Key != "BTN_MODE" || last.Action != "home" {
		t.Errorf("last button = %+v", last)
	}
	h.listening("/dev/input/event7", "", false, false)
	if got := strings.Join(h.Devices(), "|"); got != "LG TV" {
		t.Errorf("devices after the controller went = %s", got)
	}
}

func TestAHeldDirectionRepeats(t *testing.T) {
	h := newRemoteHarness(time.Hour)
	h.RepeatAfter, h.RepeatEvery = 60*time.Millisecond, 20*time.Millisecond
	var mu sync.Mutex
	var repeats []string
	h.OnRepeat = func(a string) { mu.Lock(); repeats = append(repeats, a); mu.Unlock() }
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(repeats) }

	// A short press is one step, however the TV reports its release.
	h.key("cec", keyRight, 1)
	time.Sleep(30 * time.Millisecond)
	h.key("cec", keyRight, 0)
	time.Sleep(120 * time.Millisecond)
	if actions, _, _ := h.snapshot(); strings.Join(actions, ",") != "right" || count() != 0 {
		t.Fatalf("a short press: actions %v, %d repeats; want one step and no repeat", actions, count())
	}

	// Held, it keeps going until it is let go.
	h.key("cec", keyDown, 1)
	time.Sleep(200 * time.Millisecond)
	h.key("cec", keyDown, 0)
	n := count()
	if n < 3 || n > 9 {
		t.Errorf("held for 200 ms: %d repeats, want about 7", n)
	}
	time.Sleep(80 * time.Millisecond)
	if count() != n {
		t.Errorf("it went on repeating after the button was let go")
	}
	mu.Lock()
	for _, a := range repeats {
		if a != "down" {
			t.Errorf("repeated %q, want down", a)
		}
	}
	mu.Unlock()

	// Only directions repeat: a held OK must not choose again and again.
	h.key("cec", keyOK, 1)
	time.Sleep(150 * time.Millisecond)
	h.key("cec", keyOK, 0)
	if count() != n {
		t.Errorf("OK repeated")
	}

	// Pressing another button ends the repeating of the first, also when its
	// release never arrives.
	h.key("cec", keyLeft, 1)
	time.Sleep(100 * time.Millisecond)
	h.key("cec", keyOK, 1)
	before := count()
	time.Sleep(100 * time.Millisecond)
	if count() != before {
		t.Errorf("the direction kept repeating after another button was pressed")
	}
}

func TestAControllerIsAKeyboardInsideAWebsite(t *testing.T) {
	h := newRemoteHarness(time.Hour)
	h.RepeatAfter, h.RepeatEvery = 60*time.Millisecond, 20*time.Millisecond
	h.listening("/dev/input/event7", "Xbox Wireless Controller", true, true)
	h.listening("/dev/input/event2", "LG TV", false, true)
	h.appRunning = true
	website := true
	h.PadAsKeys = func() bool { return website }
	pad := func(code uint16) {
		h.key("/dev/input/event7", code, 1)
		h.key("/dev/input/event7", code, 0)
	}

	// The direction pad, the stick (which arrives as arrows), and the buttons.
	for _, b := range []uint16{btnDpadDown, keyRight, btnSouth, btnEast, btnStart, btnTL, 0x133} {
		pad(b)
	}
	_, taps, _ := h.snapshot()
	want := []uint16{keyDown, keyRight, keyEnter, keyEsc, keyPlayPause}
	if len(taps) != len(want) {
		t.Fatalf("keys pressed in the website = %v, want %v", taps, want)
	}
	for i := range want {
		if taps[i] != want[i] {
			t.Errorf("key %d = %d, want %d", i, taps[i], want[i])
		}
	}

	// A held direction keeps scrolling.
	h.key("/dev/input/event7", btnDpadRight, 1)
	time.Sleep(160 * time.Millisecond)
	h.key("/dev/input/event7", btnDpadRight, 0)
	_, taps, _ = h.snapshot()
	if extra := len(taps) - len(want); extra < 3 {
		t.Errorf("a held direction pressed Right %d times, want several", extra)
	}

	// The remote's own arrows reach the website by themselves: nothing is
	// added for them, held or not.
	before := len(taps)
	h.key("/dev/input/event2", keyDown, 1)
	time.Sleep(120 * time.Millisecond)
	h.key("/dev/input/event2", keyDown, 0)
	if _, taps, _ = h.snapshot(); len(taps) != before {
		t.Errorf("the remote's arrow was pressed a second time by PiDash: %v", taps[before:])
	}

	// In a game the controller is the game's.
	website = false
	pad(btnSouth)
	pad(btnDpadDown)
	if _, taps, _ = h.snapshot(); len(taps) != before {
		t.Errorf("keys were pressed in a game: %v", taps[before:])
	}
	if actions, _, _ := h.snapshot(); len(actions) != 0 {
		t.Errorf("the dashboard reacted while an app was open: %v", actions)
	}
}

func TestLogoButtonAndUpOrDownIsTheVolume(t *testing.T) {
	h := newRemoteHarness(80 * time.Millisecond) // holding the logo button this long leaves an app
	h.RepeatAfter, h.RepeatEvery = 60*time.Millisecond, 20*time.Millisecond
	var mu sync.Mutex
	var steps []int
	h.OnVolume = func(step int) { mu.Lock(); steps = append(steps, step); mu.Unlock() }
	got := func() []int { mu.Lock(); defer mu.Unlock(); return append([]int{}, steps...) }
	const pad, tv = "/dev/input/event7", "/dev/input/event2"
	h.listening(pad, "DualSense Wireless Controller", true, true)
	h.listening(tv, "LG TV", false, true)
	h.appRunning = true // in the middle of a game

	// Without the logo button, up and down are the game's.
	h.key(pad, btnDpadUp, 1)
	h.key(pad, btnDpadUp, 0)
	if len(got()) != 0 {
		t.Fatalf("the volume changed without the logo button: %v", got())
	}

	// Logo button held: up, up, down. And the game is not left, although
	// the logo button stays down for longer than it takes to leave.
	h.key(pad, btnMode, 1)
	h.key(pad, btnDpadUp, 1)
	h.key(pad, btnDpadUp, 0)
	h.key(pad, keyUp, 1) // the stick, which arrives as an arrow
	h.key(pad, keyUp, 0)
	h.key(pad, btnDpadDown, 1)
	h.key(pad, btnDpadDown, 0)
	time.Sleep(200 * time.Millisecond)
	h.key(pad, btnMode, 0)
	if g := got(); len(g) != 3 || g[0] != 1 || g[1] != 1 || g[2] != -1 {
		t.Errorf("volume steps = %v, want +1 +1 -1", g)
	}
	if _, _, exits := h.snapshot(); exits != 0 {
		t.Errorf("the game was closed %d times while the logo button was held for the volume", exits)
	}

	// Held down, it keeps turning.
	h.key(pad, btnMode, 1)
	h.key(pad, btnDpadDown, 1)
	time.Sleep(160 * time.Millisecond)
	h.key(pad, btnDpadDown, 0)
	h.key(pad, btnMode, 0)
	if g := got(); len(g) < 6 {
		t.Errorf("a held direction turned the volume %d steps, want several", len(g)-3)
	}
	for _, s := range got()[3:] {
		if s != -1 {
			t.Errorf("turned the wrong way: %v", got()[3:])
			break
		}
	}

	// After letting go, the logo button is the way out of the game again.
	time.Sleep(100 * time.Millisecond) // the release is believed
	h.key(pad, btnMode, 1)
	time.Sleep(200 * time.Millisecond)
	h.key(pad, btnMode, 0)
	if _, _, exits := h.snapshot(); exits != 1 {
		t.Errorf("holding the logo button alone closed the game %d times, want once", exits)
	}

	// Left and right with the logo button are nothing, and another device's
	// arrows never count, whatever a controller is holding.
	n := len(got())
	h.appRunning = false
	h.key(pad, btnMode, 1)
	h.key(pad, btnDpadLeft, 1)
	h.key(pad, btnDpadLeft, 0)
	h.key(tv, keyUp, 1)
	h.key(tv, keyUp, 0)
	h.key(pad, btnMode, 0)
	if len(got()) != n {
		t.Errorf("the volume changed on left, or on the TV remote's arrow: %v", got()[n:])
	}
}
