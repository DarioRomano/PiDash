package photos

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestShareAddressesAndPaths(t *testing.T) {
	// As people type them: with the file manager's prefix, with slashes
	// either way, with a folder that tries to climb out.
	s := NewShare(ShareOptions{Host: " smb://nas.local/ ", Name: "/photo/", Folder: `\2024\..\..\Summer\`})
	if s.opt.Host != "nas.local" || s.opt.Name != "photo" || s.opt.Folder != "Summer" {
		t.Errorf("options = %+v", s.opt)
	}
	if got := s.Label(); got != "Share //nas.local/photo/Summer" {
		t.Errorf("label = %q", got)
	}
	if root := s.DefaultSelection(); len(root.Folders) != 1 || root.Folders[0].ID != "." || root.Folders[0].Name != "Summer" || !root.Recursive {
		t.Errorf("before anything is chosen: %+v", root)
	}
	for id, want := range map[string]string{
		".":             `Summer`,
		"Lake":          `Summer\Lake`,
		"Lake/2023":     `Summer\Lake\2023`,
		"Lake/../Other": `Summer\Other`,
	} {
		if got, err := s.in(id); err != nil || got != want {
			t.Errorf("%q is %q (%v), want %q", id, got, err, want)
		}
	}
	for _, id := range []string{"", "..", "../etc", "Lake/../../etc", "/etc", `..\etc`} {
		if got, err := s.in(id); err == nil {
			t.Errorf("%q was taken for a folder of the share: %q", id, got)
		}
	}
	// The whole share: its top is the empty path.
	whole := NewShare(ShareOptions{Host: "192.168.1.20:1445", Name: "photo"})
	if got, err := whole.in("."); err != nil || got != "" {
		t.Errorf("the top of the share is %q (%v)", got, err)
	}
	if whole.DefaultSelection().Folders[0].Name != "photo" {
		t.Errorf("the whole share is named %+v", whole.DefaultSelection())
	}

	// Nothing to go by, and nobody there: said in words.
	if _, err := NewShare(ShareOptions{}).Check(context.Background()); err == nil || !strings.Contains(err.Error(), "both needed") {
		t.Errorf("without a name: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := NewShare(ShareOptions{Host: "127.0.0.1:9", Name: "photo"}).Check(ctx); err == nil || !strings.Contains(err.Error(), "cannot be reached") {
		t.Errorf("nobody there: %v", err)
	}
}

// With PIDASH_TEST_SMB set to the address of a Samba server (host:port) that
// has a share "pictures" for user pi, password secret, and a share "open"
// for guests, both holding the sample pictures (folders Alps, Autumn, Lake),
// the real thing is tried. See scripts/sample_photos.py.
func TestShareAgainstASambaServer(t *testing.T) {
	host := os.Getenv("PIDASH_TEST_SMB")
	if host == "" {
		t.Skip("PIDASH_TEST_SMB is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	s := NewShare(ShareOptions{Host: host, Name: "pictures", Username: "pi", Password: "secret"})
	if n, err := s.Check(ctx); err != nil || n != 3 {
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
	if _, err := s.Details(ctx, it); err != nil {
		t.Errorf("what the camera wrote: %v", err)
	}

	// A connection that went away is made again without anyone noticing.
	s.mu.Lock()
	s.conn.Close()
	s.mu.Unlock()
	if again, err := s.Items(ctx, folders[1]); err != nil || len(again) == 0 {
		t.Errorf("after the connection was lost: %d photos, %v", len(again), err)
	}

	// Only a folder of the share; and a share that guests may read.
	lake := NewShare(ShareOptions{Host: host, Name: "pictures", Folder: "Lake", Username: "pi", Password: "secret"})
	if in, err := lake.Items(ctx, Folder{ID: ".", Path: "Lake"}); err != nil || len(in) == 0 {
		t.Errorf("the folder Lake alone: %d photos, %v", len(in), err)
	}
	if n, err := NewShare(ShareOptions{Host: host, Name: "open"}).Check(ctx); err != nil || n != 3 {
		t.Errorf("as a guest: %d things, %v", n, err)
	}

	// What does not work says why.
	for name, c := range map[string]struct {
		o    ShareOptions
		want string
	}{
		"a wrong password":   {ShareOptions{Host: host, Name: "pictures", Username: "pi", Password: "wrong"}, "user name or password"},
		"a share not there":  {ShareOptions{Host: host, Name: "nothing", Username: "pi", Password: "secret"}, "no such share"},
		"a folder not there": {ShareOptions{Host: host, Name: "pictures", Folder: "Nowhere", Username: "pi", Password: "secret"}, "no such share or folder"},
		"a guest where none": {ShareOptions{Host: host, Name: "pictures"}, "user name or password"},
	} {
		if _, err := NewShare(c.o).Check(ctx); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
