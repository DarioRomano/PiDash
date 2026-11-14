package server

import (
	"net/http"

	"github.com/DarioRomano/PiDash/internal/wifi"
)

// handleWifi returns the network for visitors with the code to scan. This is
// the one place a password is given out, because showing it is the point: it
// is what a visitor in the living room is meant to see on the TV.
func (s *Server) handleWifi(w http.ResponseWriter, _ *http.Request) {
	net := s.cfg.Wifi
	if net.Name == "" {
		http.Error(w, "no Wi-Fi for visitors is set up", http.StatusNotFound)
		return
	}
	rows, err := wifi.Code(net.Name, net.Password, net.Hidden)
	if err != nil {
		s.log.Warn("guest wifi: cannot make the code", "err", err)
		http.Error(w, "the code could not be made", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, struct {
		Name     string   `json:"name"`
		Password string   `json:"password"`
		Code     []string `json:"code"`
	}{net.Name, net.Password, rows})
}
