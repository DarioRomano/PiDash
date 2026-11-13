package server

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DarioRomano/PiDash/internal/apps"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/tv"
)

// takePort occupies a free port, like another program on the Pi would.
func takePort(t *testing.T) (port int) {
	t.Helper()
	other, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	return other.Addr().(*net.TCPAddr).Port
}

func TestListenUsesTheConfiguredPortWhenFree(t *testing.T) {
	port := takePort(t) + 0
	// Find one that is free by asking the system, then release it.
	probe, _ := net.Listen("tcp", "127.0.0.1:0")
	free := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	_ = port

	ln, wanted, got, err := listen("127.0.0.1:" + strconv.Itoa(free))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if wanted != free || got != free {
		t.Errorf("wanted %d, got %d, expected both to be %d", wanted, got, free)
	}
}

func TestListenMovesToTheNextPortWhenTaken(t *testing.T) {
	busy := takePort(t)
	ln, wanted, got, err := listen("127.0.0.1:" + strconv.Itoa(busy))
	if err != nil {
		t.Fatalf("a taken port must not be fatal: %v", err)
	}
	defer ln.Close()
	if wanted != busy || got <= busy || got > busy+portFallbacks {
		t.Errorf("wanted %d, got %d; expected one of the following %d ports", wanted, got, portFallbacks)
	}
}

func TestListenReportsOtherErrors(t *testing.T) {
	if _, _, _, err := listen("not-an-address"); err == nil {
		t.Error("a malformed address should be an error")
	}
	if _, _, _, err := listen("192.0.2.123:8080"); err == nil { // an address this machine does not have
		t.Error("an address that cannot be bound should be an error, not retried on other ports")
	}
}

func TestServerStartsOnAnotherPortAndSaysSo(t *testing.T) {
	busy := takePort(t)
	urlFile := filepath.Join(t.TempDir(), "run", "pidash", "url")

	cfg := config.Default()
	cfg.Server.Listen = "127.0.0.1:" + strconv.Itoa(busy)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	s := New(cfg, log, static, Deps{
		Kiosk: kiosk.New("", true, log), Apps: apps.NewManager(nil, &apps.Mock{}, log),
		Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}),
		NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
		Notices:   []string{"There is a mistake in the settings file"},
		URLFile:   urlFile, Version: "v9.9.9",
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// The launcher finds the dashboard through the recorded address.
	var url string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(urlFile); err == nil {
			url = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if url == "" || strings.Contains(url, ":"+strconv.Itoa(busy)+"/") {
		t.Fatalf("recorded address = %q; it must exist and not be the taken port %d", url, busy)
	}
	res, err := http.Get(url + "api/config")
	if err != nil {
		t.Fatalf("the dashboard is not reachable at the recorded address: %v", err)
	}
	var client config.Client
	json.NewDecoder(res.Body).Decode(&client)
	res.Body.Close()
	if len(client.Notices) != 2 || !strings.Contains(client.Notices[0], "settings file") || !strings.Contains(client.Notices[1], "Port "+strconv.Itoa(busy)+" is used by another program") {
		t.Errorf("notices for the TV = %q", client.Notices)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("shutdown: %v", err)
	}
	if _, err := os.Stat(urlFile); err == nil {
		t.Error("the recorded address should be removed when the backend stops")
	}
}
