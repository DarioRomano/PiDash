package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOpenMeteo serves a geocoding and a forecast endpoint shaped like the real ones.
type fakeOpenMeteo struct {
	geocodes, forecasts atomic.Int32
	lastForecast        atomic.Value // url.Values as string
	down                atomic.Bool
}

func (f *fakeOpenMeteo) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		f.geocodes.Add(1)
		switch r.URL.Query().Get("name") {
		case "Zurich":
			fmt.Fprint(w, `{"results":[{"name":"Zurich","latitude":47.3667,"longitude":8.55,"country":"Switzerland","country_code":"CH","admin1":"Zurich"}]}`)
		case "Portland":
			fmt.Fprint(w, `{"results":[
				{"name":"Portland","latitude":45.52,"longitude":-122.68,"country":"United States","country_code":"US","admin1":"Oregon"},
				{"name":"Portland","latitude":43.66,"longitude":-70.26,"country":"United States","country_code":"US","admin1":"Maine"}]}`)
		default:
			fmt.Fprint(w, `{"generationtime_ms":0.5}`)
		}
	})
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		f.forecasts.Add(1)
		f.lastForecast.Store(r.URL.RawQuery)
		if f.down.Load() {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":true,"reason":"Latitude must be in range of -90 to 90°."}`)
			return
		}
		// Two days of hourly data starting at local midnight, 2026-11-13.
		var times []string
		var temps, probs, precs, codes, days []any
		for d := 13; d <= 14; d++ {
			for h := 0; h < 24; h++ {
				times = append(times, fmt.Sprintf("2026-11-%02dT%02d:00", d, h))
				temps = append(temps, float64(h))
				probs = append(probs, h*2)
				precs = append(precs, 0.1)
				codes = append(codes, 61)
				days = append(days, map[bool]int{true: 1, false: 0}[h >= 7 && h < 17])
			}
		}
		probs[len(probs)-1] = nil // the service leaves gaps at the end of its range
		json.NewEncoder(w).Encode(map[string]any{
			"timezone": "Europe/Zurich", "utc_offset_seconds": 3600,
			"current": map[string]any{
				"time": "2026-11-13T14:15", "temperature_2m": 9.4, "apparent_temperature": 7.1,
				"relative_humidity_2m": 71.6, "is_day": 1, "precipitation": 0.2, "weather_code": 3,
				"wind_speed_10m": 14.2, "wind_direction_10m": 249.7, "wind_gusts_10m": 31.0,
			},
			"hourly": map[string]any{"time": times, "temperature_2m": temps, "precipitation_probability": probs, "precipitation": precs, "weather_code": codes, "is_day": days},
			"daily": map[string]any{
				"time":         []string{"2026-11-13", "2026-11-14", "2026-11-15"},
				"weather_code": []any{3, 61, nil}, "temperature_2m_max": []any{12.3, 10.0, nil}, "temperature_2m_min": []any{4.5, 3.0, nil},
				"precipitation_probability_max": []any{20, 85, nil}, "precipitation_sum": []any{0.0, 6.4, nil},
				"sunrise":      []string{"2026-11-13T07:24", "2026-11-14T07:26", "2026-11-15T07:27"},
				"sunset":       []string{"2026-11-13T16:52", "2026-11-14T16:51", "2026-11-15T16:50"},
				"uv_index_max": []any{1.8, 1.2, nil}, "wind_speed_10m_max": []any{22.0, 30.5, nil},
			},
		})
	})
	return mux
}

func newFake(t *testing.T) (*fakeOpenMeteo, Options) {
	t.Helper()
	fake := &fakeOpenMeteo{}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	zurich, _ := time.LoadLocation("Europe/Zurich")
	return fake, Options{
		ForecastURL: srv.URL + "/forecast", GeocodeURL: srv.URL + "/search",
		StatePath: filepath.Join(t.TempDir(), "state", "weather-location.json"),
		Now:       func() time.Time { return time.Date(2026, 11, 13, 14, 20, 0, 0, zurich) },
	}
}

func fetch(t *testing.T, p *Provider) State {
	t.Helper()
	v, err := p.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return v.(State)
}

