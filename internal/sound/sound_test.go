package sound

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// soundServer starts a sound service of its own with two outputs that go
// nowhere, and points the tools at it. The test is skipped where PulseAudio
// is not installed:
//
//	sudo apt install pulseaudio pulseaudio-utils
func soundServer(t *testing.T) {
	t.Helper()
	daemon, err := exec.LookPath("pulseaudio")
	if err != nil {
		t.Skip("pulseaudio is not installed")
	}
	if _, err := exec.LookPath("pactl"); err != nil {
		t.Skip("pactl is not installed")
	}
	// Not t.TempDir(): the path of the server's socket has a length limit.
	dir, err := os.MkdirTemp("", "pa")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	run, home := filepath.Join(dir, "run"), filepath.Join(dir, "home")
	os.MkdirAll(run, 0o700)
	os.MkdirAll(home, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", run)
	t.Setenv("HOME", home)
	// A server named in the environment would be asked instead of this one.
	if old, set := os.LookupEnv("PULSE_SERVER"); set {
		os.Unsetenv("PULSE_SERVER")
		t.Cleanup(func() { os.Setenv("PULSE_SERVER", old) })
	}
	cmd := exec.Command(daemon, "-n", "--system=false", "--daemonize=no", "--exit-idle-time=-1", "--log-target=file:"+filepath.Join(dir, "log"),
		"--load=module-native-protocol-unix",
		"--load=module-null-sink sink_name=alsa_output.platform-107c701400.hdmi.hdmi-stereo sink_properties=device.description='Built-in\\ Audio\\ Digital\\ Stereo\\ (HDMI)'",
		"--load=module-null-sink sink_name=alsa_output.platform-107c706400.hdmi.hdmi-stereo sink_properties=device.description='Built-in\\ Audio\\ Digital\\ Stereo\\ (HDMI)'",
		"--load=module-null-sink sink_name=bluez_output.AA_BB_CC_00_00_02.1 sink_properties=device.description='Living\\ room\\ speaker'")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	for i := 0; ; i++ {
		if exec.Command("pactl", "info").Run() == nil {
			return
		}
		if i > 100 {
			log, _ := os.ReadFile(filepath.Join(dir, "log"))
			t.Skipf("the sound service did not start here:\n%s", log)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAgainstASoundService(t *testing.T) {
	soundServer(t)
	ctx := context.Background()
	p := &Pactl{}
	const hdmi1, hdmi2, speaker = "alsa_output.platform-107c701400.hdmi.hdmi-stereo", "alsa_output.platform-107c706400.hdmi.hdmi-stereo", "bluez_output.AA_BB_CC_00_00_02.1"

	st := p.Status(ctx)
	if !st.Available || len(st.Outputs) != 3 {
		t.Fatalf("status = %+v", st)
	}
	by := func(st Status, id string) Output {
		for _, o := range st.Outputs {
			if o.ID == id {
				return o
			}
		}
		t.Fatalf("no output %s in %+v", id, st.Outputs)
		return Output{}
	}
	// The Pi's two HDMI sockets carry the same description: they are numbered.
	if a, b := by(st, hdmi1), by(st, hdmi2); a.Name != "Built-in Audio Digital Stereo (HDMI) 1" || b.Name != "Built-in Audio Digital Stereo (HDMI) 2" || a.Kind != "hdmi" {
		t.Errorf("HDMI outputs = %+v and %+v", a, b)
	}
	if o := by(st, speaker); o.Name != "Living room speaker" || o.Kind != "bluetooth" || o.Volume != 100 || o.Muted {
		t.Errorf("speaker = %+v", o)
	}
	active := 0
	for _, o := range st.Outputs {
		if o.Active {
			active++
		}
	}
	if active != 1 {
		t.Errorf("%d outputs are marked as in use, want exactly one", active)
	}

	if err := p.Use(ctx, speaker); err != nil {
		t.Fatal(err)
	}
	if err := p.SetVolume(ctx, speaker, 35); err != nil {
		t.Fatal(err)
	}
	if err := p.SetMuted(ctx, hdmi1, true); err != nil {
		t.Fatal(err)
	}
	st = p.Status(ctx)
	if o := by(st, speaker); !o.Active || o.Volume != 35 {
		t.Errorf("after choosing the speaker at 35 %%: %+v", o)
	}
	if o := by(st, hdmi1); o.Active || !o.Muted || o.Volume != 100 {
		t.Errorf("the TV's output after muting it: %+v", o)
	}
	// Never louder than full, never below silence.
	p.SetVolume(ctx, speaker, 400)
	if o := by(p.Status(ctx), speaker); o.Volume != 100 {
		t.Errorf("volume after asking for 400 %% = %d", o.Volume)
	}
	p.SetVolume(ctx, speaker, -5)
	if o := by(p.Status(ctx), speaker); o.Volume != 0 {
		t.Errorf("volume after asking for -5 %% = %d", o.Volume)
	}
	if err := p.SetMuted(ctx, hdmi1, false); err != nil || by(p.Status(ctx), hdmi1).Muted {
		t.Errorf("unmuting: %v", err)
	}

	// The test sound plays through to the end on a real service.
	if err := p.Test(ctx, speaker); err != nil {
		t.Errorf("test sound: %v", err)
	}

	// A name that is no output is never handed to the tool.
	for _, bad := range []string{"", "nope", "hdmi; reboot", "--help", "@DEFAULT_SINK@"} {
		for name, do := range map[string]func() error{
			"use":    func() error { return p.Use(ctx, bad) },
			"volume": func() error { return p.SetVolume(ctx, bad, 50) },
			"mute":   func() error { return p.SetMuted(ctx, bad, true) },
			"test":   func() error { return p.Test(ctx, bad) },
		} {
			if err := do(); err != ErrUnknown {
				t.Errorf("%s(%q) = %v, want ErrUnknown", name, bad, err)
			}
		}
	}
}

func TestWithoutASoundServiceThePageIsToldSo(t *testing.T) {
	p := &Pactl{Run: func(context.Context, string, ...string) ([]byte, error) { return nil, os.ErrNotExist }}
	st := p.Status(context.Background())
	if st.Available || st.Problem == "" || st.Outputs == nil {
		t.Errorf("status = %+v", st)
	}
	if err := p.Use(context.Background(), "hdmi"); err == nil || err == ErrUnknown {
		t.Errorf("choosing an output without a sound service: %v", err)
	}
}

func TestOnlyKnownOutputsReachTheTool(t *testing.T) {
	var calls []string
	p := &Pactl{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch strings.Join(args, " ") {
		case "-f json list sinks":
			return []byte(`[{"name":"hdmi","description":"TV","mute":false,"volume":{"front-left":{"value_percent":"80%"},"front-right":{"value_percent":"81%"}},"properties":{}}]`), nil
		case "get-default-sink":
			return []byte("hdmi\n"), nil
		case "-f json list sink-inputs":
			return []byte(`[{"index":7},{"index":9}]`), nil
		}
		return nil, nil
	}}
	ctx := context.Background()
	if st := p.Status(ctx); len(st.Outputs) != 1 || st.Outputs[0].Volume != 81 || !st.Outputs[0].Active {
		t.Errorf("status = %+v", st)
	}
	calls = nil
	if err := p.Use(ctx, "hdmi"); err != nil {
		t.Fatal(err)
	}
	// What is playing is moved along.
	got := strings.Join(calls, " | ")
	for _, want := range []string{"pactl set-default-sink hdmi", "pactl move-sink-input 7 hdmi", "pactl move-sink-input 9 hdmi"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in: %s", want, got)
		}
	}
}

func TestChimeIsAPlayableWAV(t *testing.T) {
	wav := Chime()
	if !bytes.HasPrefix(wav, []byte("RIFF")) || string(wav[8:16]) != "WAVEfmt " || string(wav[36:40]) != "data" {
		t.Fatalf("not a WAV file: % x", wav[:44])
	}
	if size := binary.LittleEndian.Uint32(wav[4:8]); int(size) != len(wav)-8 {
		t.Errorf("size in the header = %d, file is %d", size, len(wav))
	}
	if data := binary.LittleEndian.Uint32(wav[40:44]); int(data) != len(wav)-44 || data%4 != 0 {
		t.Errorf("sound data = %d bytes of %d", data, len(wav)-44)
	}
	// Half a second, and it ends in silence: no click at the end.
	if seconds := float64(len(wav)-44) / (44100 * 4); seconds < 0.3 || seconds > 0.8 {
		t.Errorf("the chime lasts %.2f s", seconds)
	}
	last := int16(binary.LittleEndian.Uint16(wav[len(wav)-2:]))
	if last > 1500 || last < -1500 {
		t.Errorf("the chime ends at %d, not near silence", last)
	}
}
