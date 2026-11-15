package update

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.3.2", "v0.3.1", true}, {"v0.4.0", "v0.3.9", true}, {"v1.0.0", "v0.99.99", true}, {"v0.3.10", "v0.3.9", true},
		{"v0.3.1", "v0.3.1", false}, {"v0.3.0", "v0.3.1", false},
		{"v0.4.0", "dev", false}, {"v0.4.0", "v0.3.1-2-gabc123-dirty", false}, {"", "v0.3.1", false}, {"latest", "v0.3.1", false},
	} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// release serves what GitHub's "latest release" address does: a redirect.
func release(t *testing.T, tag *string) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if *tag == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/owner/repo/releases/tag/"+*tag, http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/owner/repo/releases/latest"
}

func newService(t *testing.T, current string, tag *string, auto bool) (*Service, *[]string, *time.Time, *bool) {
	script := filepath.Join(t.TempDir(), "update.sh")
	os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755)
	ran, now, busy := []string{}, time.Date(2026, 3, 10, 14, 0, 0, 0, time.Local), false
	s := New(Options{
		Current: current, Auto: auto, Script: script, LatestURL: release(t, tag),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return now }, Busy: func() bool { return busy },
		Run: func(script string) error { ran = append(ran, script); return nil },
	})
	return s, &ran, &now, &busy
}

func TestANewReleaseIsInstalledAtNightOnly(t *testing.T) {
	tag := "v0.3.2"
	s, ran, now, busy := newService(t, "v0.3.1", &tag, true)
	st, err := s.Check(context.Background())
	if err != nil || st.Latest != "v0.3.2" || !st.Available || !st.Possible {
		t.Fatalf("status = %+v, err %v", st, err)
	}
	if s.shouldInstall(*now) {
		t.Error("an update would start in the afternoon")
	}
	*now = time.Date(2026, 3, 11, 3, 30, 0, 0, time.Local)
	*busy = true
	if s.shouldInstall(*now) {
		t.Error("an update would start while something is being watched")
	}
	*busy = false
	if !s.shouldInstall(*now) {
		t.Fatal("no update at night although one is available")
	}
	if err := s.Install(); err != nil || len(*ran) != 1 || !strings.HasSuffix((*ran)[0], "update.sh") {
		t.Fatalf("ran = %v, err %v", *ran, err)
	}
	// Once started it is not started again.
	if s.shouldInstall(*now) || s.Install() != nil || len(*ran) != 1 || !s.Status().Installing {
		t.Errorf("the update was started twice: %v", *ran)
	}
}

func TestNothingHappensWithoutANewerRelease(t *testing.T) {
	tag := "v0.3.1"
	s, _, now, _ := newService(t, "v0.3.1", &tag, true)
	s.Check(context.Background())
	*now = time.Date(2026, 3, 11, 4, 0, 0, 0, time.Local)
	if st := s.Status(); st.Available || s.shouldInstall(*now) {
		t.Errorf("status = %+v", st)
	}

	// Switched off: told about it, never installed by itself.
	tag = "v0.4.0"
	manual, ran, now2, _ := newService(t, "v0.3.1", &tag, false)
	manual.Check(context.Background())
	*now2 = time.Date(2026, 3, 11, 4, 0, 0, 0, time.Local)
	if !manual.Status().Available || manual.shouldInstall(*now2) || len(*ran) != 0 {
		t.Errorf("status = %+v, ran %v", manual.Status(), *ran)
	}
}

func TestWhenItCannotUpdateItself(t *testing.T) {
	tag := "v9.9.9"
	dev, ran, _, _ := newService(t, "v0.3.1-3-gabc1234", &tag, true)
	if st := dev.Status(); st.Possible || !strings.Contains(st.Reason, "development build") {
		t.Errorf("status = %+v", st)
	}
	if err := dev.Install(); err == nil || len(*ran) != 0 {
		t.Errorf("a development build updated itself: %v, %v", err, *ran)
	}

	gone, _, _, _ := newService(t, "v0.3.1", &tag, true)
	os.Remove(gone.opt.Script)
	if st := gone.Status(); st.Possible || !strings.Contains(st.Reason, "installer") {
		t.Errorf("status = %+v", st)
	}

	// The release page being away is reported and changes nothing.
	tag = ""
	s, _, _, _ := newService(t, "v0.3.1", &tag, true)
	if st, err := s.Check(context.Background()); err == nil || st.Available || !strings.Contains(st.Error, "failed") {
		t.Errorf("status = %+v, err %v", st, err)
	}
}
