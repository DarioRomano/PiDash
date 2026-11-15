// Package receivers makes the Pi a speaker that phones can play to: for
// Spotify (Spotify Connect) and for Apple devices (AirPlay).
//
// PiDash does not speak either protocol itself. Two well-known programs do,
// librespot and shairport-sync; where they are installed and the settings
// ask for it, PiDash runs them as its own, under the name it is given, with
// their sound going where the desktop's sound goes, and listens to what they
// say is playing, for the dashboard.
package receivers

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DarioRomano/PiDash/internal/playing"
)

// Options say which receivers are wanted, and under what name.
type Options struct {
	// Name is what the Pi is called in the Spotify app and on Apple devices.
	Name    string
	Spotify bool
	AirPlay bool
	// Dir is where the receivers keep what they need: PiDash's own folder.
	Dir string
	// Exe is this program, which librespot calls to say what is playing.
	Exe string
	Log *slog.Logger
	// OnChange is called whenever what is playing, or whether a receiver
	// runs, has changed.
	OnChange func()

	// For tests: where programs are looked for, and how long to wait before
	// starting one again that ended.
	LookPath func(string) (string, error)
	Again    time.Duration
}

// State is how one receiver is doing, for the settings page.
type State struct {
	// On is whether the settings ask for it.
	On bool `json:"on"`
	// Installed is whether its program is on the Pi.
	Installed bool `json:"installed"`
	Running   bool `json:"running"`
	// Problem says why it does not run though it should.
	Problem string `json:"problem,omitempty"`
}

// Status is both receivers.
type Status struct {
	Name    string `json:"name"`
	Spotify State  `json:"spotify"`
	AirPlay State  `json:"airplay"`
}

// Service runs the receivers.
type Service struct {
	opt Options

	mu      sync.Mutex
	spotify State
	airplay State
	// What each of them plays; nil for nothing. And which spoke last: with
	// two phones playing at once, the later one is what is shown.
	spotifyTrack *playing.Track
	airplayTrack *playing.Track
	airplayArt   string // the cover as a file, for AirPlay
	spotifyArt   string
	latest       string
	// The song librespot named last, by its id: "playing" only gives the id.
	spotifySong map[string]string
}

// The programs, as they are called on the Pi.
const (
	spotifyProgram = "librespot"
	airplayProgram = "shairport-sync"
)

// New prepares the receivers; Run starts them.
func New(opt Options) *Service {
	if opt.LookPath == nil {
		opt.LookPath = exec.LookPath
	}
	if opt.Again <= 0 {
		opt.Again = 10 * time.Second
	}
	if strings.TrimSpace(opt.Name) == "" {
		opt.Name = "PiDash"
	}
	s := &Service{opt: opt}
	s.spotify.On, s.airplay.On = opt.Spotify, opt.AirPlay
	_, errS := opt.LookPath(spotifyProgram)
	_, errA := opt.LookPath(airplayProgram)
	s.spotify.Installed, s.airplay.Installed = errS == nil, errA == nil
	return s
}

// Status reports how the receivers are doing.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Name: s.opt.Name, Spotify: s.spotify, AirPlay: s.airplay}
}

// Current returns what a phone is playing on the Pi, or nil.
func (s *Service) Current() *playing.Track {
	s.mu.Lock()
	defer s.mu.Unlock()
	pick := s.spotifyTrack
	if s.airplayTrack != nil && (pick == nil || s.latest == "airplay") {
		pick = s.airplayTrack
	}
	if pick == nil {
		return nil
	}
	t := *pick
	return &t
}

// ArtFile is the cover of what Current returns, when there is one.
func (s *Service) ArtFile() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.spotifyArt
	if s.airplayTrack != nil && (s.spotifyTrack == nil || s.latest == "airplay") {
		file = s.airplayArt
	}
	return file, file != ""
}

// Playing reports whether a phone is playing something right now.
func (s *Service) Playing() bool {
	t := s.Current()
	return t != nil && t.Playing
}

// OnChange sets what is called whenever what is playing, or whether a
// receiver runs, has changed. To be set before Run.
func (s *Service) OnChange(fn func()) { s.opt.OnChange = fn }

func (s *Service) changed() {
	if s.opt.OnChange != nil {
		s.opt.OnChange()
	}
}

// Run starts the receivers that are wanted and installed, and starts them
// again when they end, until ctx ends.
func (s *Service) Run(ctx context.Context) {
	if err := os.MkdirAll(s.opt.Dir, 0o700); err != nil {
		s.opt.Log.Warn("speakers for phones: no folder to keep their things in", "err", err)
		return
	}
	var wg sync.WaitGroup
	if s.opt.Spotify && s.spotify.Installed {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runSpotify(ctx)
		}()
	}
	if s.opt.AirPlay && s.airplay.Installed {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runAirPlay(ctx)
		}()
	}
	wg.Wait()
}

// keep runs a program again and again until ctx ends. command builds it
// anew each time; state is where its running is noted; ended is told when
// it has stopped, so that what it played is forgotten.
func (s *Service) keep(ctx context.Context, what string, state *State, command func() (*exec.Cmd, error), ended func()) {
	wait := s.opt.Again
	for ctx.Err() == nil {
		started := time.Now()
		cmd, err := command()
		var out tail
		if err == nil {
			cmd.Stdout, cmd.Stderr = &out, &out
			// Its own group, so that it ends with everything it started.
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
			cmd.WaitDelay = 3 * time.Second
			err = cmd.Start()
		}
		if err == nil {
			s.mu.Lock()
			state.Running, state.Problem = true, ""
			s.mu.Unlock()
			s.opt.Log.Info("speaker for phones started", "which", what, "name", s.opt.Name)
			s.changed()
			err = cmd.Wait()
		}
		if ctx.Err() != nil {
			return
		}
		problem := out.last()
		if problem == "" && err != nil {
			problem = err.Error()
		}
		s.mu.Lock()
		state.Running, state.Problem = false, problem
		s.mu.Unlock()
		ended()
		s.changed()
		s.opt.Log.Warn("speaker for phones ended; starting it again", "which", what, "said", problem)
		// One that keeps falling over is tried less and less often.
		if time.Since(started) > time.Minute {
			wait = s.opt.Again
		} else if wait < 16*s.opt.Again {
			wait *= 2
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// tail keeps the last lines a program printed, to say why it ended.
type tail struct {
	mu   sync.Mutex
	text []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.text = append(t.text, p...)
	if len(t.text) > 4096 {
		t.text = t.text[len(t.text)-4096:]
	}
	return len(p), nil
}

// last is the last line that says something.
func (t *tail) last() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(string(t.text)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			if len(line) > 300 {
				line = line[:300]
			}
			return line
		}
	}
	return ""
}

// output asks a program something and returns what it printed, whatever
// stream it printed it to and whether or not it ended well: these programs
// answer questions about themselves in their own ways.
func output(ctx context.Context, bin string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	return string(out)
}

// coverFile keeps a cover as a file in the receivers' folder and returns
// its path. The name changes with the picture, so that the page notices a
// new one; the one before is removed.
func (s *Service) coverFile(which string, data []byte, sum string) (string, error) {
	if len(data) == 0 {
		return "", errors.New("no picture")
	}
	old, _ := filepath.Glob(filepath.Join(s.opt.Dir, which+"-cover-*"))
	path := filepath.Join(s.opt.Dir, which+"-cover-"+sum)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	for _, o := range old {
		if o != path {
			_ = os.Remove(o)
		}
	}
	return path, nil
}
