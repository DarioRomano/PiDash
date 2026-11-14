package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/DarioRomano/PiDash/internal/maps"
	"github.com/DarioRomano/PiDash/internal/photos"
)

// handlePhotoFavourite marks photos as favourites, or takes the mark away.
func (s *Server) handlePhotoFavourite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keys []string `json:"keys"`
		On   bool     `json:"on"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || len(req.Keys) == 0 || len(req.Keys) > 4 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.photos == nil {
		http.Error(w, "no photo source is set up", http.StatusNotFound)
		return
	}
	if err := s.photos.Favourite(req.On, req.Keys...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.photos.Status())
}

// handlePhotoOnlyFavourites narrows the slideshow to the favourites, or
// widens it again.
func (s *Server) handlePhotoOnlyFavourites(w http.ResponseWriter, r *http.Request) {
	var req struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.photos == nil {
		http.Error(w, "no photo source is set up", http.StatusNotFound)
		return
	}
	if err := s.photos.OnlyFavourites(req.On); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.photos.Status())
}

// handlePhotoInfo says where and when the photos on screen were taken.
func (s *Server) handlePhotoInfo(w http.ResponseWriter, r *http.Request) {
	if s.photos == nil {
		http.Error(w, "no photo source is set up", http.StatusNotFound)
		return
	}
	details, err := s.photos.Info(r.Context(), r.URL.Query().Get("key"))
	switch {
	case errors.Is(err, photos.ErrNotFound):
		http.Error(w, "this photo is no longer in the slideshow", http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, struct {
		Photos []photos.Detail `json:"photos"`
		// Map is whether a map can be shown.
		Map bool `json:"map"`
	}{details, s.maps != nil})
}

// handleMapTile serves one piece of the map, from the Pi's own store.
func (s *Server) handleMapTile(w http.ResponseWriter, r *http.Request) {
	if s.maps == nil {
		http.Error(w, "the map is switched off", http.StatusNotFound)
		return
	}
	z, errZ := strconv.Atoi(r.PathValue("z"))
	x, errX := strconv.Atoi(r.PathValue("x"))
	y, errY := strconv.Atoi(r.PathValue("y"))
	if errZ != nil || errX != nil || errY != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	path, err := s.maps.Tile(r.Context(), z, x, y)
	switch {
	case errors.Is(err, maps.ErrNoTile):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case errors.Is(err, maps.ErrBusy):
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	case err != nil:
		s.log.Debug("map tile", "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}
