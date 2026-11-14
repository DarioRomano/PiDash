// Package weather fetches current conditions and the forecast from Open-Meteo
// (https://open-meteo.com), which needs no account or API key.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Units systems.
const (
	Metric   = "metric"
	Imperial = "imperial"
)

// Options configure the provider.
type Options struct {
	// Location is a place name such as "Zurich" or "Portland, Oregon". It is
	// looked up once and remembered. Ignored when coordinates are given.
	Location string
	// Latitude and Longitude pin the exact spot; both or neither.
	Latitude, Longitude *float64
	// Name overrides the label shown on screen.
	Name     string
	Units    string
	Interval time.Duration
	// StatePath remembers the looked-up place between restarts.
	StatePath string

	// ForecastURL, GeocodeURL and AirURL override the Open-Meteo endpoints (tests).
	ForecastURL, GeocodeURL, AirURL string
	HTTP                            *http.Client
	// Now overrides the clock (tests).
	Now func() time.Time
}

// Provider implements providers.Provider for weather.
type Provider struct {
	opt Options

	mu    sync.Mutex
	place *place

	// What the service turned out not to offer, so it is not asked every time.
	noQuarters bool
	noIndex    bool
	// The air changes slowly and comes from a second service: asked for less
	// often than the weather, and kept for a while when that service is away.
	air   *Air
	airAt time.Time
}

