package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/DarioRomano/PiDash/internal/playing"
)

// Player is what the server needs to know about the music that is playing.
type Player interface {
	Current() *playing.Track
	Control(ctx context.Context, button string) error
	ArtFile() (string, bool)
}

// PlayingChanged tells the dashboard what is playing now; nil for nothing.
func (s *Server) PlayingChanged(t *playing.Track) {
	s.hub.broadcast("playing", t)
}

func (s *Server) handlePlaying(w http.ResponseWriter, _ *http.Request) {
	var t *playing.Track
	if s.player != nil {
		t = s.player.Current()
	}
	writeJSON(w, t)
}

// handlePlayingControl presses play/pause, next or previous on whatever is
// playing: what the same buttons on the remote do.
func (s *Server) handlePlayingControl(w http.ResponseWriter, r *http.Request) {
	if s.player == nil {
		http.Error(w, playing.ErrNothing.Error(), http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	switch err := s.player.Control(ctx, r.PathValue("button")); {
	case errors.Is(err, playing.ErrNothing):
		http.Error(w, err.Error(), http.StatusNotFound)
	case err != nil:
		s.log.Info("now playing: button", "button", r.PathValue("button"), "err", err)
		http.Error(w, "the player did not react", http.StatusBadGateway)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handlePlayingArt serves the cover of what is playing when the player keeps
// it as a file on the Pi. Only that one file, and only if it is a picture.
func (s *Server) handlePlayingArt(w http.ResponseWriter, _ *http.Request) {
	if s.player == nil {
		http.NotFound(w, nil)
		return
	}
	path, ok := s.player.ArtFile()
	if !ok {
		http.Error(w, "no cover", http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "no cover", http.StatusNotFound)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 8<<20))
	if err != nil {
		http.Error(w, "no cover", http.StatusNotFound)
		return
	}
	kind := http.DetectContentType(data)
	if !strings.HasPrefix(kind, "image/") {
		http.Error(w, "no cover", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}
