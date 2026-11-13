// Package server exposes the dashboard over HTTP: the embedded frontend,
// a Server-Sent Events stream of widget state and remote-control input,
// and a small JSON API used by the phone remote.
package server

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DarioRomano/PiDash/internal/apps"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/photos"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/tv"
)

// Server is the PiDash HTTP server.
type Server struct {
	cfg    config.Config
	log    *slog.Logger
	hub    *hub
	runner *providers.Runner
	kiosk  *kiosk.Controller
	apps   *apps.Manager
	photos *photos.Service // nil when no photo source is configured
	input  *input.Service
	tv     *tv.Service
	pin    pinGuard
	static fs.FS
	mux    *http.ServeMux
}

// Deps are the services the HTTP server exposes.
type Deps struct {
	Kiosk  *kiosk.Controller
	Apps   *apps.Manager
	Photos *photos.Service // nil when no photo source is configured
	Input  *input.Service
	TV     *tv.Service
	// NewRunner builds the provider runner around the server's publish function.
	NewRunner func(publish func(providers.Entry)) *providers.Runner
}

// New wires the HTTP handlers. static is the built frontend (web/dist).
func New(cfg config.Config, log *slog.Logger, static fs.FS, d Deps) *Server {
	k, am, ps, newRunner := d.Kiosk, d.Apps, d.Photos, d.NewRunner
	s := &Server{cfg: cfg, log: log, hub: newHub(), kiosk: k, apps: am, photos: ps, input: d.Input, tv: d.TV,
		pin: pinGuard{pin: cfg.Remote.PIN}, static: static, mux: http.NewServeMux()}
	s.runner = newRunner(func(e providers.Entry) { s.hub.broadcast("state", e) })

	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /events", s.handleEvents)
	s.mux.HandleFunc("GET /api/config", s.handleConfig)
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("POST /api/input", s.handleInput)
	s.mux.HandleFunc("GET /api/kiosk", s.handleKioskStatus)
	s.mux.HandleFunc("POST /api/kiosk", s.handleKioskControl)
	am.OnChange(func(st apps.Status) { s.hub.broadcast("apps", st) })
	s.mux.HandleFunc("GET /api/apps", s.handleApps)
	s.mux.HandleFunc("POST /api/apps/start", s.handleAppStart)
	s.mux.HandleFunc("POST /api/apps/stop", s.handleAppStop)
	s.mux.HandleFunc("GET /api/apps/{id}/icon", s.handleAppIcon)
	if ps != nil {
		ps.OnChange(func(st photos.Status) { s.hub.broadcast("photos", st) })
	}
	s.mux.HandleFunc("GET /api/photos", s.handlePhotos)
	s.mux.HandleFunc("GET /api/photos/folders", s.handlePhotoFolders)
	s.mux.HandleFunc("PUT /api/photos/selection", s.handlePhotoSelection)
	s.mux.HandleFunc("GET /api/photos/playlist", s.handlePhotoPlaylist)
	s.mux.HandleFunc("GET /photos/{key}", s.handlePhotoImage)
	s.mux.HandleFunc("GET /photos", s.servePage("photos.html"))
	d.TV.OnChange(func(st tv.Status) { s.hub.broadcast("tv", st) })
	s.mux.HandleFunc("GET /api/remote", s.handleRemoteInfo)
	s.mux.HandleFunc("POST /api/pointer", s.handlePointer)
	s.mux.HandleFunc("POST /api/type", s.handleType)
	s.mux.HandleFunc("POST /api/tv", s.handleTVPower)
	s.mux.HandleFunc("GET /remote", s.servePage("remote.html"))
	s.mux.HandleFunc("GET /remote/", s.servePage("remote.html"))
	s.mux.Handle("GET /", http.FileServerFS(static))
	return s
}

// Run starts the provider runner and serves HTTP until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	go s.runner.Run(ctx)
	go s.apps.Run(ctx)
	go s.tv.Run(ctx)
	if s.photos != nil {
		go s.photos.Run(ctx)
	}

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
	// While an app has the screen the dashboard cannot act on the phone's
	// buttons, so they are pressed as real keys instead and reach the app.
	if s.apps.Status().Running != "" && s.input.Status().Available {
		if !s.pin.allow(w, r) {
			return
		}
		if s.input.Action(req.Action) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
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

func (s *Server) handleApps(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.apps.Status())
}

// handleAppStart launches an app: POST /api/apps/start {"id":"moonlight"}.
func (s *Server) handleAppStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	err := s.apps.Start(r.Context(), req.ID)
	switch {
	case errors.Is(err, apps.ErrUnknownApp):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case errors.Is(err, apps.ErrConsoleNotReady):
		http.Error(w, err.Error(), http.StatusPreconditionFailed)
		return
	case err != nil:
		s.log.Error("app start", "app", req.ID, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.apps.Status())
}

