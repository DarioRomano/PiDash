package hass

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeHA imitates the two parts of Home Assistant's API that PiDash uses.
type fakeHA struct {
	states map[string]string
	calls  []string
}

func (f *fakeHA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer good-token" {
		http.Error(w, "401: Unauthorized", http.StatusUnauthorized)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/states":
		attrs := map[string]map[string]any{
			"light.sofa":          {"friendly_name": "Sofa lamp"},
			"sensor.living_temp":  {"friendly_name": "Living room", "unit_of_measurement": "°C"},
			"scene.movie":         {"friendly_name": "Movie night"},
			"cover.blind":         {"friendly_name": "Blind"},
			"lock.front_door":     {"friendly_name": "Front door"},
			"switch.dead_plug":    {"friendly_name": "Old plug"},
			"media_player.stereo": {},
		}
		var out []map[string]any
		for _, id := range []string{"sensor.living_temp", "light.sofa", "scene.movie", "cover.blind", "lock.front_door", "switch.dead_plug", "media_player.stereo"} {
			out = append(out, map[string]any{"entity_id": id, "state": f.states[id], "attributes": attrs[id]})
		}
		json.NewEncoder(w).Encode(out)
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/services/"):
		body, _ := io.ReadAll(r.Body)
		var req struct {
			EntityID string `json:"entity_id"`
		}
		json.Unmarshal(body, &req)
		f.calls = append(f.calls, strings.TrimPrefix(r.URL.Path, "/api/services/")+" "+req.EntityID)
		if strings.HasSuffix(r.URL.Path, "/toggle") && req.EntityID == "light.sofa" {
			if f.states[req.EntityID] == "on" {
				f.states[req.EntityID] = "off"
			} else {
				f.states[req.EntityID] = "on"
			}
		}
		w.Write([]byte("[]"))
	default:
		http.NotFound(w, r)
	}
}

func newFake(t *testing.T) (*fakeHA, *httptest.Server) {
	f := &fakeHA{states: map[string]string{
		"light.sofa": "off", "sensor.living_temp": "21.5", "scene.movie": "2026-01-01T20:00:00+00:00",
		"cover.blind": "open", "lock.front_door": "locked", "switch.dead_plug": "unavailable", "media_player.stereo": "playing",
	}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func TestTilesShowAndSwitch(t *testing.T) {
	fake, srv := newFake(t)
	c := New(srv.URL+"/", " good-token ")
	ctx := context.Background()

	tiles, err := c.Get(ctx, []string{"light.sofa", "sensor.living_temp", "scene.movie", "cover.blind", "switch.dead_plug", "light.gone"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Entity{
		{ID: "light.sofa", Name: "Sofa lamp", Domain: "light", Text: "Off", Actionable: true, Available: true},
		{ID: "sensor.living_temp", Name: "Living room", Domain: "sensor", Text: "21.5 °C", Available: true},
		{ID: "scene.movie", Name: "Movie night", Domain: "scene", Actionable: true, Available: true},
		{ID: "cover.blind", Name: "Blind", Domain: "cover", Text: "Open", On: true, Actionable: true, Available: true},
		{ID: "switch.dead_plug", Name: "Old plug", Domain: "switch", Text: "Not available"},
		{ID: "light.gone", Name: "light.gone", Domain: "light", Text: "Not available"},
	}
	for i, w := range want {
		if tiles[i] != w {
			t.Errorf("tile %d:\n got  %+v\n want %+v", i, tiles[i], w)
		}
	}

	if err := c.Activate(ctx, "light.sofa"); err != nil {
		t.Fatal(err)
	}
	if tiles, _ = c.Get(ctx, []string{"light.sofa"}); !tiles[0].On || tiles[0].Text != "On" {
		t.Errorf("after switching: %+v", tiles[0])
	}
	c.Activate(ctx, "scene.movie")
	c.Activate(ctx, "cover.blind")
	if got, want := strings.Join(fake.calls, "; "), "homeassistant/toggle light.sofa; scene/turn_on scene.movie; cover/toggle cover.blind"; got != want {
		t.Errorf("calls = %s\nwant    %s", got, want)
	}
	// A value cannot be switched, and a lock is never touched.
	for _, id := range []string{"sensor.living_temp", "lock.front_door"} {
		if err := c.Activate(ctx, id); err != ErrNoAction {
			t.Errorf("Activate(%s) = %v, want ErrNoAction", id, err)
		}
	}
	if len(fake.calls) != 3 {
		t.Errorf("Home Assistant was called for something that must not be switched: %v", fake.calls)
	}
}

func TestListForTheSettingsPage(t *testing.T) {
	_, srv := newFake(t)
	all, err := New(srv.URL, "good-token").All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range all {
		names = append(names, e.Name)
	}
	// Sorted by name; the lock is not offered.
	if got, want := strings.Join(names, ", "), "Blind, Living room, media_player.stereo, Movie night, Old plug, Sofa lamp"; got != want {
		t.Errorf("offered: %s\nwant:    %s", got, want)
	}
}

func TestProblemsAreExplained(t *testing.T) {
	_, srv := newFake(t)
	ctx := context.Background()
	if _, err := New(srv.URL, "wrong").All(ctx); err == nil || !strings.Contains(err.Error(), "did not accept the access token") {
		t.Errorf("wrong token: %v", err)
	}
	if _, err := New("http://127.0.0.1:1", "x").All(ctx); err == nil || !strings.Contains(err.Error(), "cannot be reached") {
		t.Errorf("unreachable: %v", err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("<html>router</html>")) }))
	defer other.Close()
	if _, err := New(other.URL, "x").All(ctx); err == nil || !strings.Contains(err.Error(), "does not answer like Home Assistant") {
		t.Errorf("not Home Assistant: %v", err)
	}
}
