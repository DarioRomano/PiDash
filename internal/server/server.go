// Package server exposes the dashboard over HTTP: the embedded frontend,
// a Server-Sent Events stream of widget state and remote-control input,
// and a small JSON API used by the phone remote.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/providers"
)

// Server is the PiDash HTTP server.
type Server struct {
	cfg    config.Config
	log    *slog.Logger
	hub    *hub
	runner *providers.Runner
	kiosk  *kiosk.Controller
	static fs.FS
	mux    *http.ServeMux
}

// New wires the HTTP handlers. static is the built frontend (web/dist).
func New(cfg config.Config, log *slog.Logger, static fs.FS, k *kiosk.Controller, newRunner func(publish func(providers.Entry)) *providers.Runner) *Server {
	s := &Server{cfg: cfg, log: log, hub: newHub(), kiosk: k, static: static, mux: http.NewServeMux()}
	s.runner = newRunner(func(e providers.Entry) { s.hub.broadcast("state", e) })

	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /events", s.handleEvents)
	s.mux.HandleFunc("GET /api/config", s.handleConfig)
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("POST /api/input", s.handleInput)
	s.mux.HandleFunc("GET /api/kiosk", s.handleKioskStatus)
	s.mux.HandleFunc("POST /api/kiosk", s.handleKioskControl)
	s.mux.HandleFunc("GET /remote", s.servePage("remote.html"))
	s.mux.HandleFunc("GET /remote/", s.servePage("remote.html"))
	s.mux.Handle("GET /", http.FileServerFS(static))
	return s
}

// Run starts the provider runner and serves HTTP until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	go s.runner.Run(ctx)

	srv := &http.Server{
		Addr:              s.cfg.Server.Listen,
		Handler:           s.withLogging(s.mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ln, err := net.Listen("tcp", s.cfg.Server.Listen)
	if err != nil {
		return err
	}
	s.log.Info("listening", "addr", ln.Addr().String())

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "clients": s.hub.count()})
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.cfg.ForClient())
}

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.runner.Snapshot())
}

// inputRequest is the body of POST /api/input.
type inputRequest struct {
	Action string `json:"action"`
	// Text is optional free text, e.g. typed on the phone remote.
	Text string `json:"text,omitempty"`
}

// inputEvent is what the dashboard receives on the SSE stream.
type inputEvent struct {
	Action string `json:"action"`
	Text   string `json:"text,omitempty"`
	Source string `json:"source"`
}

func (s *Server) handleInput(w http.ResponseWriter, r *http.Request) {
	var req inputRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.Action == "" || len(req.Action) > 32 {
		http.Error(w, "missing action", http.StatusBadRequest)
		return
	}
	if len(req.Text) > 512 {
		http.Error(w, "text too long", http.StatusBadRequest)
		return
	}
	s.hub.broadcast("input", inputEvent{Action: req.Action, Text: req.Text, Source: "remote"})
	w.WriteHeader(http.StatusNoContent)
}

// kioskStatus is the response of GET and POST /api/kiosk.
type kioskStatus struct {
	// Available is false when kiosk control is disabled in the config.
	Available bool `json:"available"`
	// Active is true while the dashboard is showing on the TV.
	Active bool `json:"active"`
}

func (s *Server) handleKioskStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, kioskStatus{Available: s.kiosk.Available(), Active: s.kiosk.Active(r.Context())})
}

// handleKioskControl starts ("start") or stops ("stop") the fullscreen browser.
func (s *Server) handleKioskControl(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	var err error
	active := false
	switch req.Action {
	case "start":
		err, active = s.kiosk.Start(r.Context()), true
	case "stop":
		err = s.kiosk.Stop(r.Context())
	default:
		http.Error(w, `action must be "start" or "stop"`, http.StatusBadRequest)
		return
	}
	switch {
	case errors.Is(err, kiosk.ErrUnavailable):
		http.Error(w, err.Error(), http.StatusNotImplemented)
		return
	case err != nil:
		s.log.Error("kiosk control", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The unit starts asynchronously, so report the requested state.
	writeJSON(w, kioskStatus{Available: true, Active: active})
}

// handleEvents streams config, current state and subsequent updates as SSE.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	write := func(name string, data []byte) error {
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data); err != nil {
			return err
		}
		fl.Flush()
		return nil
	}
	send := func(name string, v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return write(name, b)
	}

	// Initial burst: config, then the current state of every provider.
	if err := send("config", s.cfg.ForClient()); err != nil {
		return
	}
	for _, e := range s.runner.Snapshot() {
		if err := send("state", e); err != nil {
			return
		}
	}

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if err := write(ev.name, ev.data); err != nil {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

// servePage serves a single file from the embedded frontend regardless of path,
// so that /remote shows remote.html.
func (s *Server) servePage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(s.static, name)
		if err != nil {
			http.Error(w, "frontend not built: run `make build-web`", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(b)
	}
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/events" || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start).Round(time.Millisecond))
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(v)
}
