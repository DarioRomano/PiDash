package games

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

// A settings file as Moonlight writes it: two PCs, the first with games.
// The second is only in the backup list, which holds no games.
const sample = `[General]
width=1920
height=1080

[gcmapping]
size=0

[hosts]
1\hostname=LIVING-PC
1\customname=false
1\uuid=3F2A9C1E-77AB-4C0D-9E55-0A1B2C3D4E5F
1\mac=@ByteArray(\x1c\x1b\r\xaa\xbb\x42)
1\localaddress=192.168.1.20
1\localport=47989
1\remoteaddress=203.0.113.9
1\manualaddress=
1\srvcert=@ByteArray(-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----\n)
1\nvidiasw=false
1\apps\1\name=Desktop
1\apps\1\id=881448767
1\apps\1\hdr=false
1\apps\1\hidden=false
1\apps\2\name=Steam Big Picture
1\apps\2\id=1093255277
1\apps\2\hidden=false
1\apps\3\name="Ori, the Blind Forest"
1\apps\3\id=-1289054231
1\apps\3\hidden=false
1\apps\4\name=Pok\xe9mon's \"Arena\"; rm -rf ~
1\apps\4\id=42
1\apps\4\hidden=false
1\apps\5\name=Secret tool
1\apps\5\id=7
1\apps\5\hidden=true
1\apps\size=5
size=1

[hostsbackup]
1\hostname=LIVING-PC
1\uuid=3F2A9C1E-77AB-4C0D-9E55-0A1B2C3D4E5F
1\mac=@ByteArray(\x1c\x1b\r\xaa\xbb\x42)
1\localaddress=192.168.1.20
2\hostname=Office
2\uuid=aaaaaaaa-0000-0000-0000-000000000002
2\mac=@ByteArray(\0\0\0\0\0\0)
2\manualaddress=office.lan
2\manualport=48989
size=2
`

func readSample(t *testing.T) []Host {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Moonlight.conf")
	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	hosts, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return hosts
}

func TestReadingMoonlightsSettings(t *testing.T) {
	hosts := readSample(t)
	if len(hosts) != 2 {
		t.Fatalf("hosts = %+v", hosts)
	}
	pc := hosts[0]
	if pc.Name != "LIVING-PC" || pc.UUID != "3F2A9C1E-77AB-4C0D-9E55-0A1B2C3D4E5F" || pc.Address != "192.168.1.20" || pc.Port != 47989 {
		t.Errorf("the PC = %+v", pc)
	}
	// Six raw bytes, of which Qt writes some as letters ("B" is 0x42) and
	// some as escapes ("\r" is 0x0d).
	if pc.MAC.String() != "1c:1b:0d:aa:bb:42" {
		t.Errorf("network card = %s, want 1c:1b:0d:aa:bb:42", pc.MAC)
	}
	var names []string
	for _, g := range pc.Games {
		names = append(names, g.Name)
	}
	want := []string{"Desktop", "Steam Big Picture", "Ori, the Blind Forest", `Pokémon's "Arena"; rm -rf ~`}
	if strings.Join(names, " | ") != strings.Join(want, " | ") {
		t.Errorf("games = %q\nwant    %q (the hidden one left out)", names, want)
	}
	if pc.Games[2].ID != -1289054231 {
		t.Errorf("a negative id = %d", pc.Games[2].ID)
	}
	// A PC known only from the backup list: no games, an address typed by
	// hand, and a network card that was never learned.
	office := hosts[1]
	if office.Name != "Office" || office.Address != "office.lan" || office.Port != 48989 || len(office.MAC) != 0 || len(office.Games) != 0 {
		t.Errorf("the second PC = %+v", office)
	}

	if _, err := Read(filepath.Join(t.TempDir(), "missing.conf")); err == nil {
		t.Error("a missing file should be an error for the caller to ignore")
	}
}

func TestQtValues(t *testing.T) {
	for raw, want := range map[string]string{
		`Desktop`:                 "Desktop",
		`"Ori, the Blind Forest"`: "Ori, the Blind Forest",
		`Pok\xe9mon`:              "Pokémon",
		`\x4e16\x754c`:            "世界",
		`a\x1b\x62`:               "a\x1bb", // a digit of the text is escaped too, so the escape before it ends
		`@String(hello)`:          "hello",
		`@@home`:                  "@home",
		`line\nbreak\ttab\\`:      "line\nbreak\ttab\\",
		`Zürich`:                  "Zürich", // newer Qt writes it as it is
		``:                        "",
	} {
		if got, _ := Unescape(raw); got != want {
			t.Errorf("Unescape(%q) = %q, want %q", raw, got, want)
		}
	}
	if _, b := Unescape(`@ByteArray(\x1\xff\0A\n)`); !bytes.Equal(b, []byte{1, 0xff, 0, 'A', '\n'}) {
		t.Errorf("bytes = % x", b)
	}
}

