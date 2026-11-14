// Package sound chooses where the Pi's sound goes (the TV over HDMI,
// Bluetooth headphones, a speaker) and how loud it is.
//
// It uses pactl, the command-line tool of the sound service on Raspberry Pi
// OS (PipeWire, which speaks PulseAudio's language). The desktop's volume
// menu does the same through the same service.
package sound

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Output is one place sound can go.
type Output struct {
	// ID is the sound service's name for it.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Kind is "hdmi", "bluetooth", "usb", "jack" or "other".
	Kind string `json:"kind"`
	// Active marks the output in use.
	Active bool `json:"active"`
	// Volume is in percent; 100 is full.
	Volume int  `json:"volume"`
	Muted  bool `json:"muted"`
	// Unplugged marks an output with nothing connected to it, such as the
	// second HDMI socket of a Pi with one TV.
	Unplugged bool `json:"unplugged,omitempty"`
}

// Usable reports whether sound sent to the output can be heard.
func (o Output) Usable() bool { return !o.Unplugged }

// Status is what the settings page shows.
type Status struct {
	// Available is false when the sound service cannot be asked; Problem
	// then says why.
	Available bool     `json:"available"`
	Problem   string   `json:"problem,omitempty"`
	Outputs   []Output `json:"outputs"`
}

// Manager is what the server needs.
type Manager interface {
	Status(ctx context.Context) Status
	// Use makes an output the one in use; what is playing moves over.
	Use(ctx context.Context, id string) error
	// SetVolume sets an output's volume in percent, 0 to 100.
	SetVolume(ctx context.Context, id string, percent int) error
	SetMuted(ctx context.Context, id string, muted bool) error
	// Test plays a short sound on an output.
	Test(ctx context.Context, id string) error
	// Watch calls changed whenever an output appears, disappears or is
	// chosen, until ctx ends.
	Watch(ctx context.Context, changed func())
}

// ErrUnknown is returned for an output that is not there (any more).
var ErrUnknown = errors.New("this output is not there any more")

// Pactl manages sound through the pactl tool.
type Pactl struct {
	// Run executes a tool and returns what it printed; nil means the real one.
	Run func(ctx context.Context, name string, args ...string) ([]byte, error)
	// Subscribe starts the stream of the sound service's events, one per
	// line; nil means "pactl subscribe".
	Subscribe func(ctx context.Context) (io.ReadCloser, error)
}

func (p *Pactl) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if p.Run != nil {
		return p.Run(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, err
}

// sink is an output as pactl describes it.
type sink struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mute        bool   `json:"mute"`
	Volume      map[string]struct {
		Percent string `json:"value_percent"`
	} `json:"volume"`
	Properties map[string]string `json:"properties"`
	ActivePort string            `json:"active_port"`
	Ports      []struct {
		Name         string `json:"name"`
		Availability string `json:"availability"`
	} `json:"ports"`
}

// unplugged reports whether the socket the output uses has nothing in it.
// Outputs without sockets (Bluetooth), and sockets that cannot tell, count
// as plugged in.
func (s sink) unplugged() bool {
	for _, p := range s.Ports {
		if p.Name == s.ActivePort || len(s.Ports) == 1 {
			return p.Availability == "not available"
		}
	}
	return false
}

func (s sink) volume() int {
	total, n := 0, 0
	for _, ch := range s.Volume {
		if v, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(ch.Percent), "%")); err == nil {
			total += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return (total + n/2) / n
}

func (s sink) kind() string {
	name := strings.ToLower(s.Name)
	bus := strings.ToLower(s.Properties["device.bus"])
	switch {
	case strings.HasPrefix(name, "bluez"), bus == "bluetooth", s.Properties["api.bluez5.address"] != "":
		return "bluetooth"
	case strings.Contains(name, "hdmi"):
		return "hdmi"
	case bus == "usb", strings.Contains(name, "usb"):
		return "usb"
	case strings.Contains(name, "analog"), strings.Contains(name, "headphone"):
		return "jack"
	}
	return "other"
}

