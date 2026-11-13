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
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DarioRomano/PiDash/internal/apps"
	"github.com/DarioRomano/PiDash/internal/bluetooth"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/hass"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/logring"
	"github.com/DarioRomano/PiDash/internal/photos"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/providers/calendar"
	"github.com/DarioRomano/PiDash/internal/providers/weather"
	"github.com/DarioRomano/PiDash/internal/radio"
	"github.com/DarioRomano/PiDash/internal/settings"
	"github.com/DarioRomano/PiDash/internal/tv"
	"github.com/DarioRomano/PiDash/internal/update"
)

// Server is the PiDash HTTP server.
type Server struct {
	cfg        config.Config
	log        *slog.Logger
	hub        *hub
	runner     *providers.Runner
	kiosk      *kiosk.Controller
	apps       *apps.Manager
	photos     *photos.Service // nil when no photo source is configured
	input      *input.Service
	tv         *tv.Service
	updater    *update.Service
	remote     *input.Remote
	bluetooth  bluetooth.Manager
	problems   *logring.Ring
	photoState string
	pin        pinGuard
	version    string
	urlFile    string
	static     fs.FS

	noticeMu sync.Mutex
	notices  []string

	settings   *settings.Store
	baseListen string
	// weatherLookup replaces the weather service in tests.
	weatherLookup func(ctx context.Context, location string) (string, error)
	started       int64
	restart       chan struct{}
	restartOne    sync.Once
	mux           *http.ServeMux
}

// Deps are the services the HTTP server exposes.
type Deps struct {
	Kiosk  *kiosk.Controller
	Apps   *apps.Manager
	Photos *photos.Service // nil when no photo source is configured
	Input  *input.Service
	TV     *tv.Service
	// TestHooks adds endpoints the end-to-end tests need. Never set outside
	// development (-mock).
	TestHooks bool
	// PhotoStatePath is where the slideshow keeps its chosen folders, for
	// restoring a backup while no photo source is set up.
	PhotoStatePath string
	// Updater looks for and installs new releases; may be nil.
	Updater *update.Service
	// Remote reads the TV remote and game controllers; nil when it does not
	// (-mock). Problems holds the latest warnings; may be nil. Both are for
	// the status page.
	Remote   *input.Remote
	Problems *logring.Ring
	// Bluetooth manages the Pi's Bluetooth devices; may be nil.
	Bluetooth bluetooth.Manager
	// Version is the PiDash version, shown on the phone remote.
	Version string
	// Notices are start-up problems to show on the TV.
	Notices []string
	// URLFile is where the address actually in use is recorded for the
	// launcher; empty to skip.
	URLFile string
	// Settings is what the settings page reads and writes.
	Settings *settings.Store
	// BaseListen is the address config.toml asks for, before any remembered
	// port fallback.
	BaseListen string
	// NewRunner builds the provider runner around the server's publish function.
	NewRunner func(publish func(providers.Entry)) *providers.Runner
}