func TestForecastByPlaceName(t *testing.T) {
	fake, opt := newFake(t)
	opt.Location = "Zurich"
	p := New(opt)
	st := fetch(t, p)

	if st.Location != "Zurich" || st.Timezone != "Europe/Zurich" || st.Units.Temperature != "°C" || st.Units.Wind != "km/h" {
		t.Errorf("header = %+v %+v", st.Location, st.Units)
	}
	c := st.Current
	if c.Temperature != 9.4 || c.FeelsLike != 7.1 || c.Humidity != 72 || c.WindDirection != 250 || c.Code != 3 || !c.IsDay {
		t.Errorf("current = %+v", c)
	}
	if c.Time != "2026-11-13T14:15:00+01:00" {
		t.Errorf("current time = %s, want the place's local time with its UTC offset", c.Time)
	}

	// Hourly starts at the current hour (14:00), not at midnight, and runs 24 hours.
	if len(st.Hourly) != 24 || st.Hourly[0].Time != "2026-11-13T14:00:00+01:00" || st.Hourly[23].Time != "2026-11-14T13:00:00+01:00" {
		t.Fatalf("hourly: %d entries, first %s, last %s", len(st.Hourly), st.Hourly[0].Time, st.Hourly[len(st.Hourly)-1].Time)
	}
	if h := st.Hourly[0]; h.Temperature != 14 || h.PrecipitationProbability != 28 || !h.IsDay || h.Code != 61 {
		t.Errorf("first hour = %+v", h)
	}
	if st.Hourly[3].IsDay { // 17:00, after sunset in the fake data
		t.Errorf("17:00 should be night: %+v", st.Hourly[3])
	}

	// The third day has no data yet and is left out.
	if len(st.Daily) != 2 {
		t.Fatalf("daily has %d entries, want 2", len(st.Daily))
	}
	if d := st.Daily[1]; d.Date != "2026-11-14" || d.Max != 10 || d.Min != 3 || d.PrecipitationProbability != 85 || d.PrecipitationSum != 6.4 ||
		d.Sunrise != "2026-11-14T07:26:00+01:00" || d.Sunset != "2026-11-14T16:51:00+01:00" || d.WindSpeedMax != 30.5 {
		t.Errorf("day = %+v", d)
	}

	// The place is looked up once and remembered, also across restarts.
	fetch(t, p)
	fetch(t, New(opt))
	if n := fake.geocodes.Load(); n != 1 {
		t.Errorf("place looked up %d times, want once", n)
	}
	q := fake.lastForecast.Load().(string)
	for _, want := range []string{"latitude=47.3667", "longitude=8.5500", "timezone=auto", "temperature_unit=celsius", "forecast_days=7"} {
		if !strings.Contains(q, want) {
			t.Errorf("forecast request is missing %s: %s", want, q)
		}
	}
}

func TestChangingThePlaceLooksItUpAgain(t *testing.T) {
	fake, opt := newFake(t)
	opt.Location = "Zurich"
	fetch(t, New(opt))
	opt.Location = "Portland, Maine"
	fetch(t, New(opt))
	if n := fake.geocodes.Load(); n != 2 {
		t.Errorf("lookups = %d, want 2", n)
	}
	if q := fake.lastForecast.Load().(string); !strings.Contains(q, "latitude=43.66") {
		t.Errorf("\"Portland, Maine\" should pick the one in Maine: %s", q)
	}
	opt.Location = "Portland, OR"
	opt.StatePath = ""
	fetch(t, New(opt))
	if q := fake.lastForecast.Load().(string); !strings.Contains(q, "latitude=45.52") {
		t.Errorf("an unmatched hint should fall back to the first result: %s", q)
	}
}

func TestCoordinatesNameAndImperialUnits(t *testing.T) {
	fake, opt := newFake(t)
	lat, lon := -33.87, 151.21
	opt.Latitude, opt.Longitude, opt.Name, opt.Units = &lat, &lon, "Home", Imperial
	st := fetch(t, New(opt))
	if st.Location != "Home" || st.Units.Temperature != "°F" || st.Units.Wind != "mph" || st.Units.Precipitation != "in" {
		t.Errorf("state = %q %+v", st.Location, st.Units)
	}
	if fake.geocodes.Load() != 0 {
		t.Error("coordinates were given; no lookup should happen")
	}
	q := fake.lastForecast.Load().(string)
	for _, want := range []string{"latitude=-33.8700", "longitude=151.2100", "temperature_unit=fahrenheit", "wind_speed_unit=mph", "precipitation_unit=inch"} {
		if !strings.Contains(q, want) {
			t.Errorf("request is missing %s: %s", want, q)
		}
	}
}

func TestProblemsAreExplained(t *testing.T) {
	fake, opt := newFake(t)

	opt.Location = "Atlantis"
	_, err := New(opt).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), `no place called "Atlantis"`) {
		t.Errorf("unknown place: %v", err)
	}

	opt.Location = "Zurich"
	fake.down.Store(true)
	_, err = New(opt).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Latitude must be in range") {
		t.Errorf("service error: %v", err)
	}

	opt.ForecastURL = "http://127.0.0.1:1/forecast"
	_, err = New(opt).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cannot reach the weather service") || strings.Contains(err.Error(), "latitude=") {
		t.Errorf("offline: %v", err)
	}
}

func TestMockIsComplete(t *testing.T) {
	v, err := Mock{}.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st := v.(State)
	if len(st.Hourly) != 24 || len(st.Daily) != 7 || st.Location == "" {
		t.Errorf("mock: %d hours, %d days", len(st.Hourly), len(st.Daily))
	}
}
