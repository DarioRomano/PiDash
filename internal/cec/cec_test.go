package cec

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeTV imitates cec-ctl talking to two adapters; only "hdmi1" has a TV.
type fakeTV struct {
	calls      []string
	registered bool
	power      string
	noTV       bool
}

func (f *fakeTV) run(_ context.Context, args ...string) (string, error) {
	line := strings.Join(args, " ")
	f.calls = append(f.calls, line)
	dev := args[1]
	rest := strings.Join(args[2:], " ")

	if rest == "" { // state query
		if dev == "hdmi0" || f.noTV {
			return "Driver Info:\n\tPhysical Address           : f.f.f.f\n\tLogical Address Mask       : 0x0000\n", nil
		}
		mask := "0x0000"
		if f.registered {
			mask = "0x0010"
		}
		return "Driver Info:\n\tDriver Name                : vc4_hdmi\n\tPhysical Address           : 1.0.0.0\n\tLogical Address Mask       : " + mask + "\n\tOSD Name                   : PiDash\n", nil
	}
	if dev != "hdmi1" {
		return "", errors.New("wrong adapter")
	}
	switch {
	case strings.HasPrefix(rest, "--playback"):
		f.registered = true
		return "", nil
	case !f.registered:
		return "", errors.New("not registered")
	case rest == "--to 0 --standby":
		f.power = "standby"
	case rest == "--to 0 --image-view-on":
		f.power = "on"
	case rest == "--to 0 --give-device-power-status":
		return "Transmit from Playback Device 1 to TV (4 to 0):\nCEC_MSG_GIVE_DEVICE_POWER_STATUS (0x8f)\n    Received from TV (0 to 4):\n    CEC_MSG_REPORT_POWER_STATUS (0x90):\n\tpwr-state: " + f.power + " (0x01)\n", nil
	}
	return "", nil
}

func newFake() (*fakeTV, *Controller) {
	tv := &fakeTV{power: "on"}
	return tv, &Controller{Run: tv.run, Devices: []string{"hdmi0", "hdmi1"}, OSDName: "PiDash"}
}

func TestSetupRegistersOnTheAdapterWithATV(t *testing.T) {
	tv, c := newFake()
	if err := c.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"-d hdmi0",
		"-d hdmi1",
		"-d hdmi1 --playback --osd-name PiDash",
		"-d hdmi1 --to 0 --image-view-on",
		"-d hdmi1 --to 15 --active-source phys-addr=1.0.0.0",
	}
	if strings.Join(tv.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(tv.calls, "\n"), strings.Join(want, "\n"))
	}
	// Already registered: it is not claimed a second time.
	tv.calls = nil
	c.Setup(context.Background())
	for _, call := range tv.calls {
		if strings.Contains(call, "--playback") {
			t.Errorf("registered twice: %v", tv.calls)
		}
	}
}

func TestStandbyWakeAndPowerStatus(t *testing.T) {
	tv, c := newFake()
	ctx := context.Background()

	if err := c.Standby(ctx); err != nil {
		t.Fatal(err)
	}
	if p, err := c.PowerStatus(ctx); err != nil || p != PowerStandby {
		t.Errorf("after standby: %v, %v", p, err)
	}
	if err := c.Wake(ctx); err != nil {
		t.Fatal(err)
	}
	if p, _ := c.PowerStatus(ctx); p != PowerOn {
		t.Errorf("after wake: %v", p)
	}
	tv.power = "to-standby" // a TV in transition is neither on nor off
	if p, _ := c.PowerStatus(ctx); p != PowerUnknown {
		t.Errorf("transitional state: %v", p)
	}
}

func TestNoTVIsReported(t *testing.T) {
	tv, c := newFake()
	tv.noTV = true
	if err := c.Standby(context.Background()); !errors.Is(err, ErrNoTV) {
		t.Errorf("err = %v, want ErrNoTV", err)
	}
	if p, err := c.PowerStatus(context.Background()); p != PowerUnknown || err == nil {
		t.Errorf("power = %v, err = %v", p, err)
	}
}

func TestVolumeButtonsArePressedAndLetGo(t *testing.T) {
	tv, c := newFake()
	var sent []string
	inner := c.Run
	c.Run = func(ctx context.Context, args ...string) (string, error) {
		if len(args) > 3 && args[2] == "--to" {
			sent = append(sent, strings.Join(args[2:], " "))
		}
		return inner(ctx, args...)
	}
	_ = tv
	ctx := context.Background()
	if err := c.Volume(ctx, "up", 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Volume(ctx, "down", 5); err != nil { // to a soundbar
		t.Fatal(err)
	}
	if err := c.Volume(ctx, "mute", 0); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--to 0 --user-control-pressed ui-cmd=volume-up --user-control-released",
		"--to 5 --user-control-pressed ui-cmd=volume-down --user-control-released",
		"--to 0 --user-control-pressed ui-cmd=mute --user-control-released",
	}
	var volume []string
	for _, s := range sent {
		if strings.Contains(s, "user-control") {
			volume = append(volume, s)
		}
	}
	if strings.Join(volume, "\n") != strings.Join(want, "\n") {
		t.Errorf("sent:\n%s\nwant:\n%s", strings.Join(volume, "\n"), strings.Join(want, "\n"))
	}
	if err := c.Volume(ctx, "louder; reboot", 0); err == nil {
		t.Error("an unknown button must be refused")
	}
}

func TestStartingTheDashboardDoesNotWakeTheTV(t *testing.T) {
	tv, c := newFake()
	ctx := context.Background()

	// The TV is on: it is told to show the Pi, and nothing more.
	shown, err := c.Announce(ctx)
	if err != nil || !shown {
		t.Fatalf("TV on: shown=%v err=%v", shown, err)
	}
	joined := strings.Join(tv.calls, "\n")
	if !strings.Contains(joined, "--playback --osd-name PiDash") || !strings.Contains(joined, "--active-source phys-addr=1.0.0.0") {
		t.Errorf("the Pi should register and ask to be shown:\n%s", joined)
	}
	if strings.Contains(joined, "--image-view-on") {
		t.Errorf("nothing that wakes a TV should be sent:\n%s", joined)
	}

	// The TV is in standby: it stays there, but the remote will reach the Pi.
	tv2, c2 := newFake()
	tv2.power = "standby"
	shown, err = c2.Announce(ctx)
	if err != nil || shown {
		t.Fatalf("TV in standby: shown=%v err=%v", shown, err)
	}
	joined = strings.Join(tv2.calls, "\n")
	if strings.Contains(joined, "--image-view-on") || strings.Contains(joined, "--active-source") || tv2.power != "standby" {
		t.Errorf("a TV in standby was disturbed:\n%s", joined)
	}
	if !tv2.registered {
		t.Error("the Pi should register even with the TV in standby")
	}
}