func TestGamesBecomeTilesThatCannotRunAnythingElse(t *testing.T) {
	hosts := readSample(t)
	art := t.TempDir()
	os.MkdirAll(filepath.Join(art, hosts[0].UUID), 0o755)
	os.WriteFile(filepath.Join(art, hosts[0].UUID, "1093255277.png"), []byte("png"), 0o644)
	moonlight := config.App{ID: "moonlight", Name: "Moonlight", Command: "moonlight-qt", Mode: config.AppModeConsole, Icon: "moonlight"}

	tiles := Tiles(hosts, moonlight, art)
	if len(tiles) != 4 {
		t.Fatalf("tiles = %+v", tiles)
	}
	seen := map[string]bool{}
	for _, tile := range tiles {
		if !strings.HasPrefix(tile.ID, Prefix) || strings.Trim(tile.ID, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" || seen[tile.ID] {
			t.Errorf("id %q should be unique and made of letters, digits and dashes", tile.ID)
		}
		seen[tile.ID] = true
		if tile.Mode != config.AppModeConsole {
			t.Errorf("%s does not start the way Moonlight itself does: %q", tile.Name, tile.Mode)
		}
	}
	if tiles[1].Name != "Steam Big Picture" || tiles[1].Icon != filepath.Join(art, hosts[0].UUID, "1093255277.png") {
		t.Errorf("the game with a cover: %+v", tiles[1])
	}
	if tiles[0].Icon != "moonlight" {
		t.Errorf("a game without a cover takes Moonlight's icon: %q", tiles[0].Icon)
	}
	// The same game has the same tile the next time, so that a running game
	// is recognised after PiDash restarts.
	if again := Tiles(readSample(t), moonlight, art); again[3].ID != tiles[3].ID {
		t.Errorf("ids change between readings: %s and %s", again[3].ID, tiles[3].ID)
	}

	// The command is handed to a shell. A name that tries to be a command
	// must arrive at Moonlight as a name, letter for letter.
	script := filepath.Join(t.TempDir(), "moonlight-qt")
	out := filepath.Join(t.TempDir(), "args")
	os.WriteFile(script, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > "+out+"\n"), 0o755)
	evil := Tiles(hosts, config.App{Command: script}, "")[3]
	canary := filepath.Join(t.TempDir(), "canary")
	os.WriteFile(canary, []byte("x"), 0o644)
	cmd := exec.Command("/bin/sh", "-c", evil.Command)
	cmd.Dir = filepath.Dir(canary)
	cmd.Env = append(os.Environ(), "HOME="+filepath.Dir(canary))
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(out)
	want := "stream\n3F2A9C1E-77AB-4C0D-9E55-0A1B2C3D4E5F\nPokémon's \"Arena\"; rm -rf ~\n"
	if string(got) != want {
		t.Errorf("Moonlight was given:\n%s\nwant:\n%s", got, want)
	}
	if _, err := os.Stat(canary); err != nil {
		t.Error("the name of a game was run as a command")
	}
}

func TestWhichAppIsMoonlight(t *testing.T) {
	for cmd, want := range map[string]bool{
		"moonlight-qt": true, "/usr/bin/moonlight": true, "flatpak run com.moonlight_stream.Moonlight": true,
		"kodi-standalone": false, "steamlink": false, "": false,
	} {
		if got := IsMoonlight(config.App{Command: cmd}); got != want {
			t.Errorf("IsMoonlight(%q) = %v", cmd, got)
		}
	}
	if IsMoonlight(config.App{URL: "https://moonlight.example", Command: ""}) {
		t.Error("a website is not Moonlight")
	}
}

func TestWakingThePC(t *testing.T) {
	mac, _ := net.ParseMAC("1c:1b:0d:aa:bb:42")
	pc := Host{Name: "LIVING-PC", Address: "192.168.1.20", Port: 47989, MAC: mac}
	ctx := context.Background()

	// Awake already: nothing is sent, nobody is told.
	sent, told := 0, 0
	w := Wake{Up: func(context.Context, Host) bool { return true }, Send: func(net.HardwareAddr, string) error { sent++; return nil }, Every: time.Millisecond}
	if err := w.Do(ctx, pc, time.Second, func() { told++ }); err != nil || sent != 0 || told != 0 {
		t.Errorf("an awake PC: err %v, %d calls, told %d times", err, sent, told)
	}

	// Asleep: called until it answers, and the person is told once.
	knocks := 0
	w.Up = func(context.Context, Host) bool { knocks++; return knocks > 4 }
	if err := w.Do(ctx, pc, time.Second, func() { told++ }); err != nil || sent < 3 || told != 1 {
		t.Errorf("a sleeping PC: err %v, %d calls, told %d times", err, sent, told)
	}

	// It never comes up: that is said, after the time it was given.
	w.Up = func(context.Context, Host) bool { return false }
	start := time.Now()
	if err := w.Do(ctx, pc, 30*time.Millisecond, nil); err != ErrAsleep || time.Since(start) > 2*time.Second {
		t.Errorf("a PC that stays off: %v after %v", err, time.Since(start))
	}

	// A PC that cannot be knocked at or woken from here is left to Moonlight.
	sent = 0
	for _, h := range []Host{{Name: "no address", MAC: mac}, {Name: "no card", Address: "192.168.1.30", Port: 47989}} {
		if err := w.Do(ctx, h, 30*time.Millisecond, nil); err != nil || sent != 0 {
			t.Errorf("%s: err %v, %d calls", h.Name, err, sent)
		}
	}
}

func TestTheWakeUpCall(t *testing.T) {
	mac, _ := net.ParseMAC("1c:1b:0d:aa:bb:42")
	p := MagicPacket(mac)
	if len(p) != 102 || !bytes.Equal(p[:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) {
		t.Fatalf("packet = % x", p)
	}
	for i := 0; i < 16; i++ {
		if !bytes.Equal(p[6+i*6:12+i*6], mac) {
			t.Fatalf("repetition %d is not the card's address", i)
		}
	}
	// The real knock: something that listens answers, nothing does not.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen here")
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if !answers(context.Background(), Host{Address: "127.0.0.1", Port: port}) {
		t.Error("a listening service was not found")
	}
	ln.Close()
	if answers(context.Background(), Host{Address: "127.0.0.1", Port: port}) {
		t.Error("a closed door was taken for an open one")
	}
}
