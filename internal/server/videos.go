package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/videos"
)

// Loopback returns the address at which PiDash is reached on the Pi itself.
// Before the server listens it is the address the settings ask for.
func (s *Server) Loopback() string {
	host, port, err := net.SplitHostPort(s.cfg.Server.Listen)
	if err != nil {
		return "http://127.0.0.1:8080"
	}
	if p := s.port.Load(); p != 0 {
		port = strconv.Itoa(int(p))
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// fromThisMachine reports whether a request was made on the Pi itself, also
// where PiDash listens on one of the Pi's addresses rather than on all: a
// program on the Pi then arrives from that same address.
func fromThisMachine(r *http.Request) bool {
	if fromThisPi(r) {
		return true
	}
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if local == nil {
		return false
	}
	here, _, err1 := net.SplitHostPort(local.String())
	there, _, err2 := net.SplitHostPort(r.RemoteAddr)
	return err1 == nil && err2 == nil && here == there
}

// handleVideos lists one folder of videos.
func (s *Server) handleVideos(w http.ResponseWriter, r *http.Request) {
	if s.videos == nil {
		http.Error(w, "no videos are set up", http.StatusNotFound)
		return
	}
	list, err := s.videos.List(r.Context(), r.URL.Query().Get("path"))
	switch {
	case errors.Is(err, videos.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case err != nil:
		s.log.Warn("videos", "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, list)
}

// handleVideoCover serves the cover of one video: the picture beside it, or
// a frame taken from it, made the first time it is asked for.
func (s *Server) handleVideoCover(w http.ResponseWriter, r *http.Request) {
	if s.videos == nil {
		http.Error(w, "no videos are set up", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	file, err := s.videos.Cover(ctx, r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, "no cover", http.StatusNotFound)
		return
	}
	// The address carries a mark that changes with the video's file.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, file)
}

// handleVideoPlay starts the player with one video. The player takes the
// screen the way an app does, and gives it back when the video ends.
func (s *Server) handleVideoPlay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil || req.Path == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.videos == nil {
		http.Error(w, "no videos are set up", http.StatusNotFound)
		return
	}
	argv, window, err := s.videos.Play(r.Context(), req.Path)
	switch {
	case errors.Is(err, videos.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case err != nil:
		s.log.Warn("video", "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// A video that is still playing makes way for the next.
	if s.apps.Status().Running == videos.AppID {
		_ = s.apps.Stop(r.Context())
	}
	if err := videos.SetPending(argv); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.apps.SetHelpers(config.App{ID: videos.AppID, Name: "the video player", Command: argv[0], Mode: config.AppModeDesktop, Window: window})
	if err := s.apps.Start(r.Context(), videos.AppID); err != nil {
		s.log.Error("video player", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.log.Info("playing a video", "player", window)
	writeJSON(w, s.apps.Status())
}

// handleVideoStream passes a video of the NAS on to the player. The address
// carries a signature only PiDash can make, and is only given to the player.
func (s *Server) handleVideoStream(w http.ResponseWriter, r *http.Request) {
	var streamer videos.Streamer
	if s.videos != nil {
		streamer, _ = s.videos.Source.(videos.Streamer)
	}
	if streamer == nil || !fromThisMachine(r) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	streamer.Stream(w, r, r.PathValue("token"), r.PathValue("name"))
}
