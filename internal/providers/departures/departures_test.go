package departures

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A pretend timetable service, answering the way transport.opendata.ch does.
func service(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("User-Agent") != "PiDash/test" {
			t.Errorf("asked as %q", r.Header.Get("User-Agent"))
		}
		switch r.URL.Path {
		case "/v1/stationboard":
			if r.URL.Query().Get("station") == "Nirgendwo" {
				fmt.Fprint(w, `{"station":null,"stationboard":[]}`)
				return
			}
			if r.URL.Query().Get("station") == "Too often" {
				http.Error(w, "slow down", http.StatusTooManyRequests)
				return
			}
			fmt.Fprint(w, `{"station":{"id":"8576193","name":"Zürich, Bellevue"},"stationboard":[
 {"stop":{"departure":"2026-11-16T12:21:00+0100","departureTimestamp":1763292060,"delay":0,"platform":"B","prognosis":{"platform":null}},"category":"T","number":"4","to":"Zürich, Bahnhof Altstetten Nord"},
 {"stop":{"departure":"2026-11-16T12:23:00+0100","departureTimestamp":1763292180,"delay":3,"platform":"C","prognosis":{"platform":"D"}},"category":"T","number":"11","to":"Zürich, Rehalp"},
 {"stop":{"departure":"2026-11-16T12:24:00+0100","departureTimestamp":null,"delay":null,"platform":null,"prognosis":{"platform":null}},"category":"B","number":"912","to":"Zürich, Bahnhof Tiefenbrunnen"},
 {"stop":{"departure":"2026-11-16T12:30:00+0100","departureTimestamp":1763292600,"delay":null,"platform":"1","prognosis":{"platform":null}},"category":"S","number":"12","to":"Brugg AG"},
 {"stop":{"departure":"2026-11-16T12:32:00+0100","departureTimestamp":1763292720,"delay":null,"platform":"7","prognosis":{"platform":null}},"category":"IC","number":"8","to":"Romanshorn"},
 {"stop":{"departure":"2026-11-16T12:40:00+0100","departureTimestamp":1763293200,"delay":null,"platform":"","prognosis":{"platform":null}},"category":"BAT","number":"3732","to":"Rapperswil SG (See)"},
 {"stop":{"departure":null,"departureTimestamp":null,"delay":null,"platform":"","prognosis":{"platform":null}},"category":"T","number":"2","to":"an arrival only"},
 {"stop":{"departure":"2026-11-16T12:45:00+0100","departureTimestamp":1763293500,"delay":null,"platform":"","prognosis":{"platform":null}},"category":"T","number":"5","to":""}]}`)
		case "/v1/locations":
			switch r.URL.Query().Get("query") {
			case "Bellevue":
				fmt.Fprint(w, `{"stations":[{"id":null,"name":"Bellevue, a restaurant"},{"id":"8576193","name":"Zürich, Bellevue"},{"id":"8501120","name":"Lausanne, Bellevue"}]}`)
			default:
				fmt.Fprint(w, `{"stations":[]}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

func TestTheNextDeparturesFromAStop(t *testing.T) {
	srv, asked := service(t)
	p := New(Options{Stop: " Zürich, Bellevue ", Walk: 4, URL: srv.URL + "/v1/", Agent: "PiDash/test"})
	if p.Name() != "departures" || p.Interval() != 90*time.Second {
		t.Errorf("name %q, every %v", p.Name(), p.Interval())
	}
	got, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st := got.(State)
	if st.Stop != "Zürich, Bellevue" || st.Walk != 4 || len(st.Departures) != 6 {
		t.Fatalf("state = %+v", st)
	}
	want := []struct {
		line, kind, to, platform string
		delay                    int
		at                       string
	}{
		{"4", "tram", "Zürich, Bahnhof Altstetten Nord", "B", 0, "11:21"},
		{"11", "tram", "Zürich, Rehalp", "D", 3, "11:23"}, // the platform it was moved to
		{"912", "bus", "Zürich, Bahnhof Tiefenbrunnen", "", 0, "11:24"},
		{"S12", "train", "Brugg AG", "1", 0, "11:30"},
		{"IC 8", "train", "Romanshorn", "7", 0, "11:32"},
		{"3732", "boat", "Rapperswil SG (See)", "", 0, "11:40"},
	}
	for i, w := range want {
		d := st.Departures[i]
		if d.Line != w.line || d.Kind != w.kind || d.To != w.to || d.Platform != w.platform || d.Delay != w.delay || d.At.Format("15:04") != w.at {
			t.Errorf("departure %d = %+v, want %+v", i, d, w)
		}
	}
	// Asked by name, and only for what is shown.
	q := (*asked)[0]
	if !strings.Contains(q, "station=Z%C3%BCrich%2C+Bellevue") || !strings.Contains(q, "limit=30") || !strings.Contains(q, "fields%5B%5D=stationboard%2Fto") {
		t.Errorf("asked %s", q)
	}

	// A stop given by its number is asked for by that.
	*asked = nil
	if _, err := New(Options{Stop: "8576193", URL: srv.URL + "/v1", Agent: "PiDash/test"}).Fetch(context.Background()); err != nil || !strings.Contains((*asked)[0], "id=8576193") {
		t.Errorf("by number: %v, %v", err, *asked)
	}
	// What goes wrong is said in words.
	for stop, want := range map[string]string{"Nirgendwo": "knows no stop", "Too often": "asked too often", "": "no stop is set"} {
		if _, err := New(Options{Stop: stop, URL: srv.URL + "/v1", Agent: "PiDash/test"}).Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", stop, err)
		}
	}
	srv.Close()
	if _, err := p.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "could not be reached") {
		t.Errorf("with the service away: %v", err)
	}
}

func TestFindingAStopByName(t *testing.T) {
	srv, _ := service(t)
	p := New(Options{URL: srv.URL + "/v1", Agent: "PiDash/test"})
	// The first real stop: what comes without a number is an address or a sight.
	if name, err := p.Lookup(context.Background(), " Bellevue "); err != nil || name != "Zürich, Bellevue" {
		t.Errorf("found %q, %v", name, err)
	}
	if _, err := p.Lookup(context.Background(), "Times Square"); err == nil || !strings.Contains(err.Error(), "covers Switzerland") {
		t.Errorf("a stop elsewhere: %v", err)
	}
	if _, err := p.Lookup(context.Background(), "  "); err == nil {
		t.Error("an empty name was looked up")
	}
}

func TestWhatIsWrittenOnTheVehicle(t *testing.T) {
	for _, c := range []struct{ category, number, line, kind string }{
		{"T", "4", "4", "tram"}, {"B", "31", "31", "bus"}, {"NFB", "N4", "N4", "bus"}, {"S", "12", "S12", "train"}, {"S", "S12", "S12", "train"},
		{"IC", "8", "IC 8", "train"}, {"IR", "36", "IR 36", "train"}, {"RE", "", "RE", "train"}, {"BAT", "", "BAT", "boat"}, {"FUN", "", "FUN", "cable"},
		{"ic", " 1 ", "IC 1", "train"}, {"R", "11", "R11", "train"},
	} {
		if line, kind := lineOf(c.category, c.number); line != c.line || kind != c.kind {
			t.Errorf("%s %s: %q %q, want %q %q", c.category, c.number, line, kind, c.line, c.kind)
		}
	}
}
