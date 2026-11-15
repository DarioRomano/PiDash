package playing

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// sessionBus starts a message bus of its own and makes it the session bus
// for this test. Skipped where dbus-daemon is not installed.
func sessionBus(t *testing.T) {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	bus := exec.Command(daemon, "--session", "--nofork", "--print-address")
	out, err := bus.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := bus.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bus.Process.Kill(); bus.Wait() })
	address, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(address))
}

// player is a music player on the bus, as a browser or Spotify would be.
type player struct {
	conn     *dbus.Conn
	identity string

	mu      sync.Mutex
	status  string
	meta    map[string]dbus.Variant
	pressed []string
}

func (p *player) press(what string) *dbus.Error {
	p.mu.Lock()
	p.pressed = append(p.pressed, what)
	p.mu.Unlock()
	return nil
}
func (p *player) PlayPause() *dbus.Error { return p.press("playpause") }
func (p *player) Next() *dbus.Error      { return p.press("next") }
func (p *player) Previous() *dbus.Error  { return p.press("previous") }

func (p *player) presses() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.pressed, " ")
}

// playerProps answers the questions about a player's state.
type playerProps struct{ p *player }

func (pp playerProps) all(iface string) map[string]dbus.Variant {
	p := pp.p
	p.mu.Lock()
	defer p.mu.Unlock()
	if iface == rootIface {
		return map[string]dbus.Variant{"Identity": dbus.MakeVariant(p.identity)}
	}
	meta := map[string]dbus.Variant{}
	for k, v := range p.meta {
		meta[k] = v
	}
	return map[string]dbus.Variant{
		"PlaybackStatus": dbus.MakeVariant(p.status), "Metadata": dbus.MakeVariant(meta),
		"CanGoNext": dbus.MakeVariant(true), "CanGoPrevious": dbus.MakeVariant(true),
	}
}

func (pp playerProps) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	return pp.all(iface), nil
}

func (pp playerProps) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	v, ok := pp.all(iface)[name]
	if !ok {
		return dbus.Variant{}, dbus.MakeFailedError(os.ErrNotExist)
	}
	return v, nil
}

func newPlayer(t *testing.T, name, identity string) *player {
	t.Helper()
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	p := &player{conn: conn, identity: identity, status: "Stopped", meta: map[string]dbus.Variant{}}
	if err := conn.Export(p, objectPath, playerIface); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(playerProps{p}, objectPath, "org.freedesktop.DBus.Properties"); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(prefix+name, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("name: %v %v", reply, err)
	}
	t.Cleanup(func() { conn.Close() })
	return p
}

func (p *player) play(status, title string, artists []string, album, art string) {
	meta := map[string]dbus.Variant{"xesam:title": dbus.MakeVariant(title), "xesam:artist": dbus.MakeVariant(artists), "xesam:album": dbus.MakeVariant(album)}
	if art != "" {
		meta["mpris:artUrl"] = dbus.MakeVariant(art)
	}
	p.mu.Lock()
	p.status, p.meta = status, meta
	p.mu.Unlock()
	// What a player announces when something about it changed.
	p.conn.Emit(objectPath, "org.freedesktop.DBus.Properties.PropertiesChanged", playerIface,
		map[string]dbus.Variant{"PlaybackStatus": dbus.MakeVariant(status)}, []string{})
}

