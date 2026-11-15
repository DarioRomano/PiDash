package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

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

// handlePhotoPlaces lists where the photos were taken, for the map of all of them.
func (s *Server) handlePhotoPlaces(w http.ResponseWriter, _ *http.Request) {
	out := struct {
		Places []photos.Place `json:"places"`
		Map    bool           `json:"map"`
	}{Places: []photos.Place{}, Map: s.maps != nil}
	if s.photos != nil {
		out.Places = s.photos.Places()
	}
	writeJSON(w, out)
}

// handlePhotoTrips lists the journeys the photos tell of.
func (s *Server) handlePhotoTrips(w http.ResponseWriter, _ *http.Request) {
	out := struct {
		Trips []photos.Trip `json:"trips"`
		Map   bool          `json:"map"`
	}{Trips: []photos.Trip{}, Map: s.maps != nil}
	if s.photos != nil {
		out.Trips = s.photos.Trips()
	}
	writeJSON(w, out)
}

// handlePhotoQuiz hands out the questions of one round of the quiz.
func (s *Server) handlePhotoQuiz(w http.ResponseWriter, r *http.Request) {
	out := struct {
		Questions []photos.Question `json:"questions"`
	}{Questions: []photos.Question{}}
	if s.photos != nil {
		n, _ := strconv.Atoi(r.URL.Query().Get("n"))
		if n <= 0 || n > 30 {
			n = 10
		}
		out.Questions = s.photos.Quiz(n, nil, time.Now())
	}
	writeJSON(w, out)
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
