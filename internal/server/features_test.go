package server

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DarioRomano/PiDash/internal/apps"
	"github.com/DarioRomano/PiDash/internal/bluetooth"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/photos"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/settings"
	"github.com/DarioRomano/PiDash/internal/tv"
	"github.com/DarioRomano/PiDash/internal/videos"
)

func getBody(t *testing.T, url string) (int, string) {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// featureServer is a server with a settings store, sample photos, Bluetooth
// and whatever else a test adds.
func featureServer(t *testing.T, mutate func(*config.Config, *Deps)) (*Server, *httptest.Server, *apps.Manager) {
	t.Helper()
	cfg := config.Default()
	cfg.Apps = []config.App{
		{ID: "netflix", Name: "Netflix", URL: "https://www.netflix.com", Mode: "desktop", Browser: "chromium"},
		{ID: "retro", Name: "RetroArch", Command: "retroarch", Mode: "desktop"},
	}
	store, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	am := apps.NewManager(cfg.Apps, &apps.Mock{}, log)
	ph := photos.New(photos.Options{Source: photos.Mock{}, StatePath: filepath.Join(t.TempDir(), "photos.json"), Log: log})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go ph.Run(ctx)
	deps := Deps{
		Kiosk: kiosk.New("", true, log), Apps: am, Photos: ph, Bluetooth: bluetooth.NewMock(),
		Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}),
		NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
		Settings:  store, BaseListen: cfg.Server.Listen,
	}
	if mutate != nil {
		mutate(&cfg, &deps)
	}
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	s := New(cfg, log, static, deps)
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	for i := 0; ph.Status().Count == 0; i++ {
		if i > 200 {
			t.Fatal("the sample photos did not appear")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return s, srv, am
}

func TestControllersAndHeadphonesAreDisconnectedWhenTheTVGoesOff(t *testing.T) {
	connected := func(blue bluetooth.Manager) string {
		var on []string
		for _, d := range blue.Status(context.Background()).Devices {
			if d.Connected {
				on = append(on, d.Name)
			}
		}
		return strings.Join(on, ", ")
	}
	blue := bluetooth.NewMock()
	// A keyboard is connected too: it stays, the TV's own remote may be one.
	_ = blue.Search(context.Background(), true)
	_ = blue.Pair(context.Background(), "AA:00:00:00:00:04")
	_ = blue.Connect(context.Background(), "AA:00:00:00:00:02")
	s, _, _ := featureServer(t, func(_ *config.Config, d *Deps) { d.Bluetooth = blue })
	if got := connected(blue); got != "Keyboard K380, Living room speaker, Xbox Wireless Controller" {
		t.Fatalf("connected at first: %s", got)
	}
	s.disconnectForTheNight()
	if got := connected(blue); got != "Keyboard K380" {
		t.Fatalf("after the TV went off: %s", got)
	}

	// Switched off in the settings, everything stays connected.
	blue = bluetooth.NewMock()
	s, _, _ = featureServer(t, func(c *config.Config, d *Deps) { d.Bluetooth, c.Bluetooth.DisconnectWithTV = blue, false })
	s.disconnectForTheNight()
	if got := connected(blue); got != "Xbox Wireless Controller" {
		t.Fatalf("with the setting off: %s", got)
	}
}

func TestQuickChangesFromTheTV(t *testing.T) {
	s, srv, am := featureServer(t, nil)

	// A tile is hidden: the dashboard is told at once, without a restart.
	code, body := post(t, srv.URL+"/api/tiles/hidden", `{"tile":"status","hidden":true}`)
	var cfg config.Client
	if err := json.Unmarshal([]byte(body), &cfg); code != 200 || err != nil || strings.Join(cfg.Display.HiddenTiles, ",") != "status" {
		t.Fatalf("hide a tile: %d %s", code, body)
	}
	if _, body := getBody(t, srv.URL+"/api/config"); !strings.Contains(body, `"hidden_tiles":["status"]`) {
		t.Fatalf("the config does not say so: %s", body)
	}
	if code, _ := post(t, srv.URL+"/api/tiles/hidden", `{"tile":"settings","hidden":true}`); code != 422 {
		t.Fatalf("hiding the Settings tile: HTTP %d", code)
	}
	if restartRequested(s) {
		t.Fatal("hiding a tile asked for a restart")
	}

	// How long a photo stays.
	code, body = post(t, srv.URL+"/api/photos/interval", `{"seconds":120}`)
	if code != 200 || !strings.Contains(body, `"intervalSeconds":120`) {
		t.Fatalf("interval: %d %s", code, body)
	}
	if code, _ := post(t, srv.URL+"/api/photos/interval", `{"seconds":1}`); code != 422 {
		t.Fatalf("one second a photo: HTTP %d", code)
	}

	// An app leaves the row.
	code, body = post(t, srv.URL+"/api/apps/remove", `{"id":"netflix"}`)
	if code != 200 || strings.Contains(body, "Netflix") || !strings.Contains(body, "RetroArch") {
		t.Fatalf("remove an app: %d %s", code, body)
	}
	if st := am.Status(); len(st.Apps) != 1 {
		t.Fatalf("apps = %+v", st.Apps)
	}
	if code, _ := post(t, srv.URL+"/api/apps/remove", `{"id":"netflix"}`); code == 200 {
		t.Fatal("removing it twice succeeded")
	}

	// What is new comes with the config, and names nothing that is not there.
	if len(cfg.WhatsNew) == 0 {
		t.Fatal("the config says nothing about what is new")
	}
}

func TestQuickChangesNeedThePINWhenTheSettingsAreLocked(t *testing.T) {
	_, srv, _ := featureServer(t, func(c *config.Config, _ *Deps) { c.Remote.PIN = "4321" })
	for path, body := range map[string]string{"/api/tiles/hidden": `{"tile":"status","hidden":true}`, "/api/apps/remove": `{"id":"netflix"}`} {
		if code, _ := post(t, srv.URL+path, body); code != 401 {
			t.Errorf("%s without the PIN: HTTP %d", path, code)
		}
		if code, said := post(t, srv.URL+path, body, "X-PiDash-PIN", "4321"); code != 200 {
			t.Errorf("%s with the PIN: HTTP %d %s", path, code, said)
		}
	}
}

func TestPlacesOverHTTP(t *testing.T) {
	_, srv, _ := featureServer(t, nil)
	code, body := getBody(t, srv.URL+"/api/photos/places")
	var out struct {
		Places []photos.Place `json:"places"`
		Map    bool           `json:"map"`
	}
	if err := json.Unmarshal([]byte(body), &out); code != 200 || err != nil || len(out.Places) != 2 || out.Map {
		t.Fatalf("places: %d %s", code, body)
	}
	if out.Places[0].Name != "Cascais, Portugal" || out.Places[0].Count != 6 {
		t.Fatalf("first place = %+v", out.Places[0])
	}
	code, body = getBody(t, srv.URL+"/api/photos/playlist?limit=50&place="+strings.ReplaceAll(out.Places[1].ID, " ", "%20"))
	var list struct {
		Items []struct {
			Place string `json:"place"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &list); code != 200 || err != nil || len(list.Items) == 0 {
		t.Fatalf("playlist of a place: %d %s", code, body)
	}
	for _, it := range list.Items {
		if it.Place != "Zermatt, Switzerland" {
			t.Errorf("a photo from %q among those of Zermatt", it.Place)
		}
	}
	if _, body := getBody(t, srv.URL+"/api/photos"); !strings.Contains(body, `"located":12`) {
		t.Errorf("the status does not count the photos with a place: %s", body)
	}
}

func TestVideosOverHTTP(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"Films/Alien.mkv", "Films/Alien.srt", "Holiday 2024.mp4"} {
		p := filepath.Join(root, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	lib := &videos.Library{Source: &videos.Local{Roots: []string{root}}, Dir: t.TempDir(), LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil }}
	_, srv, am := featureServer(t, func(c *config.Config, d *Deps) {
		c.Videos = config.Videos{Source: "folder", Folders: []string{root}}
		d.Videos = lib
	})

	if _, body := getBody(t, srv.URL+"/api/config"); !strings.Contains(body, `"videos":true`) {
		t.Fatalf("the dashboard is not told there are videos: %s", body)
	}
	code, body := getBody(t, srv.URL+"/api/videos?path=")
	var top videos.Listing
	if err := json.Unmarshal([]byte(body), &top); code != 200 || err != nil || len(top.Entries) != 2 || !top.Entries[0].Folder || top.Entries[1].Name != "Holiday 2024.mp4" || top.Player != "mpv" || top.Parent != nil {
		t.Fatalf("the top: %d %s", code, body)
	}
	code, body = getBody(t, srv.URL+"/api/videos?path="+top.Entries[0].Path)
	var films videos.Listing
	if err := json.Unmarshal([]byte(body), &films); code != 200 || err != nil || len(films.Entries) != 1 || films.Parent == nil {
		t.Fatalf("Films: %d %s", code, body)
	}
	if code, _ := getBody(t, srv.URL+"/api/videos?path=0:Gone"); code != 404 {
		t.Fatalf("a folder that is gone: HTTP %d", code)
	}

	// Playing: the player is started the way an app is, with the video and its subtitles.
	play, _ := json.Marshal(map[string]string{"path": films.Entries[0].Path})
	code, body = post(t, srv.URL+"/api/videos/play", string(play))
	if code != 200 || am.Status().Running != videos.AppID {
		t.Fatalf("play: %d %s, running %q", code, body, am.Status().Running)
	}
	argv, err := videos.Pending()
	if err != nil || argv[0] != "/usr/bin/mpv" || argv[len(argv)-1] != filepath.Join(root, "Films", "Alien.mkv") || !strings.Contains(strings.Join(argv, " "), "--sub-file="+filepath.Join(root, "Films", "Alien.srt")) {
		t.Fatalf("what the player is to run: %q %v", argv, err)
	}
	// The player has no tile of its own.
	for _, a := range am.Status().Apps {
		if a.ID == videos.AppID {
			t.Fatal("the video player is listed among the apps")
		}
	}
	// What is not a video in these folders is not played.
	for _, bad := range []string{`{"path":"0:../x.mp4"}`, `{"path":"0:Films/Alien.srt"}`, `{"path":"0:Films"}`, `{"path":""}`, `{}`} {
		if code, _ := post(t, srv.URL+"/api/videos/play", bad); code == 200 {
			t.Errorf("play %s succeeded", bad)
		}
	}
	// A folder on the Pi is not passed on through the address meant for the NAS.
	if code, _ := getBody(t, srv.URL+"/api/videos/stream/abc/def.mkv"); code != 404 {
		t.Errorf("stream without a NAS: HTTP %d", code)
	}
}

func TestWithoutVideosThePagesSaySo(t *testing.T) {
	_, srv, _ := featureServer(t, nil)
	if code, _ := getBody(t, srv.URL+"/api/videos?path="); code != 404 {
		t.Errorf("videos that are not set up: HTTP %d", code)
	}
	if code, _ := post(t, srv.URL+"/api/videos/play", `{"path":"0:a.mp4"}`); code != 404 {
		t.Errorf("playing without videos: HTTP %d", code)
	}
	if _, body := getBody(t, srv.URL+"/api/config"); !strings.Contains(body, `"videos":false`) || !strings.Contains(body, `"games_page":true`) {
		t.Errorf("config: %s", body)
	}
}

// A video on the NAS reaches the player through PiDash: the address is one
// PiDash signed, and only a program on the Pi itself may use it.
func TestAVideoOfTheNASIsPassedOnToThePlayer(t *testing.T) {
	film := []byte(strings.Repeat("0123456789", 500))
	nas := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.Form.Get("api") + "." + r.Form.Get("method") {
		case "SYNO.API.Auth.login":
			_, _ = io.WriteString(w, `{"success":true,"data":{"sid":"s1"}}`)
		case "SYNO.FileStation.List.list":
			_, _ = io.WriteString(w, `{"success":true,"data":{"total":1,"files":[{"name":"Alien.mkv","path":"/video/Alien.mkv","isdir":false,"additional":{"size":5000}}]}}`)
		case "SYNO.FileStation.Download.download":
			http.ServeContent(w, r, "Alien.mkv", time.Time{}, strings.NewReader(string(film)))
		default:
			_, _ = io.WriteString(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	defer nas.Close()
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	var srv *httptest.Server
	src := videos.NewSynology(videos.SynologyOptions{URL: nas.URL, Username: "tv", Password: "pw", Folders: []string{"/video"}, Self: func() string { return srv.URL }})
	lib := &videos.Library{Source: src, Dir: t.TempDir(), LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil }}
	_, srv, _ = featureServer(t, func(c *config.Config, d *Deps) { d.Videos = lib })

	code, body := post(t, srv.URL+"/api/videos/play", `{"path":"/video/Alien.mkv"}`)
	if code != 200 {
		t.Fatalf("play: %d %s", code, body)
	}
	argv, err := videos.Pending()
	if err != nil {
		t.Fatal(err)
	}
	address := argv[len(argv)-1]
	if !strings.HasPrefix(address, srv.URL+"/api/videos/stream/") {
		t.Fatalf("the player is given %q", address)
	}
	req, _ := http.NewRequest(http.MethodGet, address, nil)
	req.Header.Set("Range", "bytes=10-19")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	part, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || string(part) != "0123456789" {
		t.Fatalf("a part of the film: HTTP %d %q", res.StatusCode, part)
	}
	// From another machine the same address is refused.
	far := httptest.NewRequest(http.MethodGet, strings.TrimPrefix(address, srv.URL), nil)
	far.RemoteAddr = "192.168.1.77:40000"
	rec := httptest.NewRecorder()
	srv.Config.Handler.ServeHTTP(rec, far)
	if rec.Code != 404 {
		t.Fatalf("from another machine: HTTP %d", rec.Code)
	}
}

func TestOtherDevicesOnTheTV(t *testing.T) {
	set := &tv.Pretend{}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, srv, _ := featureServer(t, func(c *config.Config, d *Deps) {
		c.TV.Inputs = []config.TVInput{{Name: "PlayStation", Port: 2}, {Name: "Behind the soundbar", Address: "1.2.0.0"}}
		d.TV = tv.New(tv.Options{TV: set, Log: log})
	})
	if _, body := getBody(t, srv.URL+"/api/config"); !strings.Contains(body, `"tv_inputs":["PlayStation","Behind the soundbar"]`) {
		t.Fatalf("the dashboard is not told of the devices: %s", body)
	}
	if code, body := post(t, srv.URL+"/api/tv/input", `{"index":0}`); code != 204 {
		t.Fatalf("show the PlayStation: %d %s", code, body)
	}
	if code, body := post(t, srv.URL+"/api/tv/input", `{"index":1}`); code != 204 {
		t.Fatalf("show what is behind the soundbar: %d %s", code, body)
	}
	if got := set.Did(); got != "show 2.0.0.0\nshow 1.2.0.0" {
		t.Fatalf("the TV was told %q", got)
	}
	for _, bad := range []string{`{"index":2}`, `{"index":-1}`, `{}x`} {
		if code, _ := post(t, srv.URL+"/api/tv/input", bad); code != 400 {
			t.Errorf("input %s: HTTP %d", bad, code)
		}
	}
	// What is connected, for the settings page.
	code, body := post(t, srv.URL+"/api/tv/devices", `{}`)
	if code != 200 || !strings.Contains(body, `"address":"2.0.0.0","name":"PlayStation 5"`) {
		t.Fatalf("devices: %d %s", code, body)
	}

	// A TV that cannot be reached says so.
	_, plain, _ := featureServer(t, func(c *config.Config, _ *Deps) { c.TV.Inputs = []config.TVInput{{Name: "PlayStation", Port: 2}} })
	if code, body := post(t, plain.URL+"/api/tv/input", `{"index":0}`); code != 503 || !strings.Contains(body, "HDMI") {
		t.Errorf("without a TV to tell: %d %s", code, body)
	}
	if code, _ := post(t, plain.URL+"/api/tv/devices", `{}`); code != 503 {
		t.Errorf("devices without a TV to ask: HTTP %d", code)
	}
}

func TestTripsAndTheQuizOverHTTP(t *testing.T) {
	_, srv, _ := featureServer(t, nil)
	code, body := put(t, srv.URL+"/api/photos/selection", `{"folders":[{"id":"Journeys","name":"Journeys","path":"Journeys"}],"recursive":true}`)
	if code != 200 {
		t.Fatalf("choose the folder: %d %s", code, body)
	}
	var trips struct {
		Trips []photos.Trip `json:"trips"`
	}
	for i := 0; ; i++ {
		_, body = getBody(t, srv.URL+"/api/photos/trips")
		if err := json.Unmarshal([]byte(body), &trips); err != nil {
			t.Fatal(err)
		}
		if len(trips.Trips) == 2 {
			break
		}
		if i > 100 {
			t.Fatalf("trips: %s", body)
		}
		time.Sleep(30 * time.Millisecond)
	}
	if trips.Trips[0].Name != "Lisbon, Portugal" || trips.Trips[0].Count != 9 {
		t.Fatalf("the latest trip: %+v", trips.Trips[0])
	}
	_, body = getBody(t, srv.URL+"/api/photos/playlist?limit=50&trip="+trips.Trips[1].ID)
	if n := strings.Count(body, `"place":"Reykjavik, Iceland"`); n != 7 || strings.Contains(body, "Lisbon") {
		t.Fatalf("the photos of the trip to Iceland: %d of them in %s", n, body)
	}
	if _, body := getBody(t, srv.URL+"/api/photos"); !strings.Contains(body, `"trips":2`) || !strings.Contains(body, `"quiz":true`) {
		t.Errorf("status: %s", body)
	}
	var quiz struct {
		Questions []photos.Question `json:"questions"`
	}
	_, body = getBody(t, srv.URL+"/api/photos/quiz?n=5")
	if err := json.Unmarshal([]byte(body), &quiz); err != nil || len(quiz.Questions) != 5 || len(quiz.Questions[0].Answers) != 4 {
		t.Fatalf("quiz: %s", body)
	}
}