// place is a resolved location.
type place struct {
	Query     string  `json:"query"`
	Name      string  `json:"name"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	// Label names the place with its region and country, to tell apart places
	// that share a name.
	Label string `json:"label,omitempty"`
}

// Lookup resolves a place name and returns how the weather service knows it,
// for example "Portland, Maine, United States". The settings page uses it to
// confirm a place before saving.
func Lookup(ctx context.Context, location string) (string, error) {
	pl, err := New(Options{}).geocode(ctx, location)
	if err != nil {
		return "", err
	}
	return pl.Label, nil
}

// New returns a weather provider.
func New(opt Options) *Provider {
	if opt.ForecastURL == "" {
		opt.ForecastURL = "https://api.open-meteo.com/v1/forecast"
	}
	if opt.AirURL == "" {
		opt.AirURL = "https://air-quality-api.open-meteo.com/v1/air-quality"
	}
	if opt.GeocodeURL == "" {
		opt.GeocodeURL = "https://geocoding-api.open-meteo.com/v1/search"
	}
	if opt.HTTP == nil {
		opt.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if opt.Interval <= 0 {
		opt.Interval = 15 * time.Minute
	}
	if opt.Units == "" {
		opt.Units = Metric
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Provider{opt: opt}
}

// Name implements providers.Provider.
func (p *Provider) Name() string { return "weather" }

// Interval implements providers.Provider.
func (p *Provider) Interval() time.Duration { return p.opt.Interval }

// State is the JSON sent to the dashboard. Weather codes are WMO codes; the
// dashboard turns them into icons and words.
type State struct {
	Location string `json:"location"`
	// Timezone is the IANA zone of the location, so times are shown in the
	// place's own local time.
	Timezone string  `json:"timezone"`
	Units    Units   `json:"units"`
	Current  Current `json:"current"`
	// Hourly starts at the current hour and covers the next 24 hours.
	Hourly []Hour `json:"hourly"`
	// Daily starts today.
	Daily []Day `json:"daily"`
	// Rain says whether it rains in the next two hours, and from when to when.
	// Nil when the service has nothing that fine for the place.
	Rain *Rain `json:"rain,omitempty"`
	// Air is the air quality and the pollen. Nil when it is not known.
	Air *Air `json:"air,omitempty"`
}

// Rain is the next two hours in quarters of an hour.
type Rain struct {
	// Now is whether it is raining (or snowing) at the moment.
	Now bool `json:"now"`
	// Starts is when it begins, while it is dry; empty when it stays dry.
	Starts string `json:"starts,omitempty"`
	// Stops is when it ends, while it rains; empty when it goes on.
	Stops string `json:"stops,omitempty"`
	// Snow is whether what falls is snow.
	Snow bool `json:"snow,omitempty"`
}

// Air is the air quality now.
type Air struct {
	// Index is the number of the scale in use: the European index ("eu",
	// 0 to 100 and beyond) or the US one ("us", 0 to 500).
	Index int    `json:"index"`
	Scale string `json:"scale"`
	// Level runs from 0 (good) to 5 (as bad as the scale goes).
	Level int     `json:"level"`
	PM25  float64 `json:"pm25"`
	PM10  float64 `json:"pm10"`
	// Pollen lists what is in the air, strongest first; empty out of season
	// and outside Europe, where the service has no pollen.
	Pollen []Pollen `json:"pollen"`
}

// Pollen is one kind of pollen.
type Pollen struct {
	// Kind is alder, birch, olive, grass, mugwort or ragweed.
	Kind string `json:"kind"`
	// Level is 1 (a little), 2 (moderate) or 3 (a lot).
	Level int `json:"level"`
}

// Units are the unit labels matching the numbers in State.
type Units struct {
	Temperature   string `json:"temperature"`
	Wind          string `json:"wind"`
	Precipitation string `json:"precipitation"`
}

// Current is the weather right now.
type Current struct {
	Time          string  `json:"time"`
	Temperature   float64 `json:"temperature"`
	FeelsLike     float64 `json:"feelsLike"`
	Humidity      int     `json:"humidity"`
	WindSpeed     float64 `json:"windSpeed"`
	WindGusts     float64 `json:"windGusts"`
	WindDirection int     `json:"windDirection"`
	Precipitation float64 `json:"precipitation"`
	Code          int     `json:"code"`
	IsDay         bool    `json:"isDay"`
}

// Hour is one hour of the forecast.
type Hour struct {
	Time                     string  `json:"time"`
	Temperature              float64 `json:"temperature"`
	PrecipitationProbability int     `json:"precipitationProbability"`
	Precipitation            float64 `json:"precipitation"`
	Code                     int     `json:"code"`
	IsDay                    bool    `json:"isDay"`
}

// Day is one day of the forecast.
type Day struct {
	Date                     string  `json:"date"`
	Code                     int     `json:"code"`
	Max                      float64 `json:"max"`
	Min                      float64 `json:"min"`
	PrecipitationProbability int     `json:"precipitationProbability"`
	PrecipitationSum         float64 `json:"precipitationSum"`
	Sunrise                  string  `json:"sunrise"`
	Sunset                   string  `json:"sunset"`
	UVIndexMax               float64 `json:"uvIndexMax"`
	WindSpeedMax             float64 `json:"windSpeedMax"`
}

// Fetch implements providers.Provider.
func (p *Provider) Fetch(ctx context.Context) (any, error) {
	pl, err := p.resolve(ctx)
	if err != nil {
		return nil, err
	}
	st, err := p.forecast(ctx, pl)
	if err != nil {
		return nil, err
	}
	// The weather stands on its own: no air, no harm.
	st.Air = p.airNow(ctx, pl)
	return st, nil
}

// resolve returns the coordinates to ask the forecast for.
func (p *Provider) resolve(ctx context.Context) (place, error) {
	o := p.opt
	if o.Latitude != nil && o.Longitude != nil {
		name := o.Name
		if name == "" {
			name = o.Location
		}
		return place{Name: name, Latitude: *o.Latitude, Longitude: *o.Longitude}, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.place == nil {
		// Remembered from an earlier run, as long as the configured place is the same.
		if data, err := os.ReadFile(o.StatePath); err == nil && o.StatePath != "" {
			var saved place
			if json.Unmarshal(data, &saved) == nil && saved.Query == o.Location && saved.Name != "" {
				p.place = &saved
			}
		}
	}
	if p.place == nil {
		found, err := p.geocode(ctx, o.Location)
		if err != nil {
			return place{}, err
		}
		p.place = &found
		if o.StatePath != "" {
			if data, err := json.MarshalIndent(found, "", "  "); err == nil {
				_ = os.MkdirAll(filepath.Dir(o.StatePath), 0o700)
				_ = os.WriteFile(o.StatePath, data, 0o600)
			}
		}
	}
	pl := *p.place
	if o.Name != "" {
		pl.Name = o.Name
	}
	return pl, nil
}

// errRefused is a request the service understood and said no to, such as one
// asking for something it does not know.
var errRefused = errors.New("refused")

func (p *Provider) getJSON(ctx context.Context, rawURL string, query url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := p.opt.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // drop the long URL from the message
		}
		return fmt.Errorf("cannot reach the weather service: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Reason != "" {
			return fmt.Errorf("the weather service %w the request: %s", errRefused, apiErr.Reason)
		}
		return fmt.Errorf("the weather service answered HTTP %d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("unexpected answer from the weather service: %w", err)
	}
	return nil
}

// geocode turns a place name into coordinates. "City, Region" narrows the
// search to results in that country or region.
func (p *Provider) geocode(ctx context.Context, query string) (place, error) {
	name, hint, _ := strings.Cut(query, ",")
	name, hint = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(hint))

	var data struct {
		Results []struct {
			Name        string  `json:"name"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
			Country     string  `json:"country"`
			CountryCode string  `json:"country_code"`
			Admin1      string  `json:"admin1"`
		} `json:"results"`
	}
	q := url.Values{"name": {name}, "count": {"10"}, "language": {"en"}, "format": {"json"}}
	if err := p.getJSON(ctx, p.opt.GeocodeURL, q, &data); err != nil {
		return place{}, err
	}
	if len(data.Results) == 0 {
		return place{}, fmt.Errorf("no place called %q was found; check weather.location, or set latitude and longitude instead", query)
	}
	best := data.Results[0]
	if hint != "" {
		for _, r := range data.Results {
			if strings.EqualFold(r.CountryCode, hint) || strings.Contains(strings.ToLower(r.Country), hint) || strings.Contains(strings.ToLower(r.Admin1), hint) {
				best = r
				break
			}
		}
	}
	label := best.Name
	for _, part := range []string{best.Admin1, best.Country} {
		if part != "" && !strings.Contains(label, part) {
			label += ", " + part
		}
	}
	return place{Query: query, Name: best.Name, Latitude: best.Latitude, Longitude: best.Longitude, Label: label}, nil
}

