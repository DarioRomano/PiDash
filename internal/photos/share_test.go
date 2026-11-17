package photos

import (
	"context"
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

	// Nothing to go by, and nobody there: said in words. (A real share on
	// a real server is tried in test/integration.)
	if _, err := NewShare(ShareOptions{}).Check(context.Background()); err == nil || !strings.Contains(err.Error(), "both needed") {
		t.Errorf("without a name: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := NewShare(ShareOptions{Host: "127.0.0.1:9", Name: "photo"}).Check(ctx); err == nil || !strings.Contains(err.Error(), "cannot be reached") {
		t.Errorf("nobody there: %v", err)
	}
}
