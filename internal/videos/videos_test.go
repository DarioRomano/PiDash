package videos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func put(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(entries []Entry) string {
	var out []string
	for _, e := range entries {
		n := e.Name
		if e.Folder {
			n += "/"
		}
		out = append(out, n)
	}
	return strings.Join(out, " | ")
}

func TestNaturalOrder(t *testing.T) {
	for _, pair := range [][2]string{{"Episode 2", "Episode 10"}, {"a", "B"}, {"Film", "Film 2"}, {"S01E09", "S01E10"}, {"007", "8"}} {
		if !natural(pair[0], pair[1]) || natural(pair[1], pair[0]) {
			t.Errorf("%q should come before %q", pair[0], pair[1])
		}
	}
}

func TestLocalFolders(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "Films", "Alien.mkv"), "x")
	put(t, filepath.Join(root, "Films", "Alien.en.srt"), "x")
	put(t, filepath.Join(root, "Films", "notes.txt"), "x")
	put(t, filepath.Join(root, "Films", "Series", "Episode 10.mp4"), "x")
	put(t, filepath.Join(root, "Films", "Series", "Episode 2.mp4"), "x")
	put(t, filepath.Join(root, "Films", "@eaDir", "thumb.mp4"), "x")
	put(t, filepath.Join(root, "Films", ".hidden.mp4"), "x")
	put(t, filepath.Join(root, "secret.mp4"), "x")
	ctx := context.Background()

	lib := &Library{Source: &Local{Roots: []string{filepath.Join(root, "Films")}}, LookPath: func(string) (string, error) { return "/usr/bin/mpv", nil }, Dir: t.TempDir()}
	top, err := lib.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(top.Entries); got != "Series/ | Alien.mkv" || top.Parent != nil || top.Title != "Films" || top.Player != "mpv" {
		t.Fatalf("one folder is the top itself, folders first, only videos: %q, parent %v, title %q, player %q", got, top.Parent, top.Title, top.Player)
	}
	series, err := lib.List(ctx, top.Entries[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(series.Entries); got != "Episode 2.mp4 | Episode 10.mp4" || series.Parent == nil {
		t.Fatalf("episodes in the order people count: %q, parent %v", got, series.Parent)
	}
	if up, err := lib.List(ctx, *series.Parent); err != nil || names(up.Entries) != names(top.Entries) || up.Parent != nil {
		t.Fatalf("the folder above is the top: %v %v", up, err)
	}

	// Nothing outside the folder can be asked for.
	for _, bad := range []string{"0:../secret.mp4", "0:/../secret.mp4", "1:x.mp4", "x", "0:Series/../../secret.mp4", "-1:"} {
		if _, _, err := lib.Play(ctx, bad); err == nil {
			t.Errorf("Play(%q) should be refused", bad)
		}
	}
	if _, err := lib.List(ctx, "0:../"); err != nil {
		t.Errorf("a path that climbs is brought back to the folder, not followed: %v", err)
	}
	if _, err := lib.List(ctx, "0:Gone"); err != ErrNotFound {
		t.Errorf("a folder that is gone: %v", err)
	}
	if _, _, err := lib.Play(ctx, "0:notes.txt"); err != ErrNotFound {
		t.Errorf("only videos are played: %v", err)
	}

	argv, window, err := lib.Play(ctx, top.Entries[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Join(argv, " ")
	if window != "mpv" || argv[len(argv)-1] != filepath.Join(root, "Films", "Alien.mkv") || argv[len(argv)-2] != "--" ||
		!strings.Contains(line, "--sub-file="+filepath.Join(root, "Films", "Alien.en.srt")) || !strings.Contains(line, "--config-dir="+filepath.Join(lib.Dir, "mpv")) {
		t.Fatalf("the player's command: %q (window %q)", argv, window)
	}
	// The settings are written once, and left alone when someone changed them.
	conf := filepath.Join(lib.Dir, "mpv", "input.conf")
	if data, _ := os.ReadFile(conf); !strings.Contains(string(data), "ENTER cycle pause") {
		t.Fatalf("input.conf: %q", data)
	}
	put(t, conf, "ENTER quit\n")
	if _, _, err := lib.Play(ctx, top.Entries[1].Path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(conf); string(data) != "ENTER quit\n" {
		t.Fatalf("a changed input.conf was overwritten: %q", data)
	}

	// Several folders: the top lists them.
	two := &Library{Source: &Local{Roots: []string{filepath.Join(root, "Films"), filepath.Join(root, "Films", "Series")}}, LookPath: lib.LookPath}
	both, err := two.List(ctx, "")
	if err != nil || names(both.Entries) != "Films/ | Series/" || both.Parent != nil {
		t.Fatalf("two folders: %v %v", both, err)
	}
	inside, err := two.List(ctx, both.Entries[1].Path)
	if err != nil || inside.Parent == nil || *inside.Parent != "" || names(inside.Entries) != "Episode 2.mp4 | Episode 10.mp4" {
		t.Fatalf("inside the second folder: %+v %v", inside, err)
	}
}

func TestPlayers(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "a.mp4"), "x")
	only := func(have ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, h := range have {
				if h == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", exec.ErrNotFound
		}
	}
	ctx := context.Background()
	lib := &Library{Source: &Local{Roots: []string{root}}, Dir: t.TempDir(), LookPath: only("vlc")}
	argv, window, err := lib.Play(ctx, "0:a.mp4")
	if err != nil || window != "vlc" || argv[0] != "/usr/bin/vlc" || argv[1] != "--fullscreen" {
		t.Fatalf("VLC where there is no mpv: %q %q %v", argv, window, err)
	}
	lib.LookPath = only()
	if _, _, err := lib.Play(ctx, "0:a.mp4"); err == nil || !strings.Contains(err.Error(), "apt install mpv") {
		t.Fatalf("no player: %v", err)
	}
	if list, _ := lib.List(ctx, ""); list.Player != "" {
		t.Fatalf("no player, but the listing names %q", list.Player)
	}
	lib.Player, lib.LookPath = "celluloid --new-window", only("celluloid")
	argv, window, err = lib.Play(ctx, "0:a.mp4")
	if err != nil || window != "celluloid" || strings.Join(argv, " ") != "/usr/bin/celluloid --new-window "+filepath.Join(root, "a.mp4") {
		t.Fatalf("another player: %q %q %v", argv, window, err)
	}
}

// With the real mpv: the settings PiDash writes are ones it accepts, and a
// video that is broken off is marked as begun.
func TestRealMpv(t *testing.T) {
	mpv, err := exec.LookPath("mpv")
	ffmpeg, err2 := exec.LookPath("ffmpeg")
	if err != nil || err2 != nil {
		t.Skip("needs mpv and ffmpeg")
	}
	root := t.TempDir()
	video := filepath.Join(root, "Clip one.mp4")
	if out, err := exec.Command(ffmpeg, "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=duration=30:size=160x90:rate=10", "-pix_fmt", "yuv420p", video).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg could not make a video: %v %s", err, out)
	}
	lib := &Library{Source: &Local{Roots: []string{root}}, Dir: t.TempDir()}
	ctx := context.Background()
	argv, _, err := lib.Play(ctx, "0:Clip one.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if argv[0] != mpv {
		t.Fatalf("player %q, want %q", argv[0], mpv)
	}
	sock := filepath.Join(t.TempDir(), "s")
	// No screen and no loudspeaker here; and mpv is to say what it does not understand.
	args := append([]string{"--vo=null", "--ao=null", "--force-window=no", "--start=5", "--input-ipc-server=" + sock, "--terminal=yes", "--msg-level=all=warn"}, argv[1:]...)
	for i, a := range args {
		if a == "--no-terminal" {
			args[i] = "--really-quiet=no"
		}
	}
	var out bytes.Buffer
	cmd := exec.Command(mpv, args...)
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	// What the remote's Back does.
	var conn net.Conn
	for i := 0; i < 100 && conn == nil; i++ {
		time.Sleep(100 * time.Millisecond)
		conn, _ = net.Dial("unix", sock)
	}
	if conn == nil {
		_ = cmd.Process.Kill()
		t.Fatalf("mpv did not start: %s", out.String())
	}
	time.Sleep(700 * time.Millisecond)
	fmt.Fprintln(conn, `{"command":["keypress","ESC"]}`)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("Back did not end mpv: %s", out.String())
	}
	// What the libraries under mpv say about graphics cards this machine lacks
	// is not about the settings; nor is its control connection, which the
	// test holds, being cut off as mpv quits ("[ipc_0] Read error").
	var said []string
	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.HasPrefix(line, "[ffmpeg]") && !strings.HasPrefix(line, "[vaapi]") && !strings.HasPrefix(line, "[vdpau]") && !strings.HasPrefix(line, "[ipc") {
			said = append(said, line)
		}
	}
	if s := strings.Join(said, "\n"); strings.Contains(strings.ToLower(s), "error") || strings.Contains(s, "nknown") || strings.Contains(s, "not found") || strings.Contains(s, "Could not") {
		t.Fatalf("mpv complained about the settings:\n%s", s)
	}
	list, err := lib.List(ctx, "")
	if err != nil || len(list.Entries) != 1 || !list.Entries[0].Started {
		got, _ := filepath.Glob(filepath.Join(lib.Dir, "mpv", "watch_later", "*"))
		t.Fatalf("the video should be marked as begun: %+v %v (mpv wrote %v)\n%s", list.Entries, err, got, out.String())
	}
}