// apiForecast mirrors the Open-Meteo forecast response. Values can be null
// towards the end of the forecast range, hence the pointers.
type apiForecast struct {
	Timezone         string `json:"timezone"`
	UTCOffsetSeconds int    `json:"utc_offset_seconds"`
	Current          struct {
		Time          string  `json:"time"`
		Temperature   float64 `json:"temperature_2m"`
		FeelsLike     float64 `json:"apparent_temperature"`
		Humidity      float64 `json:"relative_humidity_2m"`
		IsDay         int     `json:"is_day"`
		Precipitation float64 `json:"precipitation"`
		Code          int     `json:"weather_code"`
		WindSpeed     float64 `json:"wind_speed_10m"`
		WindDirection float64 `json:"wind_direction_10m"`
		WindGusts     float64 `json:"wind_gusts_10m"`
	} `json:"current"`
	Hourly struct {
		Time          []string   `json:"time"`
		Temperature   []*float64 `json:"temperature_2m"`
		PrecipProb    []*float64 `json:"precipitation_probability"`
		Precipitation []*float64 `json:"precipitation"`
		Code          []*float64 `json:"weather_code"`
		IsDay         []*float64 `json:"is_day"`
	} `json:"hourly"`
	Daily struct {
		Time         []string   `json:"time"`
		Code         []*float64 `json:"weather_code"`
		Max          []*float64 `json:"temperature_2m_max"`
		Min          []*float64 `json:"temperature_2m_min"`
		PrecipProb   []*float64 `json:"precipitation_probability_max"`
		PrecipSum    []*float64 `json:"precipitation_sum"`
		Sunrise      []string   `json:"sunrise"`
		Sunset       []string   `json:"sunset"`
		UVIndexMax   []*float64 `json:"uv_index_max"`
		WindSpeedMax []*float64 `json:"wind_speed_10m_max"`
	} `json:"daily"`
	Quarters struct {
		Time          []string   `json:"time"`
		Precipitation []*float64 `json:"precipitation"`
	} `json:"minutely_15"`
}