// New wires the HTTP handlers. static is the built frontend (web/dist).
func New(cfg config.Config, log *slog.Logger, static fs.FS, d Deps) *Server {
	k, am, ps, newRunner := d.Kiosk, d.Apps, d.Photos, d.NewRunner
	s := &Server{cfg: cfg, log: log, hub: newHub(), kiosk: k, apps: am, photos: ps, input: d.Input, tv: d.TV,
		pin: pinGuard{pin: cfg.Remote.PIN}, version: d.Version, urlFile: d.URLFile, notices: d.Notices,
		settings: d.Settings, baseListen: d.BaseListen, started: time.Now().UnixMilli(), restart: make(chan struct{}),
		static: static, mux: http.NewServeMux()}
	s.runner = newRunner(func(e providers.Entry) { s.hub.broadcast("state", e) })

	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /events", s.handleEvents)
	s.mux.HandleFunc("GET /api/config", s.handleConfig)
	s.mux.HandleFunc("GET /api/state", s.handleState)
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/bluetooth", s.handleBluetooth)
	s.mux.HandleFunc("POST /api/bluetooth/{action}", s.handleBluetoothAction)
	s.mux.HandleFunc("POST /api/input", s.handleInput)
	if d.TestHooks {
		// For the end-to-end tests: a button press as if read from the TV remote.
		s.mux.HandleFunc("POST /api/test/remote", func(w http.ResponseWriter, r *http.Request) {
			var req inputRequest
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || req.Action == "" {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			s.RemoteAction(req.Action)
			w.WriteHeader(http.StatusNoContent)
		})
	}
	s.mux.HandleFunc("GET /api/kiosk", s.handleKioskStatus)
	s.mux.HandleFunc("POST /api/kiosk", s.handleKioskControl)
	am.OnChange(func(st apps.Status) { s.hub.broadcast("apps", st) })
	s.mux.HandleFunc("GET /api/apps", s.handleApps)
	s.mux.HandleFunc("POST /api/apps/start", s.handleAppStart)
	s.mux.HandleFunc("POST /api/apps/stop", s.handleAppStop)
	s.mux.HandleFunc("POST /api/apps/order", s.handleAppOrder)
	s.mux.HandleFunc("GET /api/apps/{id}/icon", s.handleAppIcon)
	if ps != nil {
		ps.OnChange(func(st photos.Status) { s.hub.broadcast("photos", st) })
	}
	s.mux.HandleFunc("GET /api/photos", s.handlePhotos)
	s.mux.HandleFunc("GET /api/photos/folders", s.handlePhotoFolders)
	s.mux.HandleFunc("PUT /api/photos/selection", s.handlePhotoSelection)
	s.mux.HandleFunc("GET /api/photos/playlist", s.handlePhotoPlaylist)
	s.mux.HandleFunc("GET /videos/{key}", s.handleVideo)
	s.mux.HandleFunc("POST /api/photos/unplayable", s.handleUnplayable)
	s.mux.HandleFunc("POST /api/photos/hide", s.handlePhotoHide)
	s.mux.HandleFunc("POST /api/photos/unhide", s.handlePhotoUnhide)
	s.mux.HandleFunc("GET /photos/{key}", s.handlePhotoImage)
	s.mux.HandleFunc("GET /photos", s.servePage("photos.html"))
	d.TV.OnChange(func(st tv.Status) { s.hub.broadcast("tv", st) })
	s.mux.HandleFunc("GET /api/remote", s.handleRemoteInfo)
	s.mux.HandleFunc("POST /api/pointer", s.handlePointer)
	s.mux.HandleFunc("POST /api/type", s.handleType)
	s.mux.HandleFunc("POST /api/tv", s.handleTVPower)
	s.mux.HandleFunc("POST /api/tv/sleep", s.handleTVSleep)
	s.mux.HandleFunc("GET /api/radio/search", s.handleRadioSearch)
	s.photoState = d.PhotoStatePath
	s.mux.HandleFunc("POST /api/settings/backup", s.handleBackup)
	s.mux.HandleFunc("POST /api/settings/restore", s.handleRestore)
	s.mux.HandleFunc("GET /api/settings", s.handleSettings)
	s.mux.HandleFunc("PUT /api/settings", s.handleSettingsSave)
	s.mux.HandleFunc("POST /api/settings/test-synology", s.handleTestSynology)
	s.mux.HandleFunc("POST /api/settings/test-weather", s.handleTestWeather)
	s.mux.HandleFunc("POST /api/settings/test-calendar", s.handleTestCalendar)
	s.mux.HandleFunc("POST /api/settings/home-entities", s.handleHomeEntities)
	s.mux.HandleFunc("POST /api/home/activate", s.handleHomeActivate)
	s.updater = d.Updater
	s.remote, s.problems, s.bluetooth = d.Remote, d.Problems, d.Bluetooth
	s.mux.HandleFunc("GET /api/update", s.handleUpdate)
	s.mux.HandleFunc("POST /api/update/check", s.handleUpdate)
	s.mux.HandleFunc("POST /api/update/install", s.handleUpdateInstall)
	s.mux.HandleFunc("GET /settings", s.servePage("settings.html"))
	s.mux.HandleFunc("GET /remote", s.servePage("remote.html"))
	s.mux.HandleFunc("GET /remote/", s.servePage("remote.html"))
	s.mux.Handle("GET /", http.FileServerFS(static))
	return s
}

// ErrRestart is returned by Run when the backend should start again to apply
// saved settings.
var ErrRestart = errors.New("restart requested")

// requestRestart makes Run return ErrRestart.
func (s *Server) requestRestart() {
	s.restartOne.Do(func() { close(s.restart) })
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
		// Requests end when the server is asked to stop. Without this the
		// dashboard's open event stream would hold up every restart.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	ln, wanted, port, err := listen(s.cfg.Server.Listen)
	if err != nil {
		return err
	}
	s.log.Info("listening", "addr", ln.Addr().String())
	if port != wanted {
		s.log.Warn("the configured port is used by another program; using the next free one", "configured", wanted, "using", port)
		// Remember the port, so PiDash goes straight to it from now on and
		// this is said once, not on every start.
		host, _, _ := net.SplitHostPort(s.cfg.Server.Listen)
		remembered := s.settings != nil && s.settings.RememberPort(s.baseListen, net.JoinHostPort(host, strconv.Itoa(port))) == nil
		msg := fmt.Sprintf("Port %d is used by another program on this Pi, so PiDash now uses port %d. The phone remote is at port %d.", wanted, port, port)
		if !remembered {
			msg += fmt.Sprintf(" To keep it there, set listen = \":%d\" under [server] in config.toml.", port)
		}
		s.addNotice(msg)
	}
	// Tell the launcher where the dashboard really is.
	if err := writeURLFile(s.urlFile, port); err != nil {
		s.log.Warn("could not record the dashboard address", "file", s.urlFile, "err", err)
	} else if s.urlFile != "" {
		defer os.Remove(s.urlFile)
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case <-s.restart:
		// Saved settings are applied by starting afresh.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return ErrRestart
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
	writeJSON(w, map[string]any{"ok": true, "clients": s.hub.count(), "version": s.version})
}

func (s *Server) addNotice(text string) {
	s.noticeMu.Lock()
	s.notices = append(s.notices, text)
	s.noticeMu.Unlock()
}

// clientConfig is what the dashboard needs to know about the settings.
func (s *Server) clientConfig() config.Client {
	c := s.cfg.ForClient()
	c.Version = s.version
	c.Started = s.started
	s.noticeMu.Lock()
	c.Notices = append([]string{}, s.notices...)
	s.noticeMu.Unlock()
	return c
}

func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.clientConfig())
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

// RemoteAction delivers a button press read directly from the TV remote to
// the dashboard. The dashboard drops it if the browser reported the same
// press itself.
func (s *Server) RemoteAction(action string) {
	s.hub.broadcast("input", inputEvent{Action: action, Source: "evdev"})
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

// handleAppOrder saves a new order of the app row, after a tile was moved
// with the TV remote. It changes the settings, so it needs the settings PIN
// when there is one.
func (s *Server) handleAppOrder(w http.ResponseWriter, r *http.Request) {
	if !s.pin.allow(w, r) {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	// The same apps in another order, or nothing is changed.
	now := s.apps.Status().Apps
	have := map[string]bool{}
	for _, a := range now {
		have[a.ID] = true
	}
	for _, id := range req.IDs {
		if !have[id] {
			http.Error(w, settings.ErrOrder.Error(), http.StatusConflict)
			return
		}
		delete(have, id)
	}
	if len(req.IDs) != len(now) {
		http.Error(w, settings.ErrOrder.Error(), http.StatusConflict)
		return
	}
	if s.settings != nil {
		if err := s.settings.ReorderApps(req.IDs); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, settings.ErrOrder) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
	}
	s.apps.Reorder(req.IDs)
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

// settingsInfo is what the settings page loads.
type settingsInfo struct {
	settings.View
	// ConsoleReady says whether programs that run outside the desktop can start.
	ConsoleReady bool `json:"consoleReady"`
	// PINRequired asks the page to send the PIN when saving.
	PINRequired bool   `json:"pinRequired"`
	Version     string `json:"version"`
	// KeyboardProblem explains why TV remote buttons cannot be passed to apps, if so.
	KeyboardProblem string `json:"keyboardProblem,omitempty"`
}

func (s *Server) handleSettings(w http.ResponseWriter, _ *http.Request) {
	if s.settings == nil {
		http.Error(w, "settings are not available", http.StatusNotFound)
		return
	}
	writeJSON(w, settingsInfo{
		View: s.settings.View(), ConsoleReady: s.apps.Status().ConsoleReady,
		PINRequired: s.pin.pin != "", Version: s.version, KeyboardProblem: s.input.KeyboardProblem(),
	})
}

// handleSettingsSave stores what the settings page sends and restarts the
// backend so it takes effect.
func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		http.Error(w, "settings are not available", http.StatusNotFound)
		return
	}
	if !s.pin.allow(w, r) {
		return
	}
	var u settings.Update
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&u); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := s.settings.Save(u); err != nil {
		// The message says which value is wrong, in words meant for the page.
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	s.log.Info("settings saved; restarting to apply them")
	writeJSON(w, map[string]bool{"restarting": true})
	// Let the answer reach the page before the server goes away.
	time.AfterFunc(400*time.Millisecond, s.requestRestart)
}

// handleTestSynology tries to sign in to the NAS with the values on the page.
func (s *Server) handleTestSynology(w http.ResponseWriter, r *http.Request) {
	if !s.pin.allow(w, r) {
		return
	}
	var req settings.Synology
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.Password == "" && s.settings != nil {
		req.Password = s.settings.SynologyPassword() // "unchanged"
	}
	if strings.TrimSpace(req.URL) == "" || req.Username == "" || req.Password == "" {
		http.Error(w, "Enter the address, user name and password first.", http.StatusUnprocessableEntity)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	nas := photos.NewSynology(photos.SynologyOptions{URL: strings.TrimSpace(req.URL), Username: req.Username, Password: req.Password, InsecureTLS: req.InsecureTLS})
	libs, err := nas.Folders(ctx, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	names := make([]string, 0, len(libs))
	for _, l := range libs {
		names = append(names, l.Name)
	}
	writeJSON(w, map[string]any{"ok": true, "libraries": names})
}

// handleUpdate reports the installed and the newest version; as a POST it
// looks for a new one first.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		http.Error(w, "updates are not available", http.StatusNotFound)
		return
	}
	if r.Method == http.MethodPost {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		st, _ := s.updater.Check(ctx) // a failure is part of the status
		writeJSON(w, st)
		return
	}
	writeJSON(w, s.updater.Status())
}

// handleUpdateInstall installs the newest release now.
func (s *Server) handleUpdateInstall(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		http.Error(w, "updates are not available", http.StatusNotFound)
		return
	}
	if !s.pin.allow(w, r) {
		return
	}
	if s.apps.Status().Running != "" {
		http.Error(w, "Close the open app first; the update restarts PiDash.", http.StatusConflict)
		return
	}
	if err := s.updater.Install(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.updater.Status())
}