func (p *Pactl) sinks(ctx context.Context) ([]sink, string, error) {
	if p.Run == nil {
		if _, err := exec.LookPath("pactl"); err != nil {
			return nil, "", errNoTool
		}
	}
	raw, err := p.run(ctx, "pactl", "-f", "json", "list", "sinks")
	if err != nil {
		return nil, "", err
	}
	var sinks []sink
	if err := json.Unmarshal(raw, &sinks); err != nil {
		return nil, "", fmt.Errorf("cannot read the list of outputs: %w", err)
	}
	def, err := p.run(ctx, "pactl", "get-default-sink")
	if err != nil {
		return nil, "", err
	}
	return sinks, strings.TrimSpace(string(def)), nil
}

var errNoTool = errors.New("pactl is not installed")

// Status implements Manager.
func (p *Pactl) Status(ctx context.Context) Status {
	st := Status{Outputs: []Output{}}
	sinks, def, err := p.sinks(ctx)
	switch {
	case errors.Is(err, errNoTool):
		st.Problem = "The tool for choosing the sound output is missing. In a terminal on the Pi: sudo apt install pulseaudio-utils"
		return st
	case err != nil:
		st.Problem = "The sound service of this Pi does not answer."
		return st
	}
	st.Available = true
	names := map[string]int{}
	for _, s := range sinks {
		o := Output{ID: s.Name, Name: strings.TrimSpace(s.Description), Kind: s.kind(), Active: s.Name == def, Volume: s.volume(), Muted: s.Mute, Unplugged: s.unplugged()}
		if o.Name == "" {
			o.Name = s.Name
		}
		names[o.Name]++
		st.Outputs = append(st.Outputs, o)
	}
	// A Pi has two HDMI sockets with the same description: number them.
	sort.SliceStable(st.Outputs, func(i, j int) bool { return st.Outputs[i].ID < st.Outputs[j].ID })
	count := map[string]int{}
	for i, o := range st.Outputs {
		if names[o.Name] > 1 {
			count[o.Name]++
			st.Outputs[i].Name = fmt.Sprintf("%s %d", o.Name, count[o.Name])
		}
	}
	return st
}

// known checks that id names an output, so that nothing else is ever handed
// to the tool as a name.
func (p *Pactl) known(ctx context.Context, id string) error {
	sinks, _, err := p.sinks(ctx)
	if err != nil {
		return errors.New("the sound service of this Pi does not answer")
	}
	for _, s := range sinks {
		if s.Name == id {
			return nil
		}
	}
	return ErrUnknown
}

// Use implements Manager.
func (p *Pactl) Use(ctx context.Context, id string) error {
	if err := p.known(ctx, id); err != nil {
		return err
	}
	if _, err := p.run(ctx, "pactl", "set-default-sink", id); err != nil {
		return fmt.Errorf("could not switch the output: %w", err)
	}
	// What is playing follows by itself on PipeWire; on plain PulseAudio it
	// has to be moved. Moving it where it already is does no harm.
	if raw, err := p.run(ctx, "pactl", "-f", "json", "list", "sink-inputs"); err == nil {
		var playing []struct {
			Index int `json:"index"`
		}
		if json.Unmarshal(raw, &playing) == nil {
			for _, in := range playing {
				_, _ = p.run(ctx, "pactl", "move-sink-input", strconv.Itoa(in.Index), id)
			}
		}
	}
	return nil
}

// SetVolume implements Manager.
func (p *Pactl) SetVolume(ctx context.Context, id string, percent int) error {
	if err := p.known(ctx, id); err != nil {
		return err
	}
	percent = max(0, min(100, percent))
	if _, err := p.run(ctx, "pactl", "set-sink-volume", id, strconv.Itoa(percent)+"%"); err != nil {
		return fmt.Errorf("could not set the volume: %w", err)
	}
	return nil
}