func at(vals []*float64, i int) (float64, bool) {
	if i < 0 || i >= len(vals) || vals[i] == nil {
		return 0, false
	}
	return *vals[i], true
}

func atOr(vals []*float64, i int) float64 {
	v, _ := at(vals, i)
	return v
}

func atString(vals []string, i int) string {
	if i < 0 || i >= len(vals) {
		return ""
	}
	return vals[i]
}

const localLayout = "2006-01-02T15:04"

func (p *Provider) forecast(ctx context.Context, pl place) (State, error) {
	tempUnit, windUnit, precipUnit := "celsius", "kmh", "mm"
	units := Units{Temperature: "°C", Wind: "km/h", Precipitation: "mm"}
	if p.opt.Units == Imperial {
		tempUnit, windUnit, precipUnit = "fahrenheit", "mph", "inch"
		units = Units{Temperature: "°F", Wind: "mph", Precipitation: "in"}
	}
	q := url.Values{
		"latitude":           {strconv.FormatFloat(pl.Latitude, 'f', 4, 64)},
		"longitude":          {strconv.FormatFloat(pl.Longitude, 'f', 4, 64)},
		"current":            {"temperature_2m,apparent_temperature,relative_humidity_2m,is_day,precipitation,weather_code,wind_speed_10m,wind_direction_10m,wind_gusts_10m"},
		"hourly":             {"temperature_2m,precipitation_probability,precipitation,weather_code,is_day"},
		"daily":              {"weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max,precipitation_sum,sunrise,sunset,uv_index_max,wind_speed_10m_max"},
		"timezone":           {"auto"},
		"forecast_days":      {"7"},
		"temperature_unit":   {tempUnit},
		"wind_speed_unit":    {windUnit},
		"precipitation_unit": {precipUnit},
	}
	p.mu.Lock()
	quarters := !p.noQuarters
	p.mu.Unlock()
	if quarters {
		q.Set("minutely_15", "precipitation")
		q.Set("forecast_minutely_15", strconv.Itoa(quartersAhead))
	}
	var api apiForecast
	err := p.getJSON(ctx, p.opt.ForecastURL, q, &api)
	if quarters && errors.Is(err, errRefused) {
		// Rather the weather without the quarter-hours than no weather.
		q.Del("minutely_15")
		q.Del("forecast_minutely_15")
		api = apiForecast{}
		if err = p.getJSON(ctx, p.opt.ForecastURL, q, &api); err == nil {
			p.mu.Lock()
			p.noQuarters = true
			p.mu.Unlock()
		}
	}
	if err != nil {
		return State{}, err
	}
	if api.Current.Time == "" || len(api.Daily.Time) == 0 {
		return State{}, errors.New("the weather service sent an incomplete forecast")
	}

	// Times arrive as local wall-clock times of the place. Attach its zone so
	// the dashboard can show them correctly wherever the Pi's own clock is set.
	loc, err := time.LoadLocation(api.Timezone)
	if err != nil || api.Timezone == "" {
		loc = time.FixedZone("", api.UTCOffsetSeconds)
	}
	stamp := func(s string) string {
		t, err := time.ParseInLocation(localLayout, s, loc)
		if err != nil {
			return ""
		}
		return t.Format(time.RFC3339)
	}

	c := api.Current
	st := State{
		Location: pl.Name,
		Timezone: api.Timezone,
		Units:    units,
		Current: Current{
			Time: stamp(c.Time), Temperature: c.Temperature, FeelsLike: c.FeelsLike,
			Humidity: int(c.Humidity + 0.5), WindSpeed: c.WindSpeed, WindGusts: c.WindGusts,
			WindDirection: int(c.WindDirection + 0.5), Precipitation: c.Precipitation,
			Code: c.Code, IsDay: c.IsDay == 1,
		},
		Hourly: []Hour{},
		Daily:  []Day{},
	}

	// The hourly list starts at midnight; keep the 24 hours from now on.
	thisHour := p.opt.Now().In(loc).Truncate(time.Hour)
	for i, s := range api.Hourly.Time {
		t, err := time.ParseInLocation(localLayout, s, loc)
		if err != nil || t.Before(thisHour) {
			continue
		}
		temp, ok := at(api.Hourly.Temperature, i)
		if !ok {
			continue
		}
		st.Hourly = append(st.Hourly, Hour{
			Time: t.Format(time.RFC3339), Temperature: temp,
			PrecipitationProbability: int(atOr(api.Hourly.PrecipProb, i) + 0.5),
			Precipitation:            atOr(api.Hourly.Precipitation, i),
			Code:                     int(atOr(api.Hourly.Code, i)),
			IsDay:                    atOr(api.Hourly.IsDay, i) == 1,
		})
		if len(st.Hourly) == 24 {
			break
		}
	}

	for i, date := range api.Daily.Time {
		hi, okHi := at(api.Daily.Max, i)
		lo, okLo := at(api.Daily.Min, i)
		if !okHi || !okLo {
			continue
		}
		st.Daily = append(st.Daily, Day{
			Date: date, Code: int(atOr(api.Daily.Code, i)), Max: hi, Min: lo,
			PrecipitationProbability: int(atOr(api.Daily.PrecipProb, i) + 0.5),
			PrecipitationSum:         atOr(api.Daily.PrecipSum, i),
			Sunrise:                  stamp(atString(api.Daily.Sunrise, i)),
			Sunset:                   stamp(atString(api.Daily.Sunset, i)),
			UVIndexMax:               atOr(api.Daily.UVIndexMax, i),
			WindSpeedMax:             atOr(api.Daily.WindSpeedMax, i),
		})
	}
	st.Rain = rainSoon(&api, st.Hourly, loc, p.opt.Now(), p.opt.Units == Imperial)
	return st, nil
}