func TestFollowsWhatIsPlaying(t *testing.T) {
	sessionBus(t)
	var mu sync.Mutex
	var last *Track
	changes := 0
	s := &Service{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), OnChange: func(tr *Track) { mu.Lock(); last = tr; changes++; mu.Unlock() }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	shows := func(what string, ok func(*Track) bool) {
		t.Helper()
		for i := 0; i < 150; i++ {
			mu.Lock()
			tr := last
			mu.Unlock()
			if ok(tr) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("never happened: %s (showing %+v)", what, last)
	}
	time.Sleep(200 * time.Millisecond) // listening
	if s.Current() != nil {
		t.Fatalf("something is playing on an empty bus: %+v", s.Current())
	}
	if err := s.Control(ctx, "playpause"); err != ErrNothing {
		t.Errorf("pressing play with no player: %v, want ErrNothing", err)
	}

	// A browser starts playing a song.
	web := newPlayer(t, "chromium.instance42", "Chromium")
	web.play("Playing", "Blue in Green", []string{"Miles Davis", "Bill Evans"}, "Kind of Blue", "https://covers.example/kind-of-blue.jpg")
	shows("the song appears", func(tr *Track) bool {
		return tr != nil && tr.Title == "Blue in Green" && tr.Artist == "Miles Davis, Bill Evans" && tr.Album == "Kind of Blue" &&
			tr.Playing && tr.Player == "Chromium" && tr.CanSkip && tr.Art == "https://covers.example/kind-of-blue.jpg"
	})

	// The remote's buttons reach it.
	for _, b := range []string{"playpause", "next", "previous"} {
		if err := s.Control(ctx, b); err != nil {
			t.Fatalf("%s: %v", b, err)
		}
	}
	if got := web.presses(); got != "playpause next previous" {
		t.Errorf("the player was told %q", got)
	}
	if err := s.Control(ctx, "Quit"); err == nil {
		t.Error("only play/pause, next and previous may be pressed")
	}

	// Paused, it stays on show; a cover that is a file on the Pi is offered
	// under an address of PiDash's own.
	cover := filepath.Join(t.TempDir(), "cover.png")
	os.WriteFile(cover, []byte("\x89PNG\r\n\x1a\n..."), 0o644)
	web.play("Paused", "So What", []string{"Miles Davis"}, "Kind of Blue", "file://"+cover)
	shows("the paused song stays", func(tr *Track) bool {
		return tr != nil && tr.Title == "So What" && !tr.Playing && strings.HasPrefix(tr.Art, "/api/playing/art?v=")
	})
	if file, ok := s.ArtFile(); !ok || file != cover {
		t.Errorf("art file = %q, %v", file, ok)
	}

	// Another player starts: what plays wins over what is paused.
	box := newPlayer(t, "spotifyd", "Spotify Connect")
	box.play("Playing", "Gymnopédie No. 1", []string{"Erik Satie"}, "", "")
	shows("the playing one takes over", func(tr *Track) bool {
		return tr != nil && tr.Title == "Gymnopédie No. 1" && tr.Player == "Spotify Connect" && tr.Art == ""
	})
	if err := s.Control(ctx, "next"); err != nil || box.presses() != "next" {
		t.Errorf("next went to %q (err %v), want the player that is on show", box.presses(), err)
	}

	// The dashboard's own radio marks itself and is left out.
	box.play("Playing", "Radio Swiss Jazz", nil, OwnMark, "")
	shows("the dashboard's own radio is not shown; the paused song is back", func(tr *Track) bool { return tr != nil && tr.Title == "So What" })

	// Players that stop or go away leave nothing.
	web.play("Stopped", "So What", nil, "", "")
	shows("nothing is playing", func(tr *Track) bool { return tr == nil })
	box.play("Playing", "Clair de Lune", []string{"Claude Debussy"}, "", "")
	shows("playing again", func(tr *Track) bool { return tr != nil && tr.Title == "Clair de Lune" })
	box.conn.Close()
	shows("the player closed", func(tr *Track) bool { return tr == nil })

	// While music simply plays, nothing is reported again and again.
	mu.Lock()
	n := changes
	mu.Unlock()
	time.Sleep(400 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if changes != n {
		t.Errorf("%d reports while nothing changed", changes-n)
	}
}

func TestCoverAddresses(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "a.png")
	os.WriteFile(small, []byte("x"), 0o644)
	for raw, wantPage := range map[string]string{
		"https://i.example/cover.jpg": "https://i.example/cover.jpg",
		"http://nas.local/cover.jpg":  "http://nas.local/cover.jpg",
		"":                            "",
		"file://" + dir:               "", // a folder is no picture
		"file:///no/such/file.png":    "",
		"javascript:alert(1)":         "",
		"data:image/png;base64,AAAA":  "",
	} {
		if page, file := artFor(raw); page != wantPage || file != "" {
			t.Errorf("artFor(%q) = %q, %q; want %q and no file", raw, page, file, wantPage)
		}
	}
	page, file := artFor("file://" + small)
	if !strings.HasPrefix(page, "/api/playing/art?v=") || file != small {
		t.Errorf("a file on the Pi: %q, %q", page, file)
	}
}
