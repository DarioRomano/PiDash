//go:build integration

package integration

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DarioRomano/PiDash/internal/photos"
)

// shareAsALibrary reads the share the way the photo source does.
func shareAsALibrary(t *testing.T, host string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s := photos.NewShare(photos.ShareOptions{Host: host, Name: "pictures", Username: "pi", Password: "secret"})
	// Samba takes a moment after it listens before it lets anyone in.
	var n int
	var err error
	for i := 0; i < 40; i++ {
		if n, err = s.Check(ctx); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil || n != 3 {
		t.Fatalf("looking into the share: %d things, %v", n, err)
	}
	top, err := s.Folders(ctx, "")
	if err != nil || len(top) != 1 || top[0].ID != "." || top[0].Name != "pictures" {
		t.Fatalf("top = %+v, %v", top, err)
	}
	folders, err := s.Folders(ctx, ".")
	if err != nil || len(folders) != 3 || folders[0].Name != "Alps" || folders[0].ID != "Alps" || folders[0].Path != "pictures / Alps" {
		t.Fatalf("folders = %+v, %v", folders, err)
	}
	// The pictures, and not what else lies in the folder.
	items, err := s.Items(ctx, folders[0])
	if err != nil || len(items) != 4 {
		t.Fatalf("photos in Alps = %+v, %v", items, err)
	}
	it := items[0]
	if !strings.HasPrefix(it.Key, "s-") || it.Folder != "pictures / Alps" || it.Size == 0 || it.Taken.IsZero() || !strings.HasPrefix(it.ID, "Alps/") {
		t.Errorf("a photo = %+v", it)
	}
	data, upright, err := s.Fetch(ctx, it, true)
	if err != nil || int64(len(data)) != it.Size || upright || string(data[:2]) != "\xff\xd8" {
		t.Errorf("the photo itself: %d bytes of %d, %v", len(data), it.Size, err)
	}
	if head, _, err := s.Peek(ctx, it); err != nil || len(head) == 0 || len(head) > 256<<10 {
		t.Errorf("its start: %d bytes, %v", len(head), err)
	}

	// Only a folder of the share; and a share that guests may read.
	lake := photos.NewShare(photos.ShareOptions{Host: host, Name: "pictures", Folder: "Lake", Username: "pi", Password: "secret"})
	if in, err := lake.Items(ctx, photos.Folder{ID: ".", Path: "Lake"}); err != nil || len(in) != 2 {
		t.Errorf("the folder Lake alone: %d photos, %v", len(in), err)
	}
	if n, err := photos.NewShare(photos.ShareOptions{Host: host, Name: "open"}).Check(ctx); err != nil || n != 3 {
		t.Errorf("as a guest: %d things, %v", n, err)
	}

	// What does not work says why, in words the settings page can show.
	for name, c := range map[string]struct {
		o    photos.ShareOptions
		want string
	}{
		"a wrong password":   {photos.ShareOptions{Host: host, Name: "pictures", Username: "pi", Password: "wrong"}, "user name or password"},
		"a share not there":  {photos.ShareOptions{Host: host, Name: "nothing", Username: "pi", Password: "secret"}, "no such share"},
		"a folder not there": {photos.ShareOptions{Host: host, Name: "pictures", Folder: "Nowhere", Username: "pi", Password: "secret"}, "no such share or folder"},
		"a guest where none": {photos.ShareOptions{Host: host, Name: "pictures"}, "user name or password"},
	} {
		if _, err := photos.NewShare(c.o).Check(ctx); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// shareThroughTheApp sets the share up on a running PiDash the way the
// settings page does, and looks at what the slideshow then shows.
func shareThroughTheApp(t *testing.T, host string) {
	binary := filepath.Join(repoRoot(t), "bin", "pidash")
	if _, err := os.Stat(binary); err != nil {
		t.Skip(`bin/pidash is missing: run "make build" first`)
	}
	dir := workDir(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	conf := filepath.Join(dir, "config", "config.toml")
	_ = os.MkdirAll(filepath.Dir(conf), 0o755)
	_ = os.WriteFile(conf, nil, 0o644)
	// Development mode: no desktop, TV or sound of this machine is touched.
	cmd := exec.Command(binary, "-config", conf, "-listen", "127.0.0.1:"+strconv.Itoa(port), "-mock")
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+filepath.Join(dir, "xdg"), "XDG_DATA_HOME="+filepath.Join(dir, "data"),
		"XDG_CACHE_HOME="+filepath.Join(dir, "cache"), "XDG_RUNTIME_DIR="+filepath.Join(dir, "run"))
	_ = os.MkdirAll(filepath.Join(dir, "run"), 0o755)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	var health struct {
		OK bool `json:"ok"`
	}
	eventually(t, "PiDash answers", 30*time.Second, func() bool { return getJSON(t, base+"/healthz", &health) == nil && health.OK })

	share := `{"host":"` + host + `","name":"pictures","folder":"","username":"pi","password":"secret"}`
	// "Test connection", with the right and with a wrong password.
	if code, body := send(t, http.MethodPost, base+"/api/settings/test-share", share); code != http.StatusOK || !strings.Contains(body, `"entries":3`) {
		t.Fatalf("testing the connection: %d %s", code, body)
	}
	if code, body := send(t, http.MethodPost, base+"/api/settings/test-share", strings.Replace(share, "secret", "wrong", 1)); code != http.StatusBadGateway || !strings.Contains(body, "user name or password") {
		t.Errorf("with a wrong password: %d %s", code, body)
	}
	if code, body := send(t, http.MethodPut, base+"/api/settings", `{"photos":{"source":"share","intervalSeconds":30,"share":`+share+`}}`); code != http.StatusOK {
		t.Fatalf("saving the share: %d %s", code, body)
	}
	var st struct {
		Source   string `json:"source"`
		Count    int    `json:"count"`
		Indexing bool   `json:"indexing"`
		Error    string `json:"error"`
	}
	eventually(t, "the share's pictures are the slideshow", 60*time.Second, func() bool {
		return getJSON(t, base+"/api/photos", &st) == nil && strings.HasPrefix(st.Source, "Share //") && st.Count == 9
	})
	var list struct {
		Items []struct {
			Key string `json:"key"`
		} `json:"items"`
	}
	if err := getJSON(t, base+"/api/photos/playlist?limit=20", &list); err != nil || len(list.Items) != 9 {
		t.Fatalf("the slideshow: %d photos, %v", len(list.Items), err)
	}
	resp, err := http.Get(base + "/photos/" + list.Items[0].Key)
	if err != nil || resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		t.Fatalf("a photo from the share, prepared for the screen: %v %+v", err, resp)
	}
	resp.Body.Close()
	// The password stays on the Pi; saving again without it keeps it.
	var saved struct {
		Photos struct {
			Share map[string]any `json:"share"`
		} `json:"photos"`
	}
	if err := getJSON(t, base+"/api/settings", &saved); err != nil || saved.Photos.Share["passwordSet"] != true || saved.Photos.Share["password"] != nil {
		t.Errorf("what the page is told: %+v %v", saved.Photos.Share, err)
	}
	if code, body := send(t, http.MethodPut, base+"/api/settings", `{"photos":{"source":"share","intervalSeconds":30,"share":`+strings.Replace(share, "secret", "", 1)+`}}`); code != http.StatusOK {
		t.Fatalf("saving again: %d %s", code, body)
	}
	time.Sleep(2 * time.Second)
	eventually(t, "the pictures are still there without the password being sent again", 60*time.Second, func() bool {
		return getJSON(t, base+"/api/photos", &st) == nil && st.Count == 9 && st.Error == ""
	})
}
