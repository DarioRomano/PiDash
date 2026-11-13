// Package bluetooth manages the Pi's Bluetooth devices: game controllers,
// headphones, speakers, keyboards. It lists them, finds new ones, pairs,
// connects and forgets them, so none of that needs a mouse and the desktop.
//
// It talks to BlueZ, the Bluetooth service of Linux, the same way the
// desktop's own Bluetooth menu does.
package bluetooth

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// Device is one Bluetooth device the Pi knows or can see.
type Device struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	// Kind is "controller", "audio", "keyboard", "mouse", "phone",
	// "computer", "tv" or "other".
	Kind string `json:"kind"`
	// Paired devices are known to the Pi; the others were found nearby.
	Paired    bool `json:"paired"`
	Connected bool `json:"connected"`
	// Signal is the strength with which a device nearby is received, in dBm
	// (closer to zero is stronger); 0 when unknown.
	Signal int `json:"signal,omitempty"`
	// Battery is how full the device's battery is, in percent. Nil for a
	// device that does not say, which many do not.
	Battery *int `json:"battery,omitempty"`
}

// Status is what the settings page shows.
type Status struct {
	// Available is false when the Pi has no Bluetooth, or its Bluetooth
	// service cannot be reached; Problem then says which.
	Available bool   `json:"available"`
	Problem   string `json:"problem,omitempty"`
	// On says whether Bluetooth is switched on.
	On bool `json:"on"`
	// Searching is true while the Pi looks for devices nearby.
	Searching bool `json:"searching"`
	// Devices lists the paired devices first, then those found nearby.
	Devices []Device `json:"devices"`
}

// Manager is what the server needs. The real one talks to BlueZ; Mock
// pretends, for development and the tests of the pages.
type Manager interface {
	Status(ctx context.Context) Status
	// Power switches Bluetooth on or off.
	Power(ctx context.Context, on bool) error
	// Search starts or stops looking for devices nearby. It stops by itself
	// after a minute.
	Search(ctx context.Context, on bool) error
	// Pair makes a device found nearby one of the Pi's own and connects it.
	Pair(ctx context.Context, address string) error
	Connect(ctx context.Context, address string) error
	Disconnect(ctx context.Context, address string) error
	// Forget removes a paired device.
	Forget(ctx context.Context, address string) error
}

// ErrUnknown is returned for an address that is not among the devices.
var ErrUnknown = errors.New("this device is not known any more. Look for devices again")

// kindOf turns BlueZ's icon name for a device into a kind.
func kindOf(icon string) string {
	switch {
	case icon == "input-gaming":
		return "controller"
	case strings.HasPrefix(icon, "audio-"), icon == "multimedia-player":
		return "audio"
	case icon == "input-keyboard":
		return "keyboard"
	case icon == "input-mouse", icon == "input-tablet":
		return "mouse"
	case icon == "phone":
		return "phone"
	case icon == "computer":
		return "computer"
	case icon == "video-display":
		return "tv"
	}
	return "other"
}

// arrange puts paired devices first, by name, and then those nearby, the
// best received first: the one held next to the Pi is the one wanted.
func arrange(devices []Device) {
	sort.SliceStable(devices, func(i, j int) bool {
		a, b := devices[i], devices[j]
		if a.Paired != b.Paired {
			return a.Paired
		}
		if !a.Paired && a.Signal != b.Signal {
			// 0 means unknown and goes last.
			if a.Signal == 0 || b.Signal == 0 {
				return b.Signal == 0
			}
			return a.Signal > b.Signal
		}
		if !strings.EqualFold(a.Name, b.Name) {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.Address < b.Address
	})
}

// errNotReady is what BlueZ answers while Bluetooth is switched off.
var errNotReady = errors.New("org.bluez.Error.NotReady")

// explain turns a BlueZ error into a sentence for the TV.
func explain(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, ErrUnknown):
		return ErrUnknown
	case strings.Contains(msg, "accessdenied"), strings.Contains(msg, "not authorized"), strings.Contains(msg, "notauthorized"), strings.Contains(msg, "not permitted"):
		return errors.New("PiDash is not allowed to manage Bluetooth. In a terminal: sudo usermod -aG bluetooth $USER, then restart the Pi")
	case strings.Contains(msg, "authentication"):
		return errors.New("the device refused. Put it into pairing mode and try again")
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(msg, "timeout"), strings.Contains(msg, "timed out"), strings.Contains(msg, "host is down"), strings.Contains(msg, "page-timeout"):
		return errors.New("the device did not answer. Is it switched on, nearby and, for a new one, in pairing mode?")
	case strings.Contains(msg, "notready"), strings.Contains(msg, "not ready"), strings.Contains(msg, "not powered"):
		return errors.New("Bluetooth is switched off")
	case strings.Contains(msg, "inprogress"), strings.Contains(msg, "in progress"), strings.Contains(msg, "busy"):
		return errors.New("the Pi is still busy with this device. Try again in a moment")
	case strings.Contains(msg, "rfkill"), strings.Contains(msg, "blocked"):
		return errors.New("Bluetooth is blocked on this Pi. In a terminal: rfkill unblock bluetooth")
	case strings.Contains(msg, "doesnotexist"), strings.Contains(msg, "does not exist"), strings.Contains(msg, "unknownobject"), strings.Contains(msg, "unknown object"):
		return ErrUnknown
	case strings.Contains(msg, "profile-unavailable"), strings.Contains(msg, "notavailable"), strings.Contains(msg, "not available"):
		return errors.New("the device is paired, but the Pi has nothing to use it with. For headphones and speakers the desktop's sound service must be running")
	}
	return errors.New("Bluetooth: " + err.Error())
}
