package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/DarioRomano/PiDash/internal/bluetooth"
)

// handleBluetooth lists the Pi's Bluetooth devices and those found nearby.
func (s *Server) handleBluetooth(w http.ResponseWriter, r *http.Request) {
	if s.bluetooth == nil {
		writeJSON(w, bluetooth.Status{Devices: []bluetooth.Device{}, Problem: "Bluetooth is not available here."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, s.bluetooth.Status(ctx))
}

// handleBluetoothAction switches Bluetooth, searches, pairs, connects and
// forgets. These change the Pi, so they need the settings PIN when one is set.
// The answer is the new state, or a sentence that says what went wrong.
func (s *Server) handleBluetoothAction(w http.ResponseWriter, r *http.Request) {
	if !s.pin.allow(w, r) {
		return
	}
	if s.bluetooth == nil {
		http.Error(w, "Bluetooth is not available here.", http.StatusNotFound)
		return
	}
	var req struct {
		Address string `json:"address"`
		On      bool   `json:"on"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	// Pairing waits for the device, which can take most of a minute.
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	var err error
	action := r.PathValue("action")
	switch action {
	case "power":
		err = s.bluetooth.Power(ctx, req.On)
	case "search":
		err = s.bluetooth.Search(ctx, req.On)
	case "pair":
		err = s.bluetooth.Pair(ctx, req.Address)
	case "connect":
		err = s.bluetooth.Connect(ctx, req.Address)
	case "disconnect":
		err = s.bluetooth.Disconnect(ctx, req.Address)
	case "forget":
		err = s.bluetooth.Forget(ctx, req.Address)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, bluetooth.ErrUnknown) {
			status = http.StatusNotFound
		}
		s.log.Info("bluetooth", "action", action, "device", req.Address, "err", err)
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, s.bluetooth.Status(ctx))
}

// lowBattery is the charge, in percent, at which the TV says that a
// controller or headset wants charging; charged above batteryFine it may be
// said again the next time.
const (
	lowBattery  = 15
	batteryFine = 25
)

// batteryNotice is sent to the dashboard when a device runs low.
type batteryNotice struct {
	Name    string `json:"name"`
	Percent int    `json:"percent"`
}

// watchBatteries tells the dashboard when a connected Bluetooth device runs
// low, once per device until it has been charged or switched off.
func (s *Server) watchBatteries(ctx context.Context, every time.Duration) {
	if s.bluetooth == nil {
		return
	}
	told := map[string]bool{}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.checkBatteries(ctx, told)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) checkBatteries(ctx context.Context, told map[string]bool) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	seen := map[string]bool{}
	for _, d := range s.bluetooth.Status(ctx).Devices {
		if !d.Connected || d.Battery == nil {
			continue
		}
		seen[d.Address] = true
		switch {
		case *d.Battery > batteryFine:
			delete(told, d.Address)
		case *d.Battery <= lowBattery && !told[d.Address]:
			told[d.Address] = true
			s.log.Info("a Bluetooth device is running low", "device", d.Name, "battery", *d.Battery)
			s.hub.broadcast("battery", batteryNotice{Name: d.Name, Percent: *d.Battery})
		}
	}
	// Switched off, or out of range: the next time it is back and low, say so again.
	for address := range told {
		if !seen[address] {
			delete(told, address)
		}
	}
}
