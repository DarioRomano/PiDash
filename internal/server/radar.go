package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/DarioRomano/PiDash/internal/radar"
)

// handleRadarFrames says which pictures of the rain radar there are.
func (s *Server) handleRadarFrames(w http.ResponseWriter, r *http.Request) {
	if s.radar == nil {
		http.Error(w, "the rain radar is switched off", http.StatusNotFound)
		return
	}
	frames, err := s.radar.Frames(r.Context())
	if err != nil {
		s.log.Debug("rain radar", "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, struct {
		Frames  []radar.Frame `json:"frames"`
		MaxZoom int           `json:"maxZoom"`
	}{frames, radar.MaxZoom})
}

// handleRadarTile serves one piece of the rain radar at one moment.
func (s *Server) handleRadarTile(w http.ResponseWriter, r *http.Request) {
	if s.radar == nil {
		http.Error(w, "the rain radar is switched off", http.StatusNotFound)
		return
	}
	at, errT := strconv.ParseInt(r.PathValue("time"), 10, 64)
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(r.PathValue("y"))
	if errT != nil || errZ != nil || errX != nil || errY != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	data, err := s.radar.Tile(r.Context(), at, z, x, y)
	switch {
	case errors.Is(err, radar.ErrNoTile):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case errors.Is(err, radar.ErrBusy):
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	case err != nil:
		s.log.Debug("rain radar", "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// A picture of a moment that has passed does not change.
	w.Header().Set("Cache-Control", "private, max-age=7200")
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(data)
}