// handleHomeEntities connects to Home Assistant and lists what can be shown
// as a tile, for the settings page. An empty token means the stored one.
func (s *Server) handleHomeEntities(w http.ResponseWriter, r *http.Request) {
	if !s.pin.allow(w, r) {
		return
	}
	var req struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		req.Token = s.cfg.Home.Token
	}
	if strings.TrimSpace(req.URL) == "" || req.Token == "" {
		http.Error(w, "Enter the address and the access token first.", http.StatusUnprocessableEntity)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	entities, err := hass.New(req.URL, req.Token).All(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"entities": entities})
}

// handleHomeActivate switches one of the configured Home Assistant tiles.
// Only what was put on the app row can be switched from here.
func (s *Server) handleHomeActivate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if !s.cfg.Home.Enabled() || !slices.Contains(s.cfg.Home.Entities, req.ID) {
		http.Error(w, "this is not one of the tiles", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := hass.New(s.cfg.Home.URL, s.cfg.Home.Token).Activate(ctx, req.ID)
	switch {
	case errors.Is(err, hass.ErrNoAction):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Show the new state without waiting for the next regular look; Home
	// Assistant needs a moment before it reports it.
	go func() {
		time.Sleep(600 * time.Millisecond)
		rctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s.runner.Refresh(rctx, "home")
	}()
	w.WriteHeader(http.StatusNoContent)
}

// handleTestCalendar reads a calendar link, to confirm it before saving.
func (s *Server) handleTestCalendar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		http.Error(w, "Enter the calendar's link first.", http.StatusUnprocessableEntity)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	days := s.cfg.Calendar.Days
	if days <= 0 {
		days = 14
	}
	n, err := calendar.New(calendar.Options{Days: days}).Count(ctx, req.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "events": n, "days": days})
}

