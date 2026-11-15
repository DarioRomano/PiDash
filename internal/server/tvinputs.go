package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/DarioRomano/PiDash/internal/cec"
	"github.com/DarioRomano/PiDash/internal/tv"
)

// RemoteActionFrom delivers a button press of a game controller to the
// dashboard, and says whose buttons the controller has, so that the screen
// can name them.
func (s *Server) RemoteActionFrom(action, pad string) {
	s.hub.broadcast("input", inputEvent{Action: action, Source: "evdev", Pad: pad})
}

// OpenPage has the dashboard open one of its pages: "games" when a
// controller has switched the TV on.
func (s *Server) OpenPage(page string) {
	s.hub.broadcast("page", struct {
		Page string `json:"page"`
	}{page})
}

// handleTVInput has the TV show another device: POST /api/tv/input {"index":0}.
func (s *Server) handleTVInput(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Index *int `json:"index"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || req.Index == nil || *req.Index < 0 || *req.Index >= len(s.cfg.TV.Inputs) {
		http.Error(w, "there is no such TV input", http.StatusBadRequest)
		return
	}
	in := s.cfg.TV.Inputs[*req.Index]
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.tv.ShowInput(ctx, in.Where()); err != nil {
		s.log.Warn("tv input", "input", in.Name, "err", err)
		code := http.StatusBadGateway
		if errors.Is(err, tv.ErrNoInputs) {
			code = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), code)
		return
	}
	s.log.Info("the TV was asked to show another input", "input", in.Name, "address", in.Where())
	w.WriteHeader(http.StatusNoContent)
}

// handleTVDevices lists what else is connected to the TV, for the settings page.
func (s *Server) handleTVDevices(w http.ResponseWriter, r *http.Request) {
	if !s.pin.allow(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	devices, err := s.tv.Others(ctx)
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, tv.ErrNoInputs) {
			code = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), code)
		return
	}
	if devices == nil {
		devices = []cec.Device{}
	}
	writeJSON(w, struct {
		Devices []cec.Device `json:"devices"`
	}{devices})
}
