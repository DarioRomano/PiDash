package videos

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// The player is started the way apps are: by a second pidash that turns
// itself into it. Which video it is to play waits for it in a small file.

func pendingPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "pidash-video.json")
}

// SetPending notes the command that is to be run next.
func SetPending(argv []string) error {
	data, err := json.Marshal(argv)
	if err != nil {
		return err
	}
	p := pendingPath()
	if err := os.WriteFile(p+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

// Pending returns the command that was noted.
func Pending() ([]string, error) {
	data, err := os.ReadFile(pendingPath())
	if err != nil {
		return nil, err
	}
	var argv []string
	if err := json.Unmarshal(data, &argv); err != nil || len(argv) == 0 || !filepath.IsAbs(argv[0]) {
		return nil, errors.New("no video was chosen")
	}
	return argv, nil
}
