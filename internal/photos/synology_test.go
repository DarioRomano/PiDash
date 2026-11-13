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
	case "Browse.Item/list":
		all := n.items(space, atoi("folder_id"))
		offset, limit := atoi("offset"), atoi("limit")
		list := []map[string]any{}
		for i := offset; i < len(all) && i < offset+limit; i++ {
			it := all[i]
			list = append(list, map[string]any{
				"id": it.ID, "filename": it.Filename, "type": it.Type, "time": 1700000000 + it.ID,
				"additional": map[string]any{"thumbnail": map[string]any{"cache_key": fmt.Sprintf("%d_1", it.ID), "unit_id": it.ID, "xl": "ready"}},
			})
		}
		ok(map[string]any{"list": list})
	case "Thumbnail/get":
		if r.Form.Get("size") != "xl" || r.Form.Get("type") != "unit" || r.Form.Get("cache_key") == "" {
			fail(120)
			return
		}
		n.thumbs++
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(jpegBytes(uniform(640, 480, color.RGBA{30, 200, 60, 255}))) // previews are green
	case "Download/download":
		id, _ := strconv.Atoi(strings.Trim(r.Form.Get("unit_id"), "[]"))
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
	nas := &fakeNAS{t: t, password: "secret"}
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
	if got, want := names(top), "p:=Personal Space; s:=Shared Space"; got != want {
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
	if err != nil || names(top) != "p:=Personal Space" {
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