// A pretend File Station: two shared folders, one with a film in it.
func fakeNAS(t *testing.T, film []byte) (*httptest.Server, *int) {
	t.Helper()
	logins := 0
	ok := func(w http.ResponseWriter, data any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}
	fail := func(w http.ResponseWriter, code int) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]int{"code": code}})
	}
	file := func(name, path string, dir bool, size int) map[string]any {
		return map[string]any{"name": name, "path": path, "isdir": dir, "additional": map[string]int{"size": size}}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("api") == "SYNO.API.Auth" {
			if r.Form.Get("account") != "tv" || r.Form.Get("passwd") != "pw" {
				fail(w, 400)
				return
			}
			logins++
			ok(w, map[string]string{"sid": fmt.Sprintf("sid%d", logins)})
			return
		}
		// The first session has run out: PiDash signs in again.
		if r.Form.Get("_sid") != fmt.Sprintf("sid%d", logins) || logins < 2 {
			logins = max(logins, 1)
			w.Header().Set("Content-Type", "application/json")
			fail(w, 119)
			return
		}
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.FileStation.List.list_share":
			ok(w, map[string]any{"shares": []any{file("video", "/video", true, 0), file("homes", "/homes", true, 0), file("#recycle", "/#recycle", true, 0)}})
		case "SYNO.FileStation.List.list":
			switch r.Form.Get("folder_path") {
			case "/video":
				ok(w, map[string]any{"total": 4, "files": []any{
					file("Alien.mkv", "/video/Alien.mkv", false, len(film)), file("Alien.srt", "/video/Alien.srt", false, 10),
					file("Kids", "/video/Kids", true, 0), file("@eaDir", "/video/@eaDir", true, 0),
				}})
			case "/video/Kids":
				ok(w, map[string]any{"total": 0, "files": []any{}})
			default:
				fail(w, 408)
			}
		case "SYNO.FileStation.Download.download":
			if r.Form.Get("path") != `["/video/Alien.mkv"]` || r.Form.Get("mode") != `"open"` {
				w.Header().Set("Content-Type", "application/json")
				fail(w, 408)
				return
			}
			http.ServeContent(w, r, "Alien.mkv", time.Time{}, bytes.NewReader(film))
		default:
			fail(w, 102)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &logins
}

