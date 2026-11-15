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
// ChannelKey is told that channel up or down was pressed on the remote. While
// the sound goes to headphones or a speaker it changes their volume, and
// reports true: the press is used up. With the sound on the TV it does
// nothing, and the buttons keep what they otherwise do.
func (s *Server) ChannelKey(up bool) bool {
	if s.sound == nil || !s.cfg.Sound.RemoteVolume {
		return false
	}
	s.soundMu.Lock()
	st := s.lastSound
	s.soundMu.Unlock()
	if st == nil || !st.Available {
		return false
	}
	for _, o := range st.Outputs {
		if o.Active && o.Kind != "hdmi" {
			step := -5
			if up {
				step = 5
			}
			select {
			case s.volumeSteps <- step:
			default: // one is on its way already; a held button sends plenty
			}
			return true
		}
	}
	return false
}

// stepVolume changes the volume of the output in use by so many percent and
// tells the pages, which show it.
func (s *Server) stepVolume(ctx context.Context, step int) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	st := s.sound.Status(ctx)
	for _, o := range st.Outputs {
		if !o.Active {
			continue
		}
		to := min(100, max(0, o.Volume+step))
		if err := s.sound.SetVolume(ctx, o.ID, to); err != nil {
			s.log.Warn("could not change the volume", "err", err)
			return
		}
		if o.Muted && step > 0 {
			_ = s.sound.SetMuted(ctx, o.ID, false)
		}
		st = s.sound.Status(ctx)
		s.soundMu.Lock()
		s.lastSound = &st
		s.soundMu.Unlock()
		s.hub.broadcast("sound", st)
		s.hub.broadcast("volume", struct {
			Name   string `json:"name"`
			Volume int    `json:"volume"`
		}{o.Name, to})
		return
	}
}

func (s *Server) followSound(ctx context.Context) {
	if s.sound == nil {
		return
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case step := <-s.volumeSteps:
				s.stepVolume(ctx, step)
			}
		}
	}()
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
