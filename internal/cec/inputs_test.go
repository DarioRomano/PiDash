package cec

import (
	"context"
	"strings"
	"testing"
)

// What cec-ctl prints for a TV with a console and a soundbar beside the Pi.
const topology = `Driver Info:
	Driver Name                : vc4_hdmi
	Physical Address           : 1.0.0.0
	Logical Address Mask       : 0x0010

	System Information for device 0 (TV) from device 4 (Playback Device 1):
		CEC Version                : 1.4
		Physical Address           : 0.0.0.0
		Primary Device Type        : TV
		Vendor ID                  : 0x00e091 (LG)
		OSD Name                   : 'TV'
		Power Status               : on

	System Information for device 8 (Playback Device 2) from device 4 (Playback Device 1):
		CEC Version                : 2.0
		Physical Address           : 2.0.0.0
		Primary Device Type        : Playback
		Vendor ID                  : 0x080046 (Sony)
		OSD Name                   : 'PlayStation 5'
		Power Status               : standby

	System Information for device 5 (Audio System) from device 4 (Playback Device 1):
		CEC Version                : 1.4
		Physical Address           : 3.0.0.0
		Primary Device Type        : Audio System
		OSD Name                   : Soundbar

	Topology:

	0.0.0.0: TV
	    1.0.0.0: Playback Device 1
	    2.0.0.0: Playback Device 2
	    3.0.0.0: Audio System
`

func TestDevicesOnTheTV(t *testing.T) {
	got := parseDevices(topology, "1.0.0.0")
	if len(got) != 2 || got[0] != (Device{"2.0.0.0", "PlayStation 5"}) || got[1] != (Device{"3.0.0.0", "Soundbar"}) {
		t.Fatalf("devices = %+v", got)
	}
	if got := parseDevices("nothing useful", "1.0.0.0"); len(got) != 0 {
		t.Fatalf("devices from nonsense: %+v", got)
	}
}

func TestShowingAnotherInput(t *testing.T) {
	var calls []string
	c := &Controller{Devices: []string{"hdmi0"}, Run: func(_ context.Context, args ...string) (string, error) {
		line := strings.Join(args, " ")
		calls = append(calls, line)
		if line == "-d hdmi0" {
			return "Physical Address : 1.0.0.0\nLogical Address Mask : 0x0010\n", nil
		}
		return "", nil
	}}
	c.active.Store(true)
	ctx := context.Background()
	if err := c.Show(ctx, PortAddress(2)); err != nil {
		t.Fatal(err)
	}
	sent := strings.Join(calls, "\n")
	if !strings.Contains(sent, "--to 15 --set-stream-path phys-addr=2.0.0.0") || !strings.Contains(sent, "--to 15 --active-source phys-addr=2.0.0.0") {
		t.Fatalf("sent:\n%s", sent)
	}
	if c.active.Load() {
		t.Error("the Pi still counts as what the TV shows")
	}
	// The Pi's own socket is showing the dashboard again.
	calls = nil
	if err := c.Show(ctx, "1.0.0.0"); err != nil {
		t.Fatal(err)
	}
	if sent := strings.Join(calls, "\n"); !strings.Contains(sent, "--active-source phys-addr=1.0.0.0") || strings.Contains(sent, "set-stream-path") || !c.active.Load() {
		t.Fatalf("back to the Pi:\n%s", sent)
	}
	for _, bad := range []string{"", "0.0.0.0", "2", "2.0.0", "g.0.0.0", "2.0.0.0; reboot"} {
		if err := c.Show(ctx, bad); err == nil {
			t.Errorf("Show(%q) was accepted", bad)
		}
	}
	if PortAddress(3) != "3.0.0.0" {
		t.Errorf("PortAddress(3) = %q", PortAddress(3))
	}
}