// handleTestWeather looks a place name up, to confirm it before saving.
func (s *Server) handleTestWeather(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Location string `json:"location"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || strings.TrimSpace(req.Location) == "" {
		http.Error(w, "Enter a place first.", http.StatusUnprocessableEntity)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	lookup := s.weatherLookup
	if lookup == nil {
		lookup = weather.Lookup
	}
	label, err := lookup(ctx, strings.TrimSpace(req.Location))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "place": label})
}

// remoteInfo tells the phone remote which extra controls to show.
type remoteInfo struct {
	Input   input.Status `json:"input"`
	TV      tv.Status    `json:"tv"`
	Version string       `json:"version"`
}

func (s *Server) handleRemoteInfo(w http.ResponseWriter, _ *http.Request) {
	in := s.input.Status()
	in.PINRequired = in.Enabled && s.pin.pin != ""
	writeJSON(w, remoteInfo{Input: in, TV: s.tv.Status(), Version: s.version})
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

// backup is the content of a backup file.
type backup struct {
	// PiDash marks the file, so that some other JSON file is not mistaken for one.
	PiDash   string        `json:"pidash"`
	Version  string        `json:"version"`
	Settings settings.File `json:"settings"`
	Photos   *photos.State `json:"photos,omitempty"`
}

// handleBackup returns everything set up on the settings page as one file.
// It contains the passwords and links stored on the Pi, so it asks for the
// PIN when there is one. A POST, so that the PIN can be sent along.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		http.Error(w, "settings are not available", http.StatusNotFound)
		return
	}
	if !s.pin.allow(w, r) {
		return
	}
	b := backup{PiDash: "backup", Version: s.version, Settings: s.settings.Export()}
	if s.photos != nil {
		st := s.photos.Export()
		b.Photos = &st
	}
	w.Header().Set("Content-Disposition", `attachment; filename="pidash-backup.json"`)
	writeJSON(w, b)
}

// handleRestore replaces the settings with those of a backup file and
// restarts to apply them.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	if s.settings == nil {
		http.Error(w, "settings are not available", http.StatusNotFound)
		return
	}
	if !s.pin.allow(w, r) {
		return
	}
	var b backup
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&b); err != nil || b.PiDash != "backup" {
		http.Error(w, "This is not a PiDash backup file.", http.StatusUnprocessableEntity)
		return
	}
	if err := s.settings.Import(b.Settings); err != nil {
		http.Error(w, "The backup does not fit this Pi: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if b.Photos != nil {
		if err := photos.WriteState(s.photoState, *b.Photos); err != nil {
			s.log.Warn("restoring the photo selection", "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
	// Let the answer reach the page before restarting.
	time.AfterFunc(400*time.Millisecond, s.requestRestart)
}

// handleRadioSearch looks radio stations up by name, for the settings page.
func (s *Server) handleRadioSearch(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	stations, err := radio.Search(ctx, r.URL.Query().Get("q"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"stations": stations})
}

// handleTVSleep sets the sleep timer: POST /api/tv/sleep {"minutes":60}; 0 cancels.
func (s *Server) handleTVSleep(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Minutes int `json:"minutes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || req.Minutes < 0 || req.Minutes > 600 {
		http.Error(w, "minutes must be between 0 and 600", http.StatusBadRequest)
		return
	}
	writeJSON(w, s.tv.SetSleep(req.Minutes))
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

