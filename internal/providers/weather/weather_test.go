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

	// quarters is the rain of the quarter-hours from 14:15 on, in mm; nil
	// stands for a service that does not know quarter-hours at all.
	quarters   atomic.Value // []float64
	noQuarters atomic.Bool
	// air is the answer of the air quality service; empty for "not reachable".
	air     atomic.Value // string
	airs    atomic.Int32
	lastAir atomic.Value
	noIndex atomic.Bool
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
		if f.noQuarters.Load() && r.URL.Query().Has("minutely_15") {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":true,"reason":"Cannot initialize SurfacePressureAndHeightVariable from invalid String value minutely_15."}`)
			return
		}
		quarters := map[string]any{}
		if rain, _ := f.quarters.Load().([]float64); rain != nil && r.URL.Query().Has("minutely_15") {
			var qt []string
			var qv []any
			for i, mm := range rain {
				at := time.Date(2026, 11, 13, 14, 15, 0, 0, time.UTC).Add(time.Duration(i) * 15 * time.Minute)
				qt, qv = append(qt, at.Format("2006-01-02T15:04")), append(qv, mm)
			}
			quarters = map[string]any{"time": qt, "precipitation": qv}
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
			"minutely_15": quarters,
			"hourly":      map[string]any{"time": times, "temperature_2m": temps, "precipitation_probability": probs, "precipitation": precs, "weather_code": codes, "is_day": days},
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
	mux.HandleFunc("/air", func(w http.ResponseWriter, r *http.Request) {
		f.airs.Add(1)
		f.lastAir.Store(r.URL.RawQuery)
		answer, _ := f.air.Load().(string)
		if answer == "" {
			http.Error(w, "away", http.StatusBadGateway)
			return
		}
		if f.noIndex.Load() && strings.Contains(r.URL.Query().Get("current"), "european_aqi") {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":true,"reason":"Cannot initialize from invalid String value european_aqi."}`)
			return
		}
		fmt.Fprint(w, answer)
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
		ForecastURL: srv.URL + "/forecast", GeocodeURL: srv.URL + "/search", AirURL: srv.URL + "/air",
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

func TestRainSoon(t *testing.T) {
	fake, opt := newFake(t)
	opt.Location = "Zurich"
	// It is 14:20. The quarters start at 14:15.
	cases := []struct {
		name     string
		quarters []float64
		want     *Rain
	}{
		{"dry all along", []float64{0, 0, 0, 0, 0, 0, 0, 0}, &Rain{}},
		{"starts at 15:00", []float64{0, 0.05, 0, 0.4, 0.9, 0, 0, 0}, &Rain{Starts: "2026-11-13T15:00:00+01:00"}},
		{"stops at 14:45", []float64{0.6, 0.3, 0, 0, 0.5, 0, 0, 0}, &Rain{Now: true, Stops: "2026-11-13T14:45:00+01:00"}},
		{"goes on", []float64{0.6, 0.3, 0.2, 0.2, 0.5, 1, 1, 1}, &Rain{Now: true}},
		{"too little to say", []float64{0, 0}, nil},
		{"nothing", nil, nil},
	}
	for _, c := range cases {
		fake.quarters.Store(c.quarters)
		got := fetch(t, New(opt)).Rain
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("%s: rain = %+v, want %+v", c.name, got, c.want)
		}
	}
	if q := fake.lastForecast.Load().(string); !strings.Contains(q, "minutely_15=precipitation") || !strings.Contains(q, "forecast_minutely_15=8") {
		t.Errorf("the quarter-hours were not asked for: %s", q)
	}

	// A service that knows no quarter-hours still gives the weather, and is
	// not asked for them a second time.
	fake.noQuarters.Store(true)
	p := New(opt)
	before := fake.forecasts.Load()
	if st := fetch(t, p); st.Rain != nil || st.Current.Temperature != 9.4 {
		t.Errorf("without quarter-hours: %+v", st.Rain)
	}
	fetch(t, p)
	if n := fake.forecasts.Load() - before; n != 3 {
		t.Errorf("%d requests, want 3: two for the first round, one after", n)
	}
}

func TestSnowIsCalledSnow(t *testing.T) {
	var api apiForecast
	zero, lots := 0.0, 0.5
	now := time.Date(2026, 1, 5, 9, 5, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		api.Quarters.Time = append(api.Quarters.Time, now.Truncate(15*time.Minute).Add(time.Duration(i)*15*time.Minute).Format(localLayout))
		v := &zero
		if i >= 5 {
			v = &lots
		}
		api.Quarters.Precipitation = append(api.Quarters.Precipitation, v)
	}
	hours := []Hour{{Time: "2026-01-05T09:00:00Z", Code: 3}, {Time: "2026-01-05T10:00:00Z", Code: 73}}
	rain := rainSoon(&api, hours, time.UTC, now, false)
	if rain == nil || rain.Now || rain.Starts != "2026-01-05T10:15:00Z" || !rain.Snow {
		t.Errorf("rain = %+v, want snow from 10:15", rain)
	}
}

func TestAirAndPollen(t *testing.T) {
	fake, opt := newFake(t)
	opt.Location = "Zurich"
	clock := time.Date(2026, 11, 13, 14, 20, 0, 0, time.UTC)
	opt.Now = func() time.Time { return clock }

	// The air service is away: the weather is there all the same.
	p := New(opt)
	if st := fetch(t, p); st.Air != nil || st.Current.Temperature != 9.4 {
		t.Fatalf("air = %+v", st.Air)
	}

	fake.air.Store(`{"current":{"european_aqi":47,"us_aqi":61,"pm10":18.2,"pm2_5":11.6,
		"alder_pollen":0.2,"birch_pollen":120,"grass_pollen":7.5,"mugwort_pollen":null,"olive_pollen":null,"ragweed_pollen":3}}`)
	clock = clock.Add(31 * time.Minute)
	air := fetch(t, p).Air
	if air == nil || air.Scale != "eu" || air.Index != 47 || air.Level != 2 || air.PM25 != 11.6 || air.PM10 != 18.2 {
		t.Fatalf("air = %+v", air)
	}
	want := []Pollen{{"birch", 3}, {"grass", 2}, {"ragweed", 1}}
	if fmt.Sprint(air.Pollen) != fmt.Sprint(want) {
		t.Errorf("pollen = %v, want %v", air.Pollen, want)
	}

	// Asked for no more than every half hour, and kept through a short outage.
	asked := fake.airs.Load()
	clock = clock.Add(10 * time.Minute)
	fetch(t, p)
	if fake.airs.Load() != asked {
		t.Error("the air was asked for again after ten minutes")
	}
	fake.air.Store("")
	clock = clock.Add(time.Hour)
	if fetch(t, p).Air == nil {
		t.Error("an hour without the service should keep the last air quality")
	}
	clock = clock.Add(3 * time.Hour)
	if fetch(t, p).Air != nil {
		t.Error("after hours without the service the old value should go")
	}

	// Miles and Fahrenheit: the US index.
	fake.air.Store(`{"current":{"european_aqi":47,"us_aqi":161,"pm10":18.2,"pm2_5":11.6}}`)
	opt.Units = Imperial
	if air := fetch(t, New(opt)).Air; air == nil || air.Scale != "us" || air.Index != 161 || air.Level != 3 || len(air.Pollen) != 0 {
		t.Errorf("US air = %+v", air)
	}

	// A service without the ready-made index: worked out from the dust.
	opt.Units = Metric
	fake.noIndex.Store(true)
	fake.air.Store(`{"current":{"pm10":18.2,"pm2_5":30}}`)
	p = New(opt)
	air = fetch(t, p).Air
	if air == nil || air.Scale != "eu" || air.Level != 3 || air.Index != 64 {
		t.Errorf("air from dust = %+v, want level 3 (poor), index 64", air)
	}
	if q := fake.lastAir.Load().(string); strings.Contains(q, "european_aqi") {
		t.Errorf("the second request should leave the index out: %s", q)
	}
	if st := fetch(t, New(Options{ForecastURL: opt.ForecastURL, GeocodeURL: opt.GeocodeURL, AirURL: opt.AirURL, Location: "Zurich", Now: opt.Now})); st.Air == nil {
		t.Error("a new provider should find its way again")
	}

	// Nothing known at all.
	fake.noIndex.Store(false)
	fake.air.Store(`{"current":{"european_aqi":null,"pm10":null}}`)
	if air := fetch(t, New(opt)).Air; air != nil {
		t.Errorf("air = %+v, want none", air)
	}
}
