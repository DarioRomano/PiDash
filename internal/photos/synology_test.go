package photos

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/image/draw"
)

// fakeNAS imitates the parts of DSM 7 / Synology Photos that PiDash uses.
//
// Personal Space:           Shared Space:
//
//	/ (1)                     / (100)
//	├─ 2024 (2)               └─ Family (101)   1 photo
//	│  └─ Holiday (3)  2 photos + 1 video + 1 HEIC
//	└─ Big (4)         501 photos (two pages)
//
// Albums:
//
//	Favourites (7)   own; photo 10 from the Personal Space, 500 from the Shared Space
//	For the TV (9)   shared by someone else, readable only with its passphrase;
//	                 2 photos + 1 video belonging to user 3
type fakeNAS struct {
	t  *testing.T
	mu sync.Mutex

	password     string
	sharedOff    bool
	sid          int
	logins       int
	thumbs       int
	downloads    int
	breakFolders bool
	// oldAlbums makes the NAS reject the combined album list, like an older version.
	oldAlbums bool
	// noLibraries refuses both libraries, as for an account that only views shared albums.
	noLibraries bool
	// viewerOnly forbids downloading the full files of the shared album.
	viewerOnly bool
	// hideOwner leaves out who a photo belongs to.
	hideOwner  bool
	passphrase string
	albumCalls int
}

type fakeAlbum struct {
	ID         int
	Name       string
	Passphrase string
	Items      []fakeItem
	Owner      []int // owner_user_id per item
}

func (n *fakeNAS) albums() (own, shared []fakeAlbum) {
	own = []fakeAlbum{{7, "Favourites", "", []fakeItem{{10, "IMG_0010.JPG", "photo"}, {500, "family.jpg", "photo"}}, []int{5, 0}}}
	shared = []fakeAlbum{{9, "For the TV", n.passphrase,
		[]fakeItem{{700, "tv-1.jpg", "photo"}, {701, "tv-2.jpg", "photo"}, {702, "tv.mov", "video"}}, []int{3, 3, 3}}}
	return own, shared
}

// unquote reads a parameter sent the way the Synology web interface sends strings.
func unquote(v string) string {
	if s, err := strconv.Unquote(v); err == nil {
		return s
	}
	return v
}

type fakeItem struct {
	ID       int
	Filename string
	Type     string
}

func uniform(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: c}, image.Point{}, draw.Src)
	return img
}

func jpegBytes(img image.Image) []byte {
	var b bytes.Buffer
	jpeg.Encode(&b, img, &jpeg.Options{Quality: 90})
	return b.Bytes()
}

func (n *fakeNAS) items(space string, folder int) []fakeItem {
	switch {
	case space == "Foto" && folder == 3:
		return []fakeItem{{10, "IMG_0010.JPG", "photo"}, {11, "IMG_0011.jpg", "photo"}, {12, "clip.mp4", "video"}, {13, "IMG_0013.HEIC", "photo"}}
	case space == "Foto" && folder == 4:
		out := make([]fakeItem, 501)
		for i := range out {
			out[i] = fakeItem{1000 + i, fmt.Sprintf("big-%03d.jpg", i), "photo"}
		}
		return out
	case space == "FotoTeam" && folder == 101:
		return []fakeItem{{500, "family.jpg", "photo"}}
	}
	return nil
}

func (n *fakeNAS) folders(space string, parent int) [][2]any {
	switch {
	case space == "Foto" && parent == 1:
		return [][2]any{{2, "/2024"}, {4, "/Big"}}
	case space == "Foto" && parent == 2:
		return [][2]any{{3, "/2024/Holiday"}}
	case space == "FotoTeam" && parent == 100:
		return [][2]any{{101, "/Family"}}
	}
	return nil
}

