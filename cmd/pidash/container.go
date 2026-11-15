package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/DarioRomano/PiDash/internal/config"
)

// What a container needs of the program.

// healthcheck asks the PiDash that runs on this machine whether it is well:
// what a container's HEALTHCHECK runs, since its image has no other program
// to ask with. It returns the exit code: 0 when PiDash answered.
func healthcheck(cfgPath, listen string) int {
	if listen == "" {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			cfg = config.Default()
		}
		listen = cfg.Server.Listen
	}
	url, err := healthURL(listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 2
	}
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: PiDash answered", res.Status)
		return 1
	}
	return 0
}

// healthURL is where the PiDash listening on addr (":8080", "0.0.0.0:8080",
// "192.168.1.5:8080") is asked from the same machine.
func healthURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("server.listen %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}

// mountedPhotos makes the folder a container was given its pictures in (the
// image names it in PIDASH_PHOTOS) the photos' folder, unless the settings
// file names one itself. With no source chosen there either, the pictures in
// it are shown: mounting them is saying so.
func mountedPhotos(cfg *config.Config, dir string) {
	if dir == "" || cfg.Photos.Folder.Path != "" {
		return
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return
	}
	cfg.Photos.Folder.Path = dir
	if cfg.Photos.Source == "" {
		cfg.Photos.Source = "folder"
	}
}
