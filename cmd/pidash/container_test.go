package main

import (
	"net"
	"os"
	"path/filepath"

	"github.com/DarioRomano/PiDash/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthcheckAsksThePiDashOnThisMachine(t *testing.T) {
	for addr, want := range map[string]string{
		":8080":            "http://127.0.0.1:8080/healthz",
		"0.0.0.0:8081":     "http://127.0.0.1:8081/healthz",
		"[::]:8080":        "http://127.0.0.1:8080/healthz",
		"192.168.1.5:9000": "http://192.168.1.5:9000/healthz",
		"localhost:8080":   "http://localhost:8080/healthz",
	} {
		if got, err := healthURL(addr); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", addr, got, err, want)
		}
	}
	if _, err := healthURL("8080"); err == nil {
		t.Error("an address without a port must be refused")
	}

	well := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" || !well {
			http.Error(w, "no", http.StatusServiceUnavailable)
		}
	}))
	addr := strings.TrimPrefix(srv.URL, "http://")
	_, port, _ := net.SplitHostPort(addr)
	if code := healthcheck("", ":"+port); code != 0 {
		t.Errorf("a PiDash that answers: exit code %d, want 0", code)
	}
	well = false
	if code := healthcheck("", ":"+port); code != 1 {
		t.Errorf("a PiDash that is unwell: exit code %d, want 1", code)
	}
	srv.Close()
	if code := healthcheck("", addr); code != 1 {
		t.Errorf("no PiDash there: exit code %d, want 1", code)
	}
}

func TestAContainersMountedPicturesAreShown(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default()
	mountedPhotos(&cfg, dir)
	if cfg.Photos.Source != "folder" || cfg.Photos.Folder.Path != dir {
		t.Errorf("mounted pictures are not the photos: %+v", cfg.Photos)
	}
	if err := cfg.Finish(); err != nil {
		t.Errorf("the settings no longer hold: %v", err)
	}

	// Nothing mounted, or a file where the folder should be: no photos.
	file := filepath.Join(dir, "a-file")
	_ = os.WriteFile(file, nil, 0o644)
	for _, missing := range []string{"", filepath.Join(dir, "nothing"), file} {
		cfg = config.Default()
		mountedPhotos(&cfg, missing)
		if cfg.Photos.Source != "" || cfg.Photos.Folder.Path != "" {
			t.Errorf("%q: photos from nowhere: %+v", missing, cfg.Photos)
		}
	}

	// What the settings file says stands: its own folder, or the NAS.
	cfg = config.Default()
	cfg.Photos.Source, cfg.Photos.Folder.Path = "folder", "/elsewhere"
	mountedPhotos(&cfg, dir)
	if cfg.Photos.Folder.Path != "/elsewhere" {
		t.Errorf("the settings file's folder was replaced: %+v", cfg.Photos)
	}
	cfg = config.Default()
	cfg.Photos.Source = "synology"
	mountedPhotos(&cfg, dir)
	if cfg.Photos.Source != "synology" || cfg.Photos.Folder.Path != dir {
		t.Errorf("the NAS must stay the source, with the folder on offer: %+v", cfg.Photos)
	}
}