func (n *fakeNAS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/webapi/entry.cgi" {
		http.NotFound(w, r)
		return
	}
	r.ParseForm()
	api, method := r.Form.Get("api"), r.Form.Get("method")
	ok := func(data any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}
	fail := func(code int) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]int{"code": code}})
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if api == "SYNO.API.Auth" {
		if r.Form.Get("account") != "pidash" || r.Form.Get("passwd") != n.password {
			fail(400)
			return
		}
		n.sid++
		n.logins++
		ok(map[string]string{"sid": "sid-" + strconv.Itoa(n.sid)})
		return
	}
	if n.sid == 0 || r.Form.Get("_sid") != "sid-"+strconv.Itoa(n.sid) {
		fail(119)
		return
	}

	space := "Foto"
	if strings.HasPrefix(api, "SYNO.FotoTeam.") {
		space = "FotoTeam"
		if n.sharedOff {
			fail(105)
			return
		}
	}
	if n.noLibraries && strings.Contains(api, "Browse.Folder") {
		fail(105)
		return
	}
	// Photos 700.. belong to another user: nothing about them is served without the passphrase.
	private := func(id int) bool { return id >= 700 && id < 800 && unquote(r.Form.Get("passphrase")) != n.passphrase }
	albumList := func(albums []fakeAlbum) {
		n.albumCalls++
		list := []map[string]any{}
		for _, a := range albums {
			list = append(list, map[string]any{"id": a.ID, "name": a.Name, "item_count": len(a.Items), "passphrase": a.Passphrase})
		}
		ok(map[string]any{"list": list})
	}
	root := map[string]int{"Foto": 1, "FotoTeam": 100}[space]
	atoi := func(k string) int { v, _ := strconv.Atoi(r.Form.Get(k)); return v }

	switch strings.TrimPrefix(strings.TrimPrefix(api, "SYNO.FotoTeam."), "SYNO.Foto.") + "/" + method {
	case "Browse.Folder/get":
		ok(map[string]any{"folder": map[string]any{"id": root, "name": "/", "parent": root}})
	case "Browse.Folder/list":
		if n.breakFolders {
			fail(105)
			return
		}
		list := []map[string]any{}
		for _, f := range n.folders(space, atoi("id")) {
			list = append(list, map[string]any{"id": f[0], "name": f[1], "parent": atoi("id")})
		}
		ok(map[string]any{"list": list})
	case "Browse.Album/list":
		own, shared := n.albums()
		switch r.Form.Get("category") {
		case "":
			albumList(own)
		case "normal_share_with_me":
			if n.oldAlbums {
				fail(120)
				return
			}
			albumList(append(own, shared...))
		default:
			fail(120)
		}
	case "Sharing.Misc/list_shared_with_me_album":
		_, shared := n.albums()
		albumList(shared)
	case "Browse.Item/list":
		all := n.items(space, atoi("folder_id"))
		var owners []int
		if space == "Foto" && r.Form.Get("folder_id") == "" {
			own, shared := n.albums()
			found := false
			for _, a := range append(own, shared...) {
				byPass := a.Passphrase != "" && unquote(r.Form.Get("passphrase")) == a.Passphrase
				byID := a.Passphrase == "" && atoi("album_id") == a.ID
				if byPass || byID {
					all, owners, found = a.Items, a.Owner, true
				}
			}
			if !found {
				fail(105)
				return
			}
		}
		offset, limit := atoi("offset"), atoi("limit")
		list := []map[string]any{}
		for i := offset; i < len(all) && i < offset+limit; i++ {
			it := all[i]
			entry := map[string]any{
				"id": it.ID, "filename": it.Filename, "type": it.Type, "time": 1700000000 + it.ID,
				"additional": map[string]any{"thumbnail": map[string]any{"cache_key": fmt.Sprintf("%d_1", it.ID), "unit_id": it.ID, "xl": "ready"}},
			}
			if owners != nil && !n.hideOwner {
				entry["owner_user_id"] = owners[i]
			}
			list = append(list, entry)
		}
		ok(map[string]any{"list": list})
	case "Thumbnail/get":
		if r.Form.Get("size") != "xl" || r.Form.Get("type") != "unit" || r.Form.Get("cache_key") == "" {
			fail(120)
			return
		}
		// A photo is only served by the library it lives in.
		if id := atoi("id"); private(id) || (id == 500) != (space == "FotoTeam") {
			fail(105)
			return
		}
		n.thumbs++
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes(uniform(640, 480, color.RGBA{30, 200, 60, 255}))) // previews are green
	case "Download/download":
		id, _ := strconv.Atoi(strings.Trim(r.Form.Get("unit_id"), "[]"))
		if private(id) || (n.viewerOnly && id >= 700 && id < 800) {
			fail(105)
			return
		}
		n.downloads++
		w.Header().Set("Content-Type", "application/octet-stream")
		if id == 13 {
			w.Write([]byte("\x00\x00\x00\x18ftypheic not decodable here"))
			return
		}
		// Full files are red, stored sideways with an EXIF "rotate" tag like a phone photo.
		w.Write(withExifOrientation(jpegBytes(uniform(800, 600, color.RGBA{220, 30, 30, 255})), 6, binary.BigEndian))
	default:
		fail(102)
	}
}

func newFakeNAS(t *testing.T) (*fakeNAS, *Synology) {
	t.Helper()
	nas := &fakeNAS{t: t, password: "secret", passphrase: "Xy12AbC"}
	srv := httptest.NewServer(nas)
	t.Cleanup(srv.Close)
	return nas, NewSynology(SynologyOptions{URL: srv.URL + "/", Username: "pidash", Password: "secret"})
}

func names(fs []Folder) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.ID+"="+f.Path)
	}
	return strings.Join(out, "; ")
}