// handleAppStop stops the running app, which brings the dashboard back.
func (s *Server) handleAppStop(w http.ResponseWriter, r *http.Request) {
	if err := s.apps.Stop(r.Context()); err != nil {
		s.log.Error("app stop", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.apps.Status())
}

func (s *Server) handleAppIcon(w http.ResponseWriter, r *http.Request) {
	icon, ok := s.apps.Icon(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "max-age=3600")
	if icon.Data != nil {
		w.Header().Set("Content-Type", icon.ContentType)
		w.Write(icon.Data)
		return
	}
	http.ServeFile(w, r, icon.Path)
}

// remoteInfo tells the phone remote which extra controls to show.
type remoteInfo struct {
	Input input.Status `json:"input"`
	TV    tv.Status    `json:"tv"`
}

func (s *Server) handleRemoteInfo(w http.ResponseWriter, _ *http.Request) {
	in := s.input.Status()
	in.PINRequired = in.Enabled && s.pin.pin != ""
	writeJSON(w, remoteInfo{Input: in, TV: s.tv.Status()})
}

// inputError maps problems of the phone's mouse and keyboard to HTTP.
func (s *Server) inputError(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, input.ErrDisabled):
		http.Error(w, err.Error(), http.StatusForbidden)
	default:
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
}

// handlePointer moves the pointer, scrolls and clicks. The phone sends the
// movement it collected since its previous request.
func (s *Server) handlePointer(w http.ResponseWriter, r *http.Request) {
	if !s.pin.allow(w, r) {
		return
	}
	var req struct {
		DX, DY           int
		ScrollX, ScrollY int
		Click            string
		// Button and Down press or release a button, for dragging.
		Button string
		Down   *bool
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	var err error
	if req.DX != 0 || req.DY != 0 {
		err = s.input.Move(req.DX, req.DY)
	}
	if err == nil && (req.ScrollX != 0 || req.ScrollY != 0) {
		err = s.input.Scroll(req.ScrollX, req.ScrollY)
	}
	if err == nil && req.Button != "" && req.Down != nil {
		err = s.input.Button(req.Button, *req.Down)
	}
	if err == nil && req.Click != "" {
		err = s.input.Click(req.Click)
	}
	s.inputError(w, err)
}

// handleType types text, or presses one named key, in the focused app.
func (s *Server) handleType(w http.ResponseWriter, r *http.Request) {
	if !s.pin.allow(w, r) {
		return
	}
	var req struct {
		Text string `json:"text"`
		Key  string `json:"key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Key != "" {
		s.inputError(w, s.input.Key(req.Key))
		return
	}
	s.inputError(w, s.input.Type(r.Context(), req.Text))
}

// handleTVPower switches the TV on or off: POST /api/tv {"power":"off"}.
func (s *Server) handleTVPower(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Power string `json:"power"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || (req.Power != "on" && req.Power != "off") {
		http.Error(w, `power must be "on" or "off"`, http.StatusBadRequest)
		return
	}
	if err := s.tv.SetPower(r.Context(), req.Power == "on"); err != nil {
		s.log.Warn("tv power", "power", req.Power, "err", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, s.tv.Status())
}

func (s *Server) photoStatus() photos.Status {
	if s.photos == nil {
		return photos.Status{Selection: photos.Selection{Folders: []photos.Folder{}}}
	}
	return s.photos.Status()
}

func (s *Server) handlePhotos(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.photoStatus())
}

// handlePhotoFolders lists the folders inside ?parent= for the folder picker.
func (s *Server) handlePhotoFolders(w http.ResponseWriter, r *http.Request) {
	if s.photos == nil {
		http.Error(w, "no photo source is configured", http.StatusNotFound)
		return
	}
	folders, err := s.photos.Folders(r.Context(), r.URL.Query().Get("parent"))
	if err != nil {
		// The message is written for people: it says what is wrong with the NAS connection.
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"folders": folders})
}

// handlePhotoSelection saves which folders the background uses.
func (s *Server) handlePhotoSelection(w http.ResponseWriter, r *http.Request) {
	if s.photos == nil {
		http.Error(w, "no photo source is configured", http.StatusNotFound)
		return
	}
	var sel photos.Selection
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&sel); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := s.photos.SetSelection(sel); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, s.photos.Status())
}

// playlistItem is one photo as the dashboard sees it.
type playlistItem struct {
	Key    string    `json:"key"`
	Name   string    `json:"name"`
	Folder string    `json:"folder"`
	Taken  time.Time `json:"taken"`
	// V changes when the photo is edited, so the browser does not show a stale copy.
	V string `json:"v"`
}

func (s *Server) handlePhotoPlaylist(w http.ResponseWriter, r *http.Request) {
	out := struct {
		Version int            `json:"version"`
		Items   []playlistItem `json:"items"`
	}{Items: []playlistItem{}}
	if s.photos != nil {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 1000 {
			limit = 200
		}
		version, items := s.photos.Playlist(limit)
		out.Version = version
		for _, it := range items {
			sum := sha1.Sum([]byte(it.Version))
			out.Items = append(out.Items, playlistItem{Key: it.Key, Name: it.Name, Folder: it.Folder, Taken: it.Taken, V: hex.EncodeToString(sum[:4])})
		}
	}
	writeJSON(w, out)
}

// handlePhotoImage serves one photo as a ready-to-show 1920x1080 JPEG.
func (s *Server) handlePhotoImage(w http.ResponseWriter, r *http.Request) {
	if s.photos == nil {
		http.NotFound(w, r)
		return
	}
	path, err := s.photos.Image(r.Context(), r.PathValue("key"))
	switch {
	case errors.Is(err, photos.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		s.log.Warn("photo", "key", r.PathValue("key"), "err", err)
		http.Error(w, "could not load this photo", http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
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
	if err := send("apps", s.apps.Status()); err != nil {
		return
	}
	if err := send("photos", s.photoStatus()); err != nil {
		return
	}
	if err := send("tv", s.tv.Status()); err != nil {
		return
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
