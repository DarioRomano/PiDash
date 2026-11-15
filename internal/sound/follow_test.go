package sound

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func outputs(active string, ids ...string) Status {
	st := Status{Available: true}
	for _, id := range ids {
		kind := "hdmi"
		if strings.HasPrefix(id, "bt") {
			kind = "bluetooth"
		}
		st.Outputs = append(st.Outputs, Output{ID: id, Name: id, Kind: kind, Active: id == active, Volume: 100})
	}
	return st
}

func TestSoundFollowsHeadphones(t *testing.T) {
	var f Follower
	steps := []struct {
		what string
		st   Status
		want string
	}{
		{"PiDash starts with headphones already on: nothing moves", outputs("tv", "tv", "tv2", "bt-old"), ""},
		{"nothing changed", outputs("tv", "tv", "tv2", "bt-old"), ""},
		{"headphones connect", outputs("tv", "tv", "tv2", "bt-old", "bt-head"), "bt-head"},
		{"the sound moved there", outputs("bt-head", "tv", "tv2", "bt-old", "bt-head"), ""},
		{"they disconnect, and the sound service falls to the wrong HDMI socket", outputs("tv2", "tv", "tv2", "bt-old"), "tv"},
		{"back on the TV", outputs("tv", "tv", "tv2", "bt-old"), ""},
		{"they connect, and the sound service has moved the sound itself", outputs("bt-head", "tv", "tv2", "bt-old", "bt-head"), ""},
		{"someone chooses the TV by hand while they are on: it stays", outputs("tv", "tv", "tv2", "bt-old", "bt-head"), ""},
		{"they disconnect while the TV is in use: nothing to do", outputs("tv", "tv", "tv2", "bt-old"), ""},
		{"someone chooses the second socket by hand", outputs("tv2", "tv", "tv2", "bt-old"), ""},
		{"headphones connect again", outputs("tv2", "tv", "tv2", "bt-old", "bt-head"), "bt-head"},
		{"in use", outputs("bt-head", "tv", "tv2", "bt-old", "bt-head"), ""},
		{"gone: back to what was chosen by hand, not to the first socket", outputs("tv", "tv", "tv2", "bt-old"), "tv2"},
	}
	for _, step := range steps {
		if got := f.Step(step.st); got != step.want {
			t.Errorf("%s: move to %q, want %q", step.what, got, step.want)
		}
	}
}

func TestSoundDoesNotReturnToAnOutputThatIsGoneOrUnplugged(t *testing.T) {
	var f Follower
	f.Step(outputs("tv", "tv"))
	f.Step(outputs("tv", "tv", "bt-head"))
	f.Step(outputs("bt-head", "tv", "bt-head"))
	// The TV's socket was unplugged meanwhile.
	st := outputs("tv2", "tv", "tv2")
	st.Outputs[0].Unplugged = true
	if got := f.Step(st); got != "" {
		t.Errorf("moved to %q, an output with nothing plugged in", got)
	}

	f = Follower{}
	f.Step(outputs("tv", "tv"))
	f.Step(outputs("bt-head", "tv", "bt-head"))
	if got := f.Step(outputs("tv2", "tv2")); got != "" {
		t.Errorf("moved to %q, which is not there", got)
	}
	// A sound service that does not answer changes nothing, and is not
	// mistaken for "everything was unplugged".
	if got := f.Step(Status{}); got != "" {
		t.Errorf("moved to %q without a sound service", got)
	}
}

