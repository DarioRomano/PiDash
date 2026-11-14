package server

import (
	"context"
	"net/http"
	"time"
)

// handlePower restarts PiDash, restarts the Pi, or shuts the Pi down. These
// take the screen away from everyone, so they need the settings PIN when
// one is set.
func (s *Server) handlePower(w http.ResponseWriter, r *http.Request) {
	if !s.pin.allow(w, r) {
		return
	}
	action := r.PathValue("action")
	switch action {
	case "restart":
		// PiDash itself: the browser on the TV and this program. Answered
		// first; the restart follows once the answer is on its way.
		w.WriteHeader(http.StatusNoContent)
		go func() {
			time.Sleep(300 * time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if s.kiosk.Active(ctx) {
				if err := s.kiosk.Restart(ctx); err != nil {
					s.log.Warn("restarting the browser on the TV", "err", err)
				}
			}
			s.log.Info("restart requested from the TV")
			s.requestRestart()
		}()
		return
	case "reboot", "off":
		if s.power == nil {
			http.Error(w, "this is not available here", http.StatusNotFound)
			return
		}
		var err error
		if action == "reboot" {
			err = s.power.Reboot(r.Context())
		} else {
			err = s.power.Off(r.Context())
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}