func TestSynologyBrowsesBothLibraries(t *testing.T) {
	_, syn := newFakeNAS(t)
	ctx := context.Background()

	top, err := syn.Folders(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(top), "p:=Personal Space; s:=Shared Space; a:=Albums"; got != want {
		t.Errorf("top level = %s, want %s", got, want)
	}
	personal, err := syn.Folders(ctx, "p:")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(personal), "p:2=Personal Space / 2024; p:4=Personal Space / Big"; got != want {
		t.Errorf("personal = %s, want %s", got, want)
	}
	sub, _ := syn.Folders(ctx, "p:2")
	if got, want := names(sub), "p:3=Personal Space / 2024 / Holiday"; got != want || sub[0].Name != "Holiday" {
		t.Errorf("sub = %s (name %q), want %s", got, sub[0].Name, want)
	}
	shared, _ := syn.Folders(ctx, "s:")
	if got, want := names(shared), "s:101=Shared Space / Family"; got != want {
		t.Errorf("shared = %s, want %s", got, want)
	}
}

func TestSynologyWithoutSharedSpace(t *testing.T) {
	nas, syn := newFakeNAS(t)
	nas.sharedOff = true
	top, err := syn.Folders(context.Background(), "")
	if err != nil || names(top) != "p:=Personal Space; a:=Albums" {
		t.Errorf("top = %s, err = %v; want only the Personal Space", names(top), err)
	}
}

func TestSynologyListsPhotosAcrossPagesAndSkipsVideos(t *testing.T) {
	_, syn := newFakeNAS(t)
	ctx := context.Background()

	holiday, err := syn.Items(ctx, Folder{ID: "p:3", Path: "Personal Space / 2024 / Holiday"})
	if err != nil {
		t.Fatal(err)
	}
	if len(holiday) != 3 {
		t.Fatalf("got %d items, want 3 (the video is skipped)", len(holiday))
	}
	if it := holiday[0]; it.Key != "p-10" || it.Name != "IMG_0010.JPG" || it.Version != "10_1" || it.Folder != "Personal Space / 2024 / Holiday" || it.Taken.Unix() != 1700000010 {
		t.Errorf("item = %+v", it)
	}
	big, err := syn.Items(ctx, Folder{ID: "p:4"})
	if err != nil || len(big) != 501 {
		t.Errorf("big folder: %d items, err %v; want 501", len(big), err)
	}
	shared, _ := syn.Items(ctx, Folder{ID: "s:101"})
	if len(shared) != 1 || shared[0].Key != "s-500" {
		t.Errorf("shared items = %+v", shared)
	}
}

func TestSynologySignsInAgainWhenTheSessionExpires(t *testing.T) {
	nas, syn := newFakeNAS(t)
	ctx := context.Background()
	if _, err := syn.Folders(ctx, "p:"); err != nil {
		t.Fatal(err)
	}
	nas.mu.Lock()
	nas.sid += 7 // the NAS forgot our session
	nas.mu.Unlock()
	if _, err := syn.Folders(ctx, "p:"); err != nil {
		t.Fatalf("expired session was not renewed: %v", err)
	}
	if nas.logins != 2 {
		t.Errorf("logins = %d, want 2", nas.logins)
	}
}

func TestSynologyLoginProblemsAreExplained(t *testing.T) {
	nas, syn := newFakeNAS(t)
	nas.password = "changed"
	_, err := syn.Folders(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "wrong username or password") {
		t.Errorf("err = %v", err)
	}

	notNAS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("<html>router</html>")) }))
	defer notNAS.Close()
	_, err = NewSynology(SynologyOptions{URL: notNAS.URL, Username: "a", Password: "b"}).Folders(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "does not look like a Synology NAS") {
		t.Errorf("err = %v", err)
	}

	_, err = NewSynology(SynologyOptions{URL: "http://127.0.0.1:1", Username: "a", Password: "b"}).Folders(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "cannot reach the NAS") {
		t.Errorf("err = %v", err)
	}
}

func TestSynologyFetch(t *testing.T) {
	nas, syn := newFakeNAS(t)
	ctx := context.Background()
	item := Item{Key: "p-10", Name: "IMG_0010.JPG", Version: "10_1", ID: "p:10"}

	preview, upright, err := syn.Fetch(ctx, item, false)
	if err != nil || !upright || !bytes.HasPrefix(preview, []byte{0xFF, 0xD8}) {
		t.Errorf("preview: upright=%v err=%v", upright, err)
	}
	full, upright, err := syn.Fetch(ctx, item, true)
	if err != nil || upright || exifOrientation(full) != 6 {
		t.Errorf("original: upright=%v err=%v orientation=%d", upright, err, exifOrientation(full))
	}
	if nas.thumbs != 1 || nas.downloads != 1 {
		t.Errorf("thumbs=%d downloads=%d", nas.thumbs, nas.downloads)
	}
}

