package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/DarioRomano/PiDash/internal/sound"
)

// handleSound lists the places sound can go, with their volume.
func (s *Server) handleSound(w http.ResponseWriter, r *http.Request) {
	if s.sound == nil {
		writeJSON(w, sound.Status{Outputs: []sound.Output{}, Problem: "Sound cannot be set here."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	writeJSON(w, s.sound.Status(ctx))
}

// handleSoundAction chooses the output, sets its volume, mutes it, or plays
// a test sound on it. Like the volume buttons of a remote, this needs no PIN.
func (s *Server) handleSoundAction(w http.ResponseWriter, r *http.Request) {
	if s.sound == nil {
		http.Error(w, "Sound cannot be set here.", http.StatusNotFound)
		return
	}
	var req struct {
		ID     string `json:"id"`
		Volume int    `json:"volume"`
		Muted  bool   `json:"muted"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var err error
	action := r.PathValue("action")
	switch action {
	case "use":
		err = s.sound.Use(ctx, req.ID)
	case "volume":
		err = s.sound.SetVolume(ctx, req.ID, req.Volume)
	case "mute":
		err = s.sound.SetMuted(ctx, req.ID, req.Muted)
	case "test":
		err = s.sound.Test(ctx, req.ID)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, sound.ErrUnknown) {
			status = http.StatusNotFound
		}
		s.log.Info("sound", "action", action, "output", req.ID, "err", err)
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, s.sound.Status(ctx))
}
