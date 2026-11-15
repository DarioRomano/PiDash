package cec

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Device is something else connected to the TV, as the TV's own bus tells of it.
type Device struct {
	// Address is where it is plugged in: "2.0.0.0" is the TV's second HDMI socket.
	Address string `json:"address"`
	// Name is what it calls itself: "PlayStation 5".
	Name string `json:"name"`
}

var addressRe = regexp.MustCompile(`^[1-9a-fA-F]\.[0-9a-fA-F]\.[0-9a-fA-F]\.[0-9a-fA-F]$`)

// PortAddress returns the address of whatever is plugged into one of the
// TV's HDMI sockets, counted from 1.
func PortAddress(port int) string { return fmt.Sprintf("%x.0.0.0", port) }

// ValidAddress reports whether a text is the address of a device on a TV.
func ValidAddress(address string) bool { return addressRe.MatchString(address) }

// Show has the TV show another of its inputs: the device at address is
// asked to take the screen, and the TV is told that it has. What is plugged
// in there may wake up for it. The Pi is no longer what the TV shows.
func (c *Controller) Show(ctx context.Context, address string) error {
	if !ValidAddress(address) {
		return fmt.Errorf("%q is not the address of a TV input", address)
	}
	a, err := c.ready(ctx)
	if err != nil {
		return err
	}
	if strings.EqualFold(address, a.phys) {
		// The Pi's own socket: that is showing the dashboard.
		return c.activate(ctx, a)
	}
	// "Set stream path" is what a TV says when an input is chosen with its
	// own remote; the device there answers by announcing itself. For one
	// that stays silent, the announcement is made on its behalf.
	if _, err := c.run(ctx, "-d", a.dev, "--to", "15", "--set-stream-path", "phys-addr="+address); err != nil {
		return err
	}
	if _, err := c.run(ctx, "-d", a.dev, "--to", "15", "--active-source", "phys-addr="+address); err != nil {
		return err
	}
	c.active.Store(false)
	return nil
}

var (
	// One block of cec-ctl's listing describes one device.
	blockRe = regexp.MustCompile(`(?m)^\s*System Information for device`)
	osdRe   = regexp.MustCompile(`OSD Name\s*:\s*'?([^'\n]*?)'?\s*$`)
)

// Others lists what else is connected to the TV. It asks every device in
// turn, which takes some seconds; devices that are switched off at the wall
// do not answer and are missing.
func (c *Controller) Others(ctx context.Context) ([]Device, error) {
	a, err := c.ready(ctx)
	if err != nil {
		return nil, err
	}
	out, err := c.run(ctx, "-d", a.dev, "--show-topology")
	if err != nil && strings.TrimSpace(out) == "" {
		return nil, err
	}
	return parseDevices(out, a.phys), nil
}

// parseDevices reads cec-ctl's listing. own is the Pi's address, left out.
func parseDevices(out, own string) []Device {
	var devices []Device
	seen := map[string]bool{}
	starts := blockRe.FindAllStringIndex(out, -1)
	for i, at := range starts {
		end := len(out)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		block := out[at[0]:end]
		m := physRe.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		address := strings.ToLower(m[1])
		if !ValidAddress(address) || address == own || seen[address] {
			continue // the TV itself is 0.0.0.0
		}
		seen[address] = true
		name := ""
		for _, line := range strings.Split(block, "\n") {
			if mm := osdRe.FindStringSubmatch(line); mm != nil {
				name = strings.TrimSpace(mm[1])
			}
		}
		devices = append(devices, Device{Address: address, Name: name})
	}
	return devices
}
