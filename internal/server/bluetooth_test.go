package server

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/DarioRomano/PiDash/internal/apps"
	"github.com/DarioRomano/PiDash/internal/bluetooth"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/tv"
)

func bluetoothServer(t *testing.T, pin string, blue bluetooth.Manager) *httptest.Server {
	t.Helper()
	cfg := config.Default()
	cfg.Remote.PIN = pin
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	s := New(cfg, log, static, Deps{
		Kiosk: kiosk.New("", true, log), Apps: apps.NewManager(nil, &apps.Mock{}, log),
		Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}), Bluetooth: blue,
		NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
	})
	srv := httptest.NewServer(s.mux)
	t.Cleanup(srv.Close)
	return srv
}

func bluetoothNames(t *testing.T, body string) string {
	t.Helper()
	var st bluetooth.Status
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	var out []string
	for _, d := range st.Devices {
		s := d.Name
		if d.Paired {
			s += "*"
		}
		if d.Connected {
			s += "+"
		}
		out = append(out, s)
	}
	return strings.Join(out, ", ")
}

func TestBluetoothOverHTTP(t *testing.T) {
	srv := bluetoothServer(t, "", bluetooth.NewMock())
	get := func() string {
		res, err := http.Get(srv.URL + "/api/bluetooth")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	if got := bluetoothNames(t, get()); got != "Living room speaker*, Xbox Wireless Controller*+" {
		t.Fatalf("devices = %s", got)
	}

	// Searching shows what is nearby; pairing makes it the Pi's own.
	if code, body := post(t, srv.URL+"/api/bluetooth/search", `{"on":true}`); code != 200 || bluetoothNames(t, body) != "Living room speaker*, Xbox Wireless Controller*+, 8BitDo Pro 2, Keyboard K380" {
		t.Fatalf("search: %d %s", code, body)
	}
	if code, body := post(t, srv.URL+"/api/bluetooth/pair", `{"address":"AA:00:00:00:00:03"}`); code != 200 || !strings.Contains(bluetoothNames(t, body), "8BitDo Pro 2*+") {
		t.Fatalf("pair: %d %s", code, body)
	}
	if code, body := post(t, srv.URL+"/api/bluetooth/disconnect", `{"address":"AA:00:00:00:00:01"}`); code != 200 || !strings.Contains(bluetoothNames(t, body), "Xbox Wireless Controller*,") {
		t.Fatalf("disconnect: %d %s", code, body)
	}
	if code, body := post(t, srv.URL+"/api/bluetooth/forget", `{"address":"AA:00:00:00:00:02"}`); code != 200 || strings.Contains(bluetoothNames(t, body), "speaker*") {
		t.Fatalf("forget: %d %s", code, body)
	}

	// What cannot be done is answered with a sentence, not a crash.
	if code, body := post(t, srv.URL+"/api/bluetooth/connect", `{"address":"00:00:00:00:00:00"}`); code != 404 || !strings.Contains(body, "not known") {
		t.Errorf("unknown device: %d %s", code, body)
	}
	if code, _ := post(t, srv.URL+"/api/bluetooth/explode", `{}`); code != 404 {
		t.Errorf("unknown action: %d", code)
	}
	if code, _ := post(t, srv.URL+"/api/bluetooth/pair", `not json`); code != 400 {
		t.Errorf("bad request: %d", code)
	}
}

func TestBluetoothChangesNeedTheSettingsPIN(t *testing.T) {
	srv := bluetoothServer(t, "4711", bluetooth.NewMock())
	if code, _ := post(t, srv.URL+"/api/bluetooth/forget", `{"address":"AA:00:00:00:00:01"}`); code != 401 {
		t.Errorf("forgetting a device without the PIN: %d, want 401", code)
	}
	if code, _ := post(t, srv.URL+"/api/bluetooth/forget", `{"address":"AA:00:00:00:00:01"}`, pinHeader, "4711"); code != 200 {
		t.Errorf("with the PIN: %d, want 200", code)
	}
	// Looking is allowed.
	if res, err := http.Get(srv.URL + "/api/bluetooth"); err != nil || res.StatusCode != 200 {
		t.Errorf("listing devices: %v %v", res, err)
	}
}

func TestWithoutBluetoothThePageIsToldSo(t *testing.T) {
	srv := bluetoothServer(t, "", nil)
	res, err := http.Get(srv.URL + "/api/bluetooth")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var st bluetooth.Status
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil || st.Available || st.Problem == "" || st.Devices == nil {
		t.Errorf("status = %+v, err %v", st, err)
	}
	if code, _ := post(t, srv.URL+"/api/bluetooth/search", `{"on":true}`); code != 404 {
		t.Errorf("searching without Bluetooth: %d", code)
	}
}

func TestALowBatteryIsSaidOnce(t *testing.T) {
	blue := bluetooth.NewMock()
	cfg := config.Default()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	var static fs.FS = fstest.MapFS{"index.html": {Data: []byte("<html>dash</html>")}}
	s := New(cfg, log, static, Deps{
		Kiosk: kiosk.New("", true, log), Apps: apps.NewManager(nil, &apps.Mock{}, log),
		Input: input.NewService(false, log), TV: tv.New(tv.Options{TV: &fakeTV{}, Log: log}), Bluetooth: blue,
		NewRunner: func(publish func(providers.Entry)) *providers.Runner { return providers.NewRunner(log, publish) },
	})
	events := s.hub.subscribe()
	defer s.hub.unsubscribe(events)
	notices := func() []string {
		var out []string
		for {
			select {
			case m := <-events:
				if m.name == "battery" {
					out = append(out, string(m.data))
				}
			default:
				return out
			}
		}
	}
	const pad = "AA:00:00:00:00:01"
	told := map[string]bool{}
	ctx := context.Background()

	s.checkBatteries(ctx, told) // 80 %: nothing to say
	if got := notices(); len(got) != 0 {
		t.Fatalf("a full controller was reported: %v", got)
	}
	blue.SetBattery(pad, 12)
	s.checkBatteries(ctx, told)
	s.checkBatteries(ctx, told)
	blue.SetBattery(pad, 9)
	s.checkBatteries(ctx, told)
	if got := notices(); len(got) != 1 || got[0] != `{"name":"Xbox Wireless Controller","percent":12}` {
		t.Fatalf("notices = %v, want one for 12 %%", got)
	}
	// A little charge is not "charged": no new notice at 20 %, then low again.
	blue.SetBattery(pad, 20)
	s.checkBatteries(ctx, told)
	blue.SetBattery(pad, 14)
	s.checkBatteries(ctx, told)
	if got := notices(); len(got) != 0 {
		t.Errorf("said again without having been charged: %v", got)
	}
	// Charged and run down again: said again.
	blue.SetBattery(pad, 90)
	s.checkBatteries(ctx, told)
	blue.SetBattery(pad, 10)
	s.checkBatteries(ctx, told)
	if got := notices(); len(got) != 1 {
		t.Errorf("after charging and running low again: %v, want one notice", got)
	}
	// Switched off while low, and on again: said again.
	blue.Disconnect(ctx, pad)
	s.checkBatteries(ctx, told)
	blue.Connect(ctx, pad)
	s.checkBatteries(ctx, told)
	if got := notices(); len(got) != 1 {
		t.Errorf("after switching it off and on while low: %v, want one notice", got)
	}
}