// SetMuted implements Manager.
func (p *Pactl) SetMuted(ctx context.Context, id string, muted bool) error {
	if err := p.known(ctx, id); err != nil {
		return err
	}
	flag := "0"
	if muted {
		flag = "1"
	}
	if _, err := p.run(ctx, "pactl", "set-sink-mute", id, flag); err != nil {
		return fmt.Errorf("could not mute the output: %w", err)
	}
	return nil
}

// Test implements Manager.
func (p *Pactl) Test(ctx context.Context, id string) error {
	if err := p.known(ctx, id); err != nil {
		return err
	}
	f, err := os.CreateTemp("", "pidash-sound-*.wav")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(Chime()); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if _, err := p.run(ctx, "paplay", "--device="+id, f.Name()); err != nil {
		return fmt.Errorf("could not play the test sound: %w", err)
	}
	return nil
}

// Watch implements Manager. It listens to the sound service's own stream of
// events, so a change is noticed the moment it happens and nothing has to
// be asked again and again.
func (p *Pactl) Watch(ctx context.Context, changed func()) {
	subscribe := p.Subscribe
	if subscribe == nil {
		subscribe = func(ctx context.Context) (io.ReadCloser, error) {
			cmd := exec.CommandContext(ctx, "pactl", "subscribe")
			out, err := cmd.StdoutPipe()
			if err != nil {
				return nil, err
			}
			if err := cmd.Start(); err != nil {
				return nil, err
			}
			go cmd.Wait()
			return out, nil
		}
	}
	// Connecting headphones is a burst of events; changed is called once
	// when it is over.
	var timer *time.Timer
	var mu sync.Mutex
	soon := func() {
		mu.Lock()
		defer mu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(400*time.Millisecond, changed)
	}
	for ctx.Err() == nil {
		if stream, err := subscribe(ctx); err == nil {
			lines := bufio.NewScanner(stream)
			for lines.Scan() {
				if outputEvent(lines.Text()) {
					soon()
				}
			}
			stream.Close()
			// The service went away, or was restarted: what it offers may
			// have changed meanwhile.
			soon()
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
	mu.Lock()
	if timer != nil {
		timer.Stop()
	}
	mu.Unlock()
}

// outputEvent reports whether a line of "pactl subscribe" is about the
// outputs: one appeared, disappeared or changed, or another became the one
// in use. Lines look like "Event 'new' on sink #57". What is playing
// ("sink-input") comes and goes all the time and is of no interest.
func outputEvent(line string) bool {
	_, what, ok := strings.Cut(line, " on ")
	if !ok {
		return false
	}
	kind, _, _ := strings.Cut(strings.TrimSpace(what), " ")
	return kind == "sink" || kind == "server" || kind == "card"
}

// Chime returns a short two-note sound as a WAV file: enough to hear which
// speaker it comes out of, quiet enough not to startle anyone.
func Chime() []byte {
	const rate = 44100
	notes := []struct {
		hz      float64
		seconds float64
	}{{659.25, 0.16}, {987.77, 0.34}}
	var pcm []int16
	for _, n := range notes {
		count := int(n.seconds * rate)
		for i := 0; i < count; i++ {
			t := float64(i) / rate
			// A quick start and a long fade, so there is no click.
			env := math.Min(1, t/0.01) * math.Exp(-t*7)
			v := int16(0.28 * env * math.Sin(2*math.Pi*n.hz*t) * math.MaxInt16)
			pcm = append(pcm, v, v) // both channels
		}
	}
	var b bytes.Buffer
	data := len(pcm) * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+data))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))     // size of this part
	binary.Write(&b, binary.LittleEndian, uint16(1))      // PCM
	binary.Write(&b, binary.LittleEndian, uint16(2))      // channels
	binary.Write(&b, binary.LittleEndian, uint32(rate))   // samples per second
	binary.Write(&b, binary.LittleEndian, uint32(rate*4)) // bytes per second
	binary.Write(&b, binary.LittleEndian, uint16(4))      // bytes per sample, both channels
	binary.Write(&b, binary.LittleEndian, uint16(16))     // bits
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(data))
	binary.Write(&b, binary.LittleEndian, pcm)
	return b.Bytes()
}