// quartersAhead is how far "rain soon" looks: two hours.
const quartersAhead = 8

// rainSoon reads the quarter-hours from now on.
func rainSoon(api *apiForecast, hours []Hour, loc *time.Location, now time.Time, inches bool) *Rain {
	// Less than this in a quarter of an hour is not rain anyone would notice.
	wet := 0.1
	if inches {
		wet = 0.004
	}
	type quarter struct {
		at  time.Time
		wet bool
	}
	var ahead []quarter
	for i, s := range api.Quarters.Time {
		t, err := time.ParseInLocation(localLayout, s, loc)
		v, ok := at(api.Quarters.Precipitation, i)
		if err != nil || !ok || !t.Add(15*time.Minute).After(now) {
			continue
		}
		ahead = append(ahead, quarter{t, v >= wet})
		if len(ahead) == quartersAhead {
			break
		}
	}
	// Not without the quarter that is running now: the rest says nothing about "now".
	if len(ahead) < 4 || ahead[0].at.After(now) {
		return nil
	}
	rain := &Rain{Now: ahead[0].wet}
	change := time.Time{}
	for _, q := range ahead[1:] {
		if q.wet != rain.Now {
			change = q.at
			break
		}
	}
	if !change.IsZero() {
		if rain.Now {
			rain.Stops = change.Format(time.RFC3339)
		} else {
			rain.Starts = change.Format(time.RFC3339)
		}
	}
	// Snow or rain: what the forecast says for the hour in question.
	when := now
	if rain.Starts != "" {
		when = change
	}
	for _, h := range hours {
		if t, err := time.Parse(time.RFC3339, h.Time); err == nil && !t.After(when) && t.Add(time.Hour).After(when) {
			rain.Snow = (h.Code >= 71 && h.Code <= 77) || h.Code == 85 || h.Code == 86
		}
	}
	return rain
}
