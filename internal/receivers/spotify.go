package receivers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DarioRomano/PiDash/internal/playing"
)

// SocketEnv names the place librespot's messenger (this program, started
// with -speaker-event) delivers what librespot told it.
const SocketEnv = "PIDASH_SPEAKER_SOCKET"

// What librespot puts into the environment of the program it calls when
// something happens, and what of it is passed on.
var spotifyFields = []string{"PLAYER_EVENT", "TRACK_ID", "NAME", "ARTISTS", "ALBUM", "COVERS", "SHOW_NAME"}

// SendEvent is what librespot's messenger does: it passes on what librespot
// put into its environment, and is done.
func SendEvent() error {
	socket := os.Getenv(SocketEnv)
	if socket == "" {
		return errors.New("not started by PiDash's Spotify speaker")
	}
	event := map[string]string{}
	for _, k := range spotifyFields {
		if v := os.Getenv(k); v != "" {
			event[k] = v
		}
	}
	conn, err := net.DialTimeout("unix", socket, 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	return json.NewEncoder(conn).Encode(event)
}

// runSpotify keeps librespot running and listens to its messenger.
func (s *Service) runSpotify(ctx context.Context) {
	bin, err := s.opt.LookPath(spotifyProgram)
	if err != nil {
		return
	}
	socket := filepath.Join(s.opt.Dir, "spotify.sock")
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		s.opt.Log.Warn("Spotify speaker: cannot listen for what is playing", "err", err)
	} else {
		_ = os.Chmod(socket, 0o600)
		go func() {
			<-ctx.Done()
			ln.Close()
			_ = os.Remove(socket)
		}()
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go func() {
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
					var event map[string]string
					if json.NewDecoder(io.LimitReader(conn, 64<<10)).Decode(&event) == nil {
						s.spotifyEvent(ctx, event)
					}
				}()
			}
		}()
	}
	// Through the desktop's sound where librespot can, so that the sound
	// goes where PiDash's settings send it.
	backend := ""
	if strings.Contains(output(ctx, bin, "--backend", "?"), "pulseaudio") {
		backend = "pulseaudio"
	}
	cache := filepath.Join(s.opt.Dir, "spotify")
	s.keep(ctx, "Spotify", &s.spotify, func() (*exec.Cmd, error) {
		args := []string{"--name", s.opt.Name, "--device-type", "tv", "--bitrate", "320",
			"--cache", cache, "--disable-audio-cache", "--initial-volume", "80"}
		if backend != "" {
			args = append(args, "--backend", backend)
		}
		if s.opt.Exe != "" && ln != nil {
			args = append(args, "--onevent", s.opt.Exe+" -speaker-event")
		}
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = append(os.Environ(), SocketEnv+"="+socket)
		return cmd, nil
	}, func() {
		s.mu.Lock()
		s.spotifyTrack, s.spotifyArt = nil, ""
		s.mu.Unlock()
	})
}

// spotifyEvent takes in what librespot said happened.
func (s *Service) spotifyEvent(ctx context.Context, e map[string]string) {
	lines := func(v string) string {
		var parts []string
		for _, p := range strings.Split(v, "\n") {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
		return strings.Join(parts, ", ")
	}
	var cover string
	s.mu.Lock()
	switch e["PLAYER_EVENT"] {
	case "track_changed":
		// What the song is called. Whether it plays is said separately.
		was := s.spotifyTrack
		t := &playing.Track{Player: "Spotify", Title: strings.TrimSpace(e["NAME"]), Artist: lines(e["ARTISTS"]), Album: strings.TrimSpace(e["ALBUM"]),
			Playing: was == nil || was.Playing}
		if t.Artist == "" {
			t.Artist = strings.TrimSpace(e["SHOW_NAME"]) // a podcast
		}
		s.spotifyTrack, s.spotifyArt = t, ""
		if s.spotifySong == nil || len(s.spotifySong) > 50 {
			s.spotifySong = map[string]string{}
		}
		s.spotifySong[e["TRACK_ID"]] = t.Title
		cover = strings.TrimSpace(strings.SplitN(e["COVERS"], "\n", 2)[0])
		s.latest = "spotify"
	case "playing", "start", "change":
		if s.spotifyTrack == nil {
			s.spotifyTrack = &playing.Track{Player: "Spotify", Title: s.spotifySong[e["TRACK_ID"]]}
		}
		s.spotifyTrack.Playing = true
		s.latest = "spotify"
	case "paused":
		if s.spotifyTrack != nil {
			s.spotifyTrack.Playing = false
		}
	case "stopped", "stop", "session_disconnected":
		s.spotifyTrack, s.spotifyArt = nil, ""
	default:
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.changed()
	if cover != "" {
		go s.spotifyCover(ctx, cover)
	}
}

// spotifyCover fetches the cover Spotify named and keeps it as a file: the
// dashboard's pages load pictures from the Pi only.
func (s *Service) spotifyCover(ctx context.Context, address string) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || !(strings.HasSuffix(u.Hostname(), ".scdn.co") || strings.HasSuffix(u.Hostname(), ".spotifycdn.com")) {
		return // not one of Spotify's pictures
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil || !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return
	}
	sum := sha1.Sum([]byte(address))
	mark := hex.EncodeToString(sum[:6])
	path, err := s.coverFile("spotify", data, mark)
	if err != nil {
		return
	}
	s.mu.Lock()
	if s.spotifyTrack != nil {
		s.spotifyArt = path
		s.spotifyTrack.Art = "/api/playing/art?v=" + mark
	}
	s.mu.Unlock()
	s.changed()
}
