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

// followSound keeps the dashboard informed about the outputs and, when that
// is wanted, moves the sound to headphones as they connect and back as they
// disconnect.
func (s *Server) followSound(ctx context.Context) {
	if s.sound == nil {
		return
	}
	var follower sound.Follower
	look := func() {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		st := s.sound.Status(ctx)
		if id := follower.Step(st); id != "" && s.cfg.Sound.FollowBluetooth {
			if err := s.sound.Use(ctx, id); err != nil {
				s.log.Warn("could not move the sound", "to", id, "err", err)
			} else {
				s.log.Info("the sound follows the headphones", "to", id)
				st = s.sound.Status(ctx)
				follower.Step(st) // seen: this was our own doing
			}
		}
		s.soundMu.Lock()
		s.lastSound = &st
		s.soundMu.Unlock()
		s.hub.broadcast("sound", st)
	}
	// Changes are only noted here and looked at one after the other. A look
	// may change something itself (moving the sound), which is reported as
	// another change; looking from inside the report would wait for itself.
	changed := make(chan struct{}, 1)
	go s.sound.Watch(ctx, func() {
		select {
		case changed <- struct{}{}:
		default: // a look is already due
		}
	})
	look()
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
			look()
		}
	}
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
