package server

import (
	"context"
	"encoding/json"
	"net/http"
)

// startArt begins listing the museum's paintings. It is done the first time
// they are wanted, so that a Pi on which nobody looks at them asks the
// museum for nothing.
func (s *Server) startArt() {
	s.artStart.Do(func() {
		ctx := s.runCtx
		if ctx == nil {
			ctx = context.Background()
		}
		go s.art.Run(ctx)
	})
}

// handleArtMode switches between the photos and the paintings:
// POST /api/art/mode {"on":true}. The choice is kept.
func (s *Server) handleArtMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On *bool `json:"on"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || req.On == nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.settings != nil {
		if err := s.settings.SetArtMode(*req.On); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.artOn.Store(*req.On)
	if *req.On {
		s.startArt()
	}
	s.log.Info("art mode", "on", *req.On)
	cfg := s.clientConfig()
	s.hub.broadcast("config", cfg)
	writeJSON(w, cfg)
}
