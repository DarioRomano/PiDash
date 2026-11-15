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
	out := struct {
		Frames  []radar.Frame `json:"frames"`
		MaxZoom int           `json:"maxZoom"`
		// Ahead is the pictures to come, where they can be worked out.
		Ahead *radar.Forecast `json:"ahead,omitempty"`
	}{Frames: frames, MaxZoom: radar.MaxZoom}
	// Asked with a place: where the rain around it is going.
	lat, errLat := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lon, errLon := strconv.ParseFloat(r.URL.Query().Get("lon"), 64)
	if errLat == nil && errLon == nil && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 {
		if ahead, ok := s.radar.Ahead(r.Context(), lat, lon); ok {
			out.Ahead = &ahead
		}
	}
	writeJSON(w, out)
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
	var data []byte
	var err error
	if q := r.URL.Query(); q.Has("from") {
		// A picture to come: the newest one, moved on.
		from, errF := strconv.ParseInt(q.Get("from"), 10, 64)
		mx, errMX := strconv.ParseFloat(q.Get("mx"), 64)
		my, errMY := strconv.ParseFloat(q.Get("my"), 64)
		if errF != nil || errMX != nil || errMY != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		data, err = s.radar.TileAhead(r.Context(), from, at, radar.Motion{X: mx, Y: my}, z, x, y)
	} else {
		data, err = s.radar.Tile(r.Context(), at, z, x, y)
	}
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
