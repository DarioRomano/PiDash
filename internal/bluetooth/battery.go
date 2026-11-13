package bluetooth

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// powerSupplies is where Linux lists batteries, those of the devices
// connected to the computer among them.
var powerSupplies = "/sys/class/power_supply"

var macIn = regexp.MustCompile(`[0-9a-f]{2}(?::[0-9a-f]{2}){5}`)

// kernelBatteries returns the charge, in percent, of connected devices whose
// driver in Linux knows it, by their Bluetooth address in lower case.
//
// BlueZ only knows the battery of devices that report it the Bluetooth way.
// PlayStation and Nintendo controllers report it to their driver instead,
// which lists it here under a name that contains the controller's address,
// such as "ps-controller-battery-a4:53:85:01:02:03".
func kernelBatteries(dir string) map[string]int {
	out := map[string]int{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	read := func(name, file string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name, file))
		return strings.TrimSpace(string(b))
	}
	for _, e := range entries {
		mac := macIn.FindString(strings.ToLower(e.Name()))
		// "Device" marks the battery of something attached, as opposed to
		// the computer's own.
		if mac == "" || read(e.Name(), "scope") != "Device" {
			continue
		}
		if pct, err := strconv.Atoi(read(e.Name(), "capacity")); err == nil && pct >= 0 && pct <= 100 {
			out[mac] = pct
		}
	}
	return out
}
