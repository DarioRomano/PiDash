package server

import (
	"context"
	"net/http"
	"time"

	"github.com/DarioRomano/PiDash/internal/bluetooth"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/logring"
	"github.com/DarioRomano/PiDash/internal/update"
)

// statusReport is everything the status page on the TV shows, apart from
// the Pi's own readings, which the dashboard already receives as "system".
// It contains no secrets: no addresses of calendars, no passwords.
type statusReport struct {
	Version string `json:"version"`
	// Started is when the backend started, in milliseconds since 1970.
	Started int64          `json:"started"`
	Update  *update.Status `json:"update,omitempty"`
	TV      *tvReport      `json:"tv,omitempty"`
	// Remote is absent when the backend does not read input devices (-mock).
	Remote *remoteReport `json:"remote,omitempty"`
	Photos *photoReport  `json:"photos,omitempty"`
	// Sources are the things fetched regularly: weather, calendar, ….
	Sources []sourceReport `json:"sources"`
	Apps    appsReport     `json:"apps"`
	// Bluetooth lists the devices connected over Bluetooth right now.
	Bluetooth []bluetooth.Device `json:"bluetooth"`
	Notices   []string           `json:"notices"`
	// Problems are the latest warnings and errors from the log, newest first.
	Problems []logring.Line `json:"problems"`
}

type tvReport struct {
	// Control is true when the TV can be reached over HDMI-CEC.
	Control      bool       `json:"control"`
	Asleep       bool       `json:"asleep"`
	Off          string     `json:"off,omitempty"`
	On           string     `json:"on,omitempty"`
	SleepAt      *time.Time `json:"sleepAt,omitempty"`
	SleepMinutes int        `json:"sleepMinutes,omitempty"`
}

type remoteReport struct {
	// Devices are the remotes, keyboards and controllers being listened to.
	Devices []string     `json:"devices"`
	Last    *input.Heard `json:"last,omitempty"`
	// Keys says why buttons cannot be passed on to apps; empty when they can.
	Keys string `json:"keys,omitempty"`
	// Focus is true when PiDash can hand the keyboard to an app's window.
	Focus bool `json:"focus"`
}

type photoReport struct {
	Source   string `json:"source"`
	Count    int    `json:"count"`
	Indexing bool   `json:"indexing"`
	Hidden   int    `json:"hidden"`
	Error    string `json:"error,omitempty"`
}

type sourceReport struct {
	Name      string    `json:"name"`
	UpdatedAt time.Time `json:"updatedAt"`
	Stale     bool      `json:"stale"`
	Error     string    `json:"error,omitempty"`
}

type appsReport struct {
	Count   int    `json:"count"`
	Running string `json:"running,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	rep := statusReport{Version: s.version, Started: s.started, Sources: []sourceReport{}, Notices: []string{}, Problems: []logring.Line{}, Bluetooth: []bluetooth.Device{}}
	if s.bluetooth != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		for _, d := range s.bluetooth.Status(ctx).Devices {
			if d.Connected {
				rep.Bluetooth = append(rep.Bluetooth, d)
			}
		}
		cancel()
	}
	if s.updater != nil {
		st := s.updater.Status()
		rep.Update = &st
	}
	if s.tv != nil {
		st := s.tv.Status()
		rep.TV = &tvReport{Control: st.Available, Asleep: st.Asleep, Off: st.Off, On: st.On, SleepAt: st.SleepAt, SleepMinutes: st.SleepMinutes}
	}
	if s.remote != nil {
		r := &remoteReport{Devices: s.remote.Devices(), Focus: s.apps != nil && s.apps.CanFocus()}
		if last, ok := s.remote.Last(); ok {
			r.Last = &last
		}
		if s.input != nil {
			r.Keys = s.input.KeyboardProblem()
		}
		rep.Remote = r
	}
	if s.photos != nil {
		st := s.photos.Status()
		rep.Photos = &photoReport{Source: st.Source, Count: st.Count, Indexing: st.Indexing, Hidden: st.Hidden, Error: st.Error}
	}
	for _, e := range s.runner.Snapshot() {
		if e.Provider == "system" {
			continue
		}
		rep.Sources = append(rep.Sources, sourceReport{Name: e.Provider, UpdatedAt: e.UpdatedAt, Stale: e.Stale, Error: e.Error})
	}
	if s.apps != nil {
		st := s.apps.Status()
		rep.Apps = appsReport{Count: len(st.Apps), Running: st.Running, Error: st.Error}
	}
	s.noticeMu.Lock()
	rep.Notices = append(rep.Notices, s.notices...)
	s.noticeMu.Unlock()
	if s.problems != nil {
		rep.Problems = s.problems.Lines()
	}
	writeJSON(w, rep)
}
