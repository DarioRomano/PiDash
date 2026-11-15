package receivers

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/playing"
)

// The test program doubles as librespot's messenger, as pidash does.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "-speaker-event" {
		if err := SendEvent(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// stage is a folder with pretend programs in it, and a short folder for the
// receivers' own things (a socket's path may not be long).
type stage struct {
	bin, dir, out string
}

func newStage(t *testing.T) stage {
	t.Helper()
	dir, err := os.MkdirTemp("", "pd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s := stage{bin: filepath.Join(dir, "bin"), dir: filepath.Join(dir, "sp"), out: filepath.Join(dir, "out")}
	for _, d := range []string{s.bin, s.out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (s stage) program(t *testing.T, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(s.bin, name), []byte("#!/bin/sh\nOUT="+s.out+"\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (s stage) look(name string) (string, error) {
	p := filepath.Join(s.bin, name)
	if _, err := os.Stat(p); err != nil {
		return "", exec.ErrNotFound
	}
	return p, nil
}

func (s stage) service(spotify, airplay bool, changed func()) *Service {
	return New(Options{Name: `Living "room"`, Spotify: spotify, AirPlay: airplay, Dir: s.dir, Exe: os.Args[0], Log: quiet,
		OnChange: changed, LookPath: s.look, Again: 50 * time.Millisecond})
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never happened: %s", what)
}

// A pretend librespot: says what it can, notes how it was started, and
// tells its messenger of a song that plays and is then held.
const pretendLibrespot = `
if [ "$1" = "--backend" ] && [ "$2" = "?" ]; then
  echo "Available backends: - alsa (default) - pulseaudio - pipe"; exit 0
fi
echo "$@" > "$OUT/librespot-args"
while [ $# -gt 0 ]; do [ "$1" = "--onevent" ] && ev="$2"; shift; done
PLAYER_EVENT=track_changed TRACK_ID=abc NAME="A song" ARTISTS="First
Second" ALBUM="The album" COVERS="https://example.org/not-spotify.jpg" $ev
PLAYER_EVENT=playing TRACK_ID=abc $ev
while [ ! -e "$OUT/hold" ]; do sleep 0.05; done
PLAYER_EVENT=paused TRACK_ID=abc $ev
while [ ! -e "$OUT/leave" ]; do sleep 0.05; done
PLAYER_EVENT=session_disconnected $ev
exec sleep 60
`

func TestTheSpotifySpeaker(t *testing.T) {
	st := newStage(t)
	st.program(t, "librespot", pretendLibrespot)
	var changes atomic.Int32
	s := st.service(true, true, func() { changes.Add(1) })
	// AirPlay is asked for too, but its program is not on this Pi.
	if got := s.Status(); !got.Spotify.Installed || got.AirPlay.Installed || got.Name != `Living "room"` {
		t.Fatalf("status = %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	until(t, "the song plays", func() bool { c := s.Current(); return c != nil && c.Playing && c.Title != "" })
	want := playing.Track{Player: "Spotify", Title: "A song", Artist: "First, Second", Album: "The album", Playing: true}
	if got := *s.Current(); got != want {
		t.Errorf("playing = %+v, want %+v", got, want)
	}
	if !s.Playing() || !s.Status().Spotify.Running {
		t.Errorf("not playing, or not running: %+v", s.Status())
	}
	// A cover that is not one of Spotify's own pictures is not fetched.
	if _, ok := s.ArtFile(); ok {
		t.Error("a picture from anywhere was fetched")
	}
	args, _ := os.ReadFile(filepath.Join(st.out, "librespot-args"))
	for _, part := range []string{`--name Living "room"`, "--device-type tv", "--backend pulseaudio", "--onevent " + os.Args[0] + " -speaker-event", "--cache " + filepath.Join(st.dir, "spotify")} {
		if !strings.Contains(string(args), part) {
			t.Errorf("librespot was started without %q: %s", part, args)
		}
	}

	os.WriteFile(filepath.Join(st.out, "hold"), nil, 0o644)
	until(t, "the song is held", func() bool { c := s.Current(); return c != nil && !c.Playing })
	if s.Playing() {
		t.Error("held music counts as playing")
	}
	os.WriteFile(filepath.Join(st.out, "leave"), nil, 0o644)
	until(t, "the phone has gone", func() bool { return s.Current() == nil })
	if changes.Load() < 4 {
		t.Errorf("the dashboard was told %d times", changes.Load())
	}

	// With PiDash, the speaker ends.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the speaker did not end")
	}
	if _, err := os.Stat(filepath.Join(st.dir, "spotify.sock")); err == nil {
		t.Error("the socket was left behind")
	}
}

// What shairport-sync writes into its pipe.
func airplaySays(kind, code, data string) string {
	item := "<item><type>" + hex.EncodeToString([]byte(kind)) + "</type><code>" + hex.EncodeToString([]byte(code)) + "</code><length>" + fmt.Sprint(len(data)) + "</length>"
	if data != "" {
		item += "\n<data encoding=\"base64\">\n" + base64.StdEncoding.EncodeToString([]byte(data)) + "</data>"
	}
	return item + "</item>\n"
}

func TestTheAirPlaySpeaker(t *testing.T) {
	st := newStage(t)
	// A PNG, as far as anyone looking at its first bytes can tell.
	cover := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 5000)
	said := airplaySays("ssnc", "pbeg", "") + airplaySays("core", "asal", "Blue") + airplaySays("core", "asar", "Joni Mitchell") +
		airplaySays("core", "minm", "A Case of You") + airplaySays("ssnc", "PICT", cover)
	os.WriteFile(filepath.Join(st.out, "said"), []byte(said), 0o644)
	os.WriteFile(filepath.Join(st.out, "held"), []byte(airplaySays("ssnc", "pfls", "")), 0o644)
	os.WriteFile(filepath.Join(st.out, "over"), []byte(airplaySays("ssnc", "pend", "")), 0o644)
	st.program(t, "shairport-sync", `
if [ "$1" = "-V" ]; then echo "4.3.2-OpenSSL-Avahi-ALSA-pa-soxr-metadata-sysconfdir:/etc"; exit 0; fi
cp "$2" "$OUT/conf"
pipe=$(sed -n 's/.*pipe_name = "\(.*\)";/\1/p' "$2")
cat "$OUT/said" > "$pipe"
while [ ! -e "$OUT/hold" ]; do sleep 0.05; done
cat "$OUT/held" > "$pipe"
while [ ! -e "$OUT/leave" ]; do sleep 0.05; done
cat "$OUT/over" > "$pipe"
exec sleep 60
`)
	s := st.service(false, true, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	until(t, "the song plays, with its cover", func() bool { c := s.Current(); return c != nil && c.Title != "" && c.Art != "" })
	got := *s.Current()
	if got.Player != "AirPlay" || got.Title != "A Case of You" || got.Artist != "Joni Mitchell" || got.Album != "Blue" || !got.Playing || !strings.HasPrefix(got.Art, "/api/playing/art?v=") {
		t.Errorf("playing = %+v", got)
	}
	file, ok := s.ArtFile()
	if data, _ := os.ReadFile(file); !ok || string(data) != cover {
		t.Errorf("the cover is not kept as it came: %q %v", file, ok)
	}
	conf, _ := os.ReadFile(filepath.Join(st.out, "conf"))
	for _, part := range []string{`name = "Living room";`, `output_backend = "pa";`, `include_cover_art = "yes";`, `pipe_name = "` + filepath.Join(st.dir, "airplay-metadata") + `";`} {
		if !strings.Contains(string(conf), part) {
			t.Errorf("shairport-sync's settings lack %q:\n%s", part, conf)
		}
	}

	os.WriteFile(filepath.Join(st.out, "hold"), nil, 0o644)
	until(t, "the song is held", func() bool { c := s.Current(); return c != nil && !c.Playing })
	os.WriteFile(filepath.Join(st.out, "leave"), nil, 0o644)
	until(t, "the phone has gone", func() bool { return s.Current() == nil })
	if _, ok := s.ArtFile(); ok {
		t.Error("the cover outlived the song")
	}
}

func TestASpeakerThatFallsOverIsStartedAgainAndSaysWhy(t *testing.T) {
	st := newStage(t)
	st.program(t, "shairport-sync", `
if [ "$1" = "-V" ]; then echo "3.3.8-OpenSSL-Avahi-ALSA-sysconfdir:/etc"; exit 0; fi
echo started >> "$OUT/starts"
echo "fatal error: could not bind to port 5000: is another copy running?" >&2
exit 1
`)
	s := st.service(false, true, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	until(t, "it was started three times", func() bool {
		data, _ := os.ReadFile(filepath.Join(st.out, "starts"))
		return strings.Count(string(data), "started") >= 3
	})
	until(t, "the reason is known", func() bool { return strings.Contains(s.Status().AirPlay.Problem, "could not bind to port 5000") })
	if s.Status().AirPlay.Running && s.Current() != nil {
		t.Errorf("status = %+v", s.Status())
	}
	// An older copy without the desktop's sound and without telling what
	// plays: started with neither asked of it.
	conf, _ := os.ReadFile(filepath.Join(st.dir, "shairport-sync.conf"))
	if strings.Contains(string(conf), "output_backend") || strings.Contains(string(conf), "metadata") {
		t.Errorf("asked of an old copy what it cannot do:\n%s", conf)
	}
}

// A player on the Pi, for trying the two together.
type onThePi struct {
	track   *playing.Track
	pressed []string
}

func (o *onThePi) Current() *playing.Track { return o.track }
func (o *onThePi) Control(_ context.Context, b string) error {
	o.pressed = append(o.pressed, b)
	return nil
}
func (o *onThePi) ArtFile() (string, bool) { return "/pi/cover", true }

func TestWhatAPhonePlaysComesBeforeWhatThePiPlays(t *testing.T) {
	s := New(Options{Dir: t.TempDir(), Log: quiet, LookPath: func(string) (string, error) { return "", exec.ErrNotFound }})
	pi := &onThePi{track: &playing.Track{Player: "Chromium", Title: "On the Pi", Playing: true}}
	both := Both{Speakers: s, Others: pi}
	ctx := context.Background()

	if got := both.Current(); got == nil || got.Title != "On the Pi" {
		t.Fatalf("with no phone playing: %+v", got)
	}
	if err := both.Control(ctx, "playpause"); err != nil || len(pi.pressed) != 1 {
		t.Errorf("the Pi's player was not pressed: %v %v", err, pi.pressed)
	}

	s.airplayEvent("ssnc", "pbeg", nil)
	s.airplayEvent("core", "minm", []byte("From the phone"))
	if got := both.Current(); got == nil || got.Title != "From the phone" {
		t.Fatalf("with a phone playing: %+v", got)
	}
	// Its buttons are on the phone.
	if err := both.Control(ctx, "playpause"); err != ErrOnThePhone || len(pi.pressed) != 1 {
		t.Errorf("a press went to the wrong player: %v %v", err, pi.pressed)
	}
	// Held on the phone while the Pi plays: the Pi's music is what is on.
	s.airplayEvent("ssnc", "pfls", nil)
	if got := both.Current(); got == nil || got.Title != "On the Pi" {
		t.Errorf("with the phone held: %+v", got)
	}
	if file, _ := both.ArtFile(); file != "/pi/cover" {
		t.Errorf("cover = %q", file)
	}
	// With nothing on the Pi, what the phone holds is still shown.
	pi.track = nil
	if got := both.Current(); got == nil || got.Title != "From the phone" || got.Playing {
		t.Errorf("held, nothing on the Pi: %+v", got)
	}

	// Two phones: the one that began last.
	s.spotifyEvent(ctx, map[string]string{"PLAYER_EVENT": "track_changed", "TRACK_ID": "x", "NAME": "From Spotify"})
	s.spotifyEvent(ctx, map[string]string{"PLAYER_EVENT": "playing", "TRACK_ID": "x"})
	if got := s.Current(); got == nil || got.Title != "From Spotify" {
		t.Errorf("Spotify began last: %+v", got)
	}
	s.airplayEvent("ssnc", "prsm", nil)
	if got := s.Current(); got == nil || got.Title != "From the phone" {
		t.Errorf("AirPlay carried on last: %+v", got)
	}
}
