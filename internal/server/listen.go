package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// portFallbacks is how many following ports are tried when the configured one
// is taken.
const portFallbacks = 20

// listen binds addr. If its port is already used by another program, the
// following ports are tried instead of giving up: a dashboard that silently
// fails to start is worse than one on a neighbouring port. It returns the
// listener together with the port asked for and the port obtained.
func listen(addr string) (ln net.Listener, wanted, got int, err error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("server.listen %q: %w", addr, err)
	}
	wanted, convErr := strconv.Atoi(portStr)
	if convErr != nil || wanted == 0 {
		// A named or automatic port: no neighbours to fall back to.
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return nil, 0, 0, err
		}
		return ln, wanted, ln.Addr().(*net.TCPAddr).Port, nil
	}
	for port := wanted; port <= wanted+portFallbacks && port <= 65535; port++ {
		ln, err = net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			return ln, wanted, port, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return nil, wanted, 0, err
		}
	}
	return nil, wanted, 0, fmt.Errorf("ports %d to %d are all in use: %w", wanted, wanted+portFallbacks, err)
}

// URLFile is where the backend records its own address for the launcher:
// $XDG_RUNTIME_DIR/pidash/url. It returns "" when there is no runtime directory.
func URLFile() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "pidash", "url")
}

// writeURLFile records the address the kiosk browser should open.
func writeURLFile(path string, port int) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(fmt.Sprintf("http://127.0.0.1:%d/\n", port)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