func TestSynologyFolders(t *testing.T) {
	film := bytes.Repeat([]byte("0123456789"), 1000)
	nas, logins := fakeNAS(t, film)
	ctx := context.Background()
	dir := t.TempDir()
	newSource := func(folders ...string) *Synology {
		return NewSynology(SynologyOptions{URL: nas.URL, Username: "tv", Password: "pw", Folders: folders, Self: func() string { return "http://127.0.0.1:1" }, SecretPath: filepath.Join(dir, "secret")})
	}
	src := newSource()
	lib := &Library{Source: src, LookPath: func(string) (string, error) { return "/usr/bin/mpv", nil }, Dir: t.TempDir()}

	top, err := lib.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if names(top.Entries) != "homes/ | video/" || top.Parent != nil {
		t.Fatalf("the shared folders: %q", names(top.Entries))
	}
	if *logins != 2 {
		t.Fatalf("signed in %d times; a session that ran out should be renewed once", *logins)
	}
	video, err := lib.List(ctx, "/video")
	if err != nil || names(video.Entries) != "Kids/ | Alien.mkv" || video.Parent == nil || *video.Parent != "" || video.Entries[1].Size != int64(len(film)) {
		t.Fatalf("/video: %+v %v", video, err)
	}
	kids, err := lib.List(ctx, "/video/Kids")
	if err != nil || len(kids.Entries) != 0 || kids.Parent == nil || *kids.Parent != "/video" {
		t.Fatalf("/video/Kids: %+v %v", kids, err)
	}
	if _, err := lib.List(ctx, "/nowhere"); err != ErrNotFound {
		t.Fatalf("a folder that is not there: %v", err)
	}
	if _, _, err := lib.Play(ctx, "/video/Gone.mkv"); err != ErrNotFound {
		t.Fatalf("a video that is not listed: %v", err)
	}
	for _, bad := range []string{"/video/../etc", "video", "/", "/video/"} {
		if _, err := lib.List(ctx, bad); err != ErrNotFound {
			t.Errorf("List(%q) = %v, want it refused", bad, err)
		}
	}

	// The player is given an address on the Pi, the same every time, and the subtitles beside it.
	argv, _, err := lib.Play(ctx, "/video/Alien.mkv")
	if err != nil {
		t.Fatal(err)
	}
	target := argv[len(argv)-1]
	again, _ := newSource().Target(ctx, "/video/Alien.mkv")
	if !strings.HasPrefix(target, "http://127.0.0.1:1/api/videos/stream/") || !strings.HasSuffix(target, ".mkv") || target != again {
		t.Fatalf("the video's address: %q, and after a restart %q", target, again)
	}
	if strings.Contains(target, "Alien") || strings.Contains(target, "sid") {
		t.Fatalf("the address says too much: %q", target)
	}
	if line := strings.Join(argv, " "); strings.Count(line, "--sub-file=http://127.0.0.1:1/api/videos/stream/") != 1 {
		t.Fatalf("the subtitles: %q", line)
	}

	// Through that address the player gets the part of the file it asks for.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/videos/stream/{token}/{name}", func(w http.ResponseWriter, r *http.Request) {
		src.Stream(w, r, r.PathValue("token"), r.PathValue("name"))
	})
	pi := httptest.NewServer(mux)
	defer pi.Close()
	get := func(path, byteRange string) (int, string, []byte) {
		req, _ := http.NewRequest(http.MethodGet, pi.URL+path, nil)
		if byteRange != "" {
			req.Header.Set("Range", byteRange)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Range"), body
	}
	path := strings.TrimPrefix(target, "http://127.0.0.1:1")
	if code, _, body := get(path, ""); code != 200 || !bytes.Equal(body, film) {
		t.Fatalf("the whole film: HTTP %d, %d bytes", code, len(body))
	}
	if code, cr, body := get(path, "bytes=5000-5009"); code != 206 || string(body) != "0123456789" || cr != "bytes 5000-5009/10000" {
		t.Fatalf("a part of it: HTTP %d, %q, %q", code, cr, body)
	}
	// An address nobody signed is not passed on.
	parts := strings.Split(path, "/")
	parts[4] = strings.Repeat("0", len(parts[4]))
	if code, _, _ := get(strings.Join(parts, "/"), ""); code != 404 {
		t.Fatalf("a forged address: HTTP %d", code)
	}

	// Only the folders that were set up.
	one := &Library{Source: newSource("/video/Kids/"), LookPath: lib.LookPath}
	only, err := one.List(ctx, "")
	if err != nil || only.Title != "Kids" || only.Parent != nil {
		t.Fatalf("one folder set up is the top: %+v %v", only, err)
	}
	if _, err := one.List(ctx, "/video"); err != ErrNotFound {
		t.Fatalf("outside what was set up: %v", err)
	}
	if _, err := one.Source.Target(ctx, "/video/Alien.mkv"); err != ErrNotFound {
		t.Fatalf("a video outside what was set up: %v", err)
	}

	// A wrong password is said plainly.
	wrong := NewSynology(SynologyOptions{URL: nas.URL, Username: "tv", Password: "no", Self: func() string { return "" }})
	if _, _, _, _, err := wrong.List(ctx, ""); err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatalf("wrong password: %v", err)
	}
}

func TestPending(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, err := Pending(); err == nil {
		t.Fatal("nothing was noted, yet something is pending")
	}
	if err := SetPending([]string{"/usr/bin/mpv", "--", "/films/a b.mkv"}); err != nil {
		t.Fatal(err)
	}
	argv, err := Pending()
	if err != nil || len(argv) != 3 || argv[2] != "/films/a b.mkv" {
		t.Fatalf("%q %v", argv, err)
	}
	if err := SetPending([]string{"mpv"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Pending(); err == nil {
		t.Fatal("a command without a full path must not be run")
	}
}