func TestSynologyListsOwnAndSharedAlbums(t *testing.T) {
	for _, old := range []bool{false, true} {
		nas, syn := newFakeNAS(t)
		nas.oldAlbums = old
		albums, err := syn.Folders(context.Background(), "a:")
		if err != nil {
			t.Fatalf("old=%v: %v", old, err)
		}
		if got, want := names(albums), "a:7=Albums / Favourites; a:9=Albums / For the TV"; got != want {
			t.Errorf("old=%v: albums = %s, want %s", old, got, want)
		}
		if a := albums[1]; !a.Leaf || a.Detail != "3 items" || strings.Contains(a.ID, nas.passphrase) {
			t.Errorf("old=%v: album = %+v; want a leaf with a count and no passphrase in its id", old, a)
		}
		// Nothing is inside an album but photos.
		if inside, err := syn.Folders(context.Background(), "a:9"); err != nil || len(inside) != 0 {
			t.Errorf("old=%v: inside an album: %v, %v", old, inside, err)
		}
	}
}

func TestSynologyAccountWithOnlyASharedAlbum(t *testing.T) {
	nas, syn := newFakeNAS(t)
	nas.noLibraries = true
	top, err := syn.Folders(context.Background(), "")
	if err != nil || names(top) != "a:=Albums" {
		t.Errorf("top = %s, err = %v; want only Albums", names(top), err)
	}
}

func TestSynologyReadsASharedAlbum(t *testing.T) {
	nas, syn := newFakeNAS(t)
	ctx := context.Background()

	items, err := syn.Items(ctx, Folder{ID: "a:9", Path: "Albums / For the TV"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (the video is skipped)", len(items))
	}
	if it := items[0]; it.Key != "p-700" || it.Folder != "Albums / For the TV" || it.Version != "700_1" {
		t.Errorf("item = %+v", it)
	}
	if _, upright, err := syn.Fetch(ctx, items[0], false); err != nil || !upright {
		t.Errorf("preview of a shared photo: upright=%v err=%v", upright, err)
	}
	if _, _, err := syn.Fetch(ctx, items[0], true); err != nil {
		t.Errorf("full file of a shared photo: %v", err)
	}

	// With permission to view only, the full file is refused and says so,
	// which makes the slideshow use the preview.
	nas.viewerOnly = true
	if _, _, err := syn.Fetch(ctx, items[0], true); err == nil {
		t.Error("the full file was returned although downloading is not allowed")
	}

	// Sharing was switched off and on: the album has a new passphrase.
	nas.mu.Lock()
	nas.passphrase = "NewPass99"
	nas.mu.Unlock()
	syn.mu.Lock()
	syn.albumsAt = time.Time{} // as if a minute had passed
	syn.mu.Unlock()
	items, err = syn.Items(ctx, Folder{ID: "a:9", Path: "Albums / For the TV"})
	if err != nil || len(items) != 2 {
		t.Fatalf("after a new passphrase: %d items, err %v", len(items), err)
	}
	if _, _, err := syn.Fetch(ctx, items[1], false); err != nil {
		t.Errorf("preview after a new passphrase: %v", err)
	}

	if _, err := syn.Items(ctx, Folder{ID: "a:12345", Path: "Albums / Gone"}); err == nil || !strings.Contains(err.Error(), "no longer") {
		t.Errorf("missing album: err = %v", err)
	}
}

func TestSynologyAlbumMixesBothLibraries(t *testing.T) {
	for _, hideOwner := range []bool{false, true} {
		nas, syn := newFakeNAS(t)
		nas.hideOwner = hideOwner
		ctx := context.Background()

		items, err := syn.Items(ctx, Folder{ID: "a:7", Path: "Albums / Favourites"})
		if err != nil || len(items) != 2 {
			t.Fatalf("hideOwner=%v: %d items, err %v", hideOwner, len(items), err)
		}
		if !hideOwner && (items[0].Key != "p-10" || items[1].Key != "s-500") {
			// The same keys as when the photos are found through their folders,
			// so a photo chosen both ways is shown once.
			t.Errorf("keys = %s, %s; want p-10, s-500", items[0].Key, items[1].Key)
		}
		for _, it := range items {
			if _, _, err := syn.Fetch(ctx, it, false); err != nil {
				t.Errorf("hideOwner=%v: preview of %s: %v", hideOwner, it.Name, err)
			}
		}
	}
}

func TestSynologyAlbumListIsReusedWhileIndexing(t *testing.T) {
	nas, syn := newFakeNAS(t)
	ctx := context.Background()
	for _, id := range []string{"a:7", "a:9", "a:7"} {
		if _, err := syn.Items(ctx, Folder{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if nas.albumCalls != 1 {
		t.Errorf("the album list was requested %d times, want 1", nas.albumCalls)
	}
}
