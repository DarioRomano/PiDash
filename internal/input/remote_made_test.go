package input

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWhichDevicesAreMadeByAProgram(t *testing.T) {
	root := t.TempDir()
	class := filepath.Join(root, "sys", "class", "input")
	if err := os.MkdirAll(class, 0o755); err != nil {
		t.Fatal(err)
	}
	// As the kernel lays them out: a USB pad, a Bluetooth pad, and one a
	// program made.
	for name, where := range map[string]string{
		"event3": "devices/platform/axi/1000120000.pcie/usb1/1-2/1-2:1.0/input/input7/event3",
		"event5": "devices/virtual/misc/uhid/0005:054C:0CE6.0007/input/input20/event5",
		"event9": "devices/virtual/input/input31/event9",
	} {
		target := filepath.Join(root, "sys", where)
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(class, name)); err != nil {
			t.Fatal(err)
		}
	}
	for device, want := range map[string]bool{"/dev/input/event3": false, "/dev/input/event5": false, "/dev/input/event9": true, "/dev/input/event77": false} {
		if got := madeByAProgram(class, device); got != want {
			t.Errorf("%s: made by a program = %v, want %v", device, got, want)
		}
	}
}
