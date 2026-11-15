package receivers

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/DarioRomano/PiDash/internal/playing"
)

// runAirPlay keeps shairport-sync running, with settings of PiDash's own,
// and reads what it says is playing.
func (s *Service) runAirPlay(ctx context.Context) {
	bin, err := s.opt.LookPath(airplayProgram)
	if err != nil {
		return
	}
	// What this copy was built with: where its sound can go, and whether it
	// tells what is playing.
	built := strings.ToLower(output(ctx, bin, "-V"))
	has := func(part string) bool {
		return strings.Contains(built, "-"+part+"-") || strings.HasSuffix(strings.TrimSpace(built), "-"+part)
	}
	backend := ""
	switch {
	case has("pa") || has("pulseaudio"):
		backend = "pa" // the desktop's sound, which follows PiDash's settings
	case has("pw") || has("pipewire"):
		backend = "pw"
	}
	pipe := ""
	if has("metadata") {
		pipe = filepath.Join(s.opt.Dir, "airplay-metadata")
		_ = os.Remove(pipe)
		if err := syscall.Mkfifo(pipe, 0o600); err != nil {
			s.opt.Log.Warn("AirPlay speaker: cannot listen for what is playing", "err", err)
			pipe = ""
		}
	}
	if pipe != "" {
		// Opened for reading and writing, so that it neither waits for
		// shairport-sync to open its end nor ends when that closes it.
		if f, err := os.OpenFile(pipe, os.O_RDWR, 0); err == nil {
			go func() {
				<-ctx.Done()
				f.Close()
				_ = os.Remove(pipe)
			}()
			go s.readAirPlay(f)
		} else {
			pipe = ""
		}
	}
	conf := filepath.Join(s.opt.Dir, "shairport-sync.conf")
	s.keep(ctx, "AirPlay", &s.airplay, func() (*exec.Cmd, error) {
		if err := os.WriteFile(conf, []byte(airplayConfig(s.opt.Name, backend, pipe)), 0o600); err != nil {
			return nil, err
		}
		return exec.CommandContext(ctx, bin, "-c", conf), nil
	}, func() {
		s.mu.Lock()
		s.airplayTrack, s.airplayArt = nil, ""
		s.mu.Unlock()
	})
}

// airplayConfig is the settings file shairport-sync is started with.
func airplayConfig(name, backend, pipe string) string {
	// Inside quotes in that file: no quotes or backslashes of its own.
	name = strings.NewReplacer(`"`, "", `\`, "").Replace(name)
	var b strings.Builder
	b.WriteString("// Written by PiDash each time it starts its AirPlay speaker. Changes are lost.\n")
	fmt.Fprintf(&b, "general = {\n  name = \"%s\";\n", name)
	if backend != "" {
		fmt.Fprintf(&b, "  output_backend = \"%s\";\n", backend)
	}
	b.WriteString("};\n")
	if pipe != "" {
		fmt.Fprintf(&b, "metadata = {\n  enabled = \"yes\";\n  include_cover_art = \"yes\";\n  pipe_name = \"%s\";\n  pipe_timeout = 5000;\n};\n", pipe)
	}
	// A second phone may take over from the first.
	b.WriteString("sessioncontrol = {\n  allow_session_interruption = \"yes\";\n};\n")
	return b.String()
}

// One thing shairport-sync says: what kind, which, and what about it.
var airplayItem = regexp.MustCompile(`(?s)<item><type>([0-9a-fA-F]{8})</type><code>([0-9a-fA-F]{8})</code><length>\d+</length>(?:\s*<data encoding="base64">\s*([A-Za-z0-9+/=\s]*?)\s*</data>)?\s*</item>`)

// readAirPlay reads what shairport-sync says until its pipe is closed.
func (s *Service) readAirPlay(r io.Reader) {
	in := bufio.NewReaderSize(r, 64<<10)
	var pending strings.Builder
	for {
		line, err := in.ReadString('>')
		pending.WriteString(line)
		if strings.HasSuffix(line, "</item>") {
			if m := airplayItem.FindStringSubmatch(pending.String()); m != nil {
				kind, _ := hex.DecodeString(m[1])
				code, _ := hex.DecodeString(m[2])
				data, _ := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(m[3]), ""))
				s.airplayEvent(string(kind), string(code), data)
			}
			pending.Reset()
		}
		// A cover is some hundred thousand letters; more than that is not one.
		if pending.Len() > 12<<20 {
			pending.Reset()
		}
		if err != nil {
			return
		}
	}
}

// airplayEvent takes in one thing shairport-sync said.
func (s *Service) airplayEvent(kind, code string, data []byte) {
	s.mu.Lock()
	t := s.airplayTrack
	begin := func() {
		if t == nil {
			t = &playing.Track{Player: "AirPlay", Playing: true}
			s.airplayTrack = t
		}
	}
	text := strings.TrimSpace(string(data))
	switch kind + "/" + code {
	case "core/minm": // the title
		begin()
		t.Title = text
	case "core/asar": // the artist
		begin()
		t.Artist = text
	case "core/asal": // the album
		begin()
		t.Album = text
	case "ssnc/pbeg", "ssnc/prsm", "ssnc/pres": // it begins, or carries on
		begin()
		t.Playing = true
		s.latest = "airplay"
	case "ssnc/pfls", "ssnc/paus": // held
		if t != nil {
			t.Playing = false
		}
	case "ssnc/pend", "ssnc/aend": // over
		s.airplayTrack, s.airplayArt = nil, ""
	case "ssnc/PICT": // the cover
		if t == nil {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		sum := sha1.Sum(data)
		mark := hex.EncodeToString(sum[:6])
		path, err := s.coverFile("airplay", data, mark)
		s.mu.Lock()
		if s.airplayTrack != nil {
			if err != nil {
				s.airplayArt, s.airplayTrack.Art = "", ""
			} else {
				s.airplayArt, s.airplayTrack.Art = path, "/api/playing/art?v="+mark
			}
		}
	default:
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.changed()
}