func TestWhichEventsAreAboutOutputs(t *testing.T) {
	for line, want := range map[string]bool{
		"Event 'new' on sink #57":              true,
		"Event 'remove' on sink #57":           true,
		"Event 'change' on server #4294967295": true,
		"Event 'new' on card #3":               true,
		"Event 'new' on sink-input #12":        false, // something started playing
		"Event 'change' on sink-input #12":     false,
		"Event 'new' on source #5":             false, // a microphone
		"Event 'new' on client #88":            false,
		"":                                     false,
		"garbage":                              false,
	} {
		if got := outputEvent(line); got != want {
			t.Errorf("outputEvent(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestAnUnpluggedSocketIsMarked(t *testing.T) {
	p := &Pactl{Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "get-default-sink" {
			return []byte("hdmi-a\n"), nil
		}
		return []byte(`[
		  {"name":"hdmi-a","description":"HDMI","active_port":"hdmi-output-0","ports":[{"name":"hdmi-output-0","availability":"available"}]},
		  {"name":"hdmi-b","description":"HDMI","active_port":"hdmi-output-1","ports":[{"name":"hdmi-output-1","availability":"not available"}]},
		  {"name":"usb","description":"USB","active_port":"analog","ports":[{"name":"analog","availability":"availability unknown"}]},
		  {"name":"bluez_output.x","description":"Headphones","active_port":null,"ports":[]}
		]`), nil
	}}
	got := map[string]bool{}
	for _, o := range p.Status(context.Background()).Outputs {
		got[o.ID] = o.Unplugged
	}
	if len(got) != 4 || got["hdmi-a"] || !got["hdmi-b"] || got["usb"] || got["bluez_output.x"] {
		t.Errorf("unplugged = %v; want only hdmi-b", got)
	}
}

func TestWatchReportsBurstsOnceAndSurvivesARestartOfTheService(t *testing.T) {
	var mu sync.Mutex
	changes := 0
	streams := make(chan io.ReadCloser, 2)
	r1, w1 := io.Pipe()
	streams <- r1
	p := &Pactl{Subscribe: func(ctx context.Context) (io.ReadCloser, error) {
		select {
		case s := <-streams:
			return s, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Watch(ctx, func() { mu.Lock(); changes++; mu.Unlock() })
	count := func() int { mu.Lock(); defer mu.Unlock(); return changes }

	// Headphones connecting: several events within a moment, one report.
	io.WriteString(w1, "Event 'new' on card #3\nEvent 'new' on sink #57\nEvent 'new' on sink-input #9\nEvent 'change' on server #1\n")
	time.Sleep(700 * time.Millisecond)
	if count() != 1 {
		t.Fatalf("a burst of events was reported %d times, want once", count())
	}
	// Music starting and stopping is nobody's business here.
	io.WriteString(w1, "Event 'new' on sink-input #10\nEvent 'remove' on sink-input #10\n")
	time.Sleep(600 * time.Millisecond)
	if count() != 1 {
		t.Errorf("something playing was reported as a change of outputs")
	}
	// The sound service ends: reported once, since what it offers may differ afterwards.
	w1.Close()
	time.Sleep(700 * time.Millisecond)
	if count() != 2 {
		t.Errorf("after the service went away: %d reports, want 2", count())
	}
}

func TestWatchAgainstASoundService(t *testing.T) {
	soundServer(t)
	p := &Pactl{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed := make(chan struct{}, 8)
	go p.Watch(ctx, func() { changed <- struct{}{} })
	time.Sleep(500 * time.Millisecond) // subscribed

	wait := func(what string) {
		t.Helper()
		select {
		case <-changed:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s was not noticed", what)
		}
	}
	// Headphones connecting, as far as the sound service is concerned.
	out, err := exec.Command("pactl", "load-module", "module-null-sink", "sink_name=bluez_output.new").Output()
	if err != nil {
		t.Fatal(err)
	}
	wait("a new output")
	if !strings.Contains(statusIDs(p), "bluez_output.new") {
		t.Errorf("the new output is not listed: %s", statusIDs(p))
	}
	if err := p.Use(ctx, "bluez_output.new"); err != nil {
		t.Fatal(err)
	}
	wait("choosing another output")
	if err := exec.Command("pactl", "unload-module", strings.TrimSpace(string(out))).Run(); err != nil {
		t.Fatal(err)
	}
	wait("an output going away")
}

func statusIDs(p *Pactl) string {
	var ids []string
	for _, o := range p.Status(context.Background()).Outputs {
		ids = append(ids, o.ID)
	}
	return strings.Join(ids, " ")
}