// handleVideo serves a video clip of the slideshow. The browser asks for it
// in parts, which http.ServeFile takes care of.
func (s *Server) handleVideo(w http.ResponseWriter, r *http.Request) {
	if s.photos == nil {
		http.NotFound(w, r)
		return
	}
	path, name, err := s.photos.Video(r.Context(), r.PathValue("key"))
	switch {
	case errors.Is(err, photos.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		s.log.Warn("video", "err", err)
		http.Error(w, "the video is not available", http.StatusBadGateway)
		return
	}
	// The cached copy has no file extension to tell its kind by.
	kind := "video/mp4"
	if strings.HasSuffix(strings.ToLower(name), ".webm") {
		kind = "video/webm"
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

// handleUnplayable is told by the dashboard that a video clip did not play.
func (s *Server) handleUnplayable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil || req.Key == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.photos != nil {
		s.photos.Unplayable(req.Key)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handlePhotoHide takes photos out of the slideshow for good.
func (s *Server) handlePhotoHide(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || len(req.Keys) == 0 || len(req.Keys) > 4 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if s.photos == nil {
		http.Error(w, "no photo source is set up", http.StatusNotFound)
		return
	}
	if err := s.photos.Hide(req.Keys...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.photos.Status())
}

// handlePhotoUnhide brings all hidden photos back.
func (s *Server) handlePhotoUnhide(w http.ResponseWriter, _ *http.Request) {
	if s.photos == nil {
		http.Error(w, "no photo source is set up", http.StatusNotFound)
		return
	}
	if err := s.photos.ShowHidden(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	Place  string    `json:"place,omitempty"`
	Video  bool      `json:"video,omitempty"`
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
		version, slides := s.photos.Slides(limit)
		if folder := r.URL.Query().Get("folder"); folder != "" {
			// "More from this album": that folder's photos, in the order taken.
			version, slides = s.photos.SlidesFrom(folder, r.URL.Query().Get("after"), limit)
		}
		out.Version = version
		for _, slide := range slides {
			// A slide is one photo, or two upright ones side by side; the
			// caption comes from the first.
			first := slide.Items[0]
			name, versions := first.Name, ""
			for i, it := range slide.Items {
				versions += it.Version + "|"
				if i > 0 {
					name += " + " + it.Name
				}
			}
			sum := sha1.Sum([]byte(versions + s.photos.Variant()))
			out.Items = append(out.Items, playlistItem{Key: slide.Key, Name: name, Folder: first.Folder, Taken: first.Taken, Place: first.Place, Video: first.Video, V: hex.EncodeToString(sum[:4])})
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
	// After the backend restarts (saved settings, an update) pages should be
	// back in touch within a second, not the browser's default three: until
	// then the TV remote's OK and Back do not reach them.
	fmt.Fprint(w, "retry: 800\n\n")

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
	if err := send("config", s.clientConfig()); err != nil {
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
