package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// What is set from the TV itself, one thing at a time, and takes effect at
// once: no settings page, no restart.

// handleTileHidden hides one of PiDash's own tiles, or shows it again.
func (s *Server) handleTileHidden(w http.ResponseWriter, r *http.Request) {
	if !s.pin.allow(w, r) {
		return
	}
	var req struct {
		Tile   string `json:"tile"`
		Hidden bool   `json:"hidden"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || s.settings == nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	now, err := s.settings.HideTile(req.Tile, req.Hidden)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	s.liveMu.Lock()
	s.liveHidden = &now
	s.liveMu.Unlock()
	s.hub.broadcast("config", s.clientConfig())
	writeJSON(w, s.clientConfig())
}

// handlePhotoInterval sets how long a photo stays on screen.
func (s *Server) handlePhotoInterval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Seconds int `json:"seconds"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || (s.photos == nil && s.art == nil) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.settings != nil {
		if err := s.settings.SetPhotoSeconds(req.Seconds); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}
	// The paintings stay as long as the photos do.
	if s.art != nil {
		s.art.SetInterval(time.Duration(req.Seconds) * time.Second)
	}
	if s.photos == nil {
		writeJSON(w, s.art.Status())
		return
	}
	s.photos.SetInterval(time.Duration(req.Seconds) * time.Second)
	writeJSON(w, s.photos.Status())
}

// handleAppRemove takes an app out of the app row. Nothing is uninstalled.
func (s *Server) handleAppRemove(w http.ResponseWriter, r *http.Request) {
	if !s.pin.allow(w, r) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.apps.Status().Running == req.ID {
		http.Error(w, "close the app first", http.StatusConflict)
		return
	}
	if s.settings != nil {
		if err := s.settings.RemoveApp(req.ID); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
	}
	s.apps.Remove(req.ID)
	writeJSON(w, s.apps.Status())
}
