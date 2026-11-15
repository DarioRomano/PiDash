package weather

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"time"
)

const (
	// airEvery is how often the air is asked for; the service has a new value
	// once an hour.
	airEvery = 30 * time.Minute
	// airKept is how long the last answer is shown when no new one comes.
	airKept = 3 * time.Hour
)

// apiAir mirrors the Open-Meteo air quality response. Everything can be null:
// pollen outside Europe and out of season, the rest where no model reaches.
type apiAir struct {
	Current struct {
		EuropeanAQI *float64 `json:"european_aqi"`
		USAQI       *float64 `json:"us_aqi"`
		PM10        *float64 `json:"pm10"`
		PM25        *float64 `json:"pm2_5"`
		Alder       *float64 `json:"alder_pollen"`
		Birch       *float64 `json:"birch_pollen"`
		Grass       *float64 `json:"grass_pollen"`
		Mugwort     *float64 `json:"mugwort_pollen"`
		Olive       *float64 `json:"olive_pollen"`
		Ragweed     *float64 `json:"ragweed_pollen"`
	} `json:"current"`
}

const (
	airIndexes = "european_aqi,us_aqi,"
	airRest    = "pm10,pm2_5,alder_pollen,birch_pollen,grass_pollen,mugwort_pollen,olive_pollen,ragweed_pollen"
)

// airNow returns the air quality, from memory while that is fresh. Nil when it
// is not known: the weather is shown without it.
func (p *Provider) airNow(ctx context.Context, pl place) *Air {
	now := p.opt.Now()
	p.mu.Lock()
	air, at, noIndex := p.air, p.airAt, p.noIndex
	p.mu.Unlock()
	if !at.IsZero() && now.Sub(at) < airEvery {
		return air
	}

	q := url.Values{
		"latitude":  {strconv.FormatFloat(pl.Latitude, 'f', 4, 64)},
		"longitude": {strconv.FormatFloat(pl.Longitude, 'f', 4, 64)},
		"current":   {airIndexes + airRest},
		"timezone":  {"auto"},
	}
	if noIndex {
		q.Set("current", airRest)
	}
	var api apiAir
	err := p.getJSON(ctx, p.opt.AirURL, q, &api)
	if !noIndex && errors.Is(err, errRefused) {
		// The index can be worked out from the dust, should the service not have it.
		q.Set("current", airRest)
		api = apiAir{}
		if err = p.getJSON(ctx, p.opt.AirURL, q, &api); err == nil {
			noIndex = true
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.noIndex = noIndex
	if err != nil {
		if now.Sub(p.airAt) > airKept {
			p.air = nil
		}
		return p.air
	}
	p.air, p.airAt = readAir(&api, p.opt.Units == Imperial), now
	return p.air
}

// The upper ends of the levels "good" to "very poor" of each scale.
var (
	euBands   = []float64{20, 40, 60, 80, 100}
	usBands   = []float64{50, 100, 150, 200, 300}
	pm25Bands = []float64{10, 20, 25, 50, 75}
	pm10Bands = []float64{20, 40, 50, 100, 150}
)

func band(v float64, bands []float64) int {
	for i, top := range bands {
		if v <= top {
			return i
		}
	}
	return len(bands)
}

// indexFrom works the European index out of one kind of dust: each level is
// twenty points wide, as on the real scale.
func indexFrom(v float64, bands []float64) float64 {
	low := 0.0
	for i, top := range bands {
		if v <= top {
			return float64(i)*20 + (v-low)/(top-low)*20
		}
		low = top
	}
	return 100 + (v - low)
}

// readAir turns the answer into what is shown. Places measured in miles and
// Fahrenheit get the US index, everyone else the European one.
func readAir(api *apiAir, american bool) *Air {
	c := api.Current
	air := &Air{Pollen: []Pollen{}}
	if c.PM25 != nil {
		air.PM25 = *c.PM25
	}
	if c.PM10 != nil {
		air.PM10 = *c.PM10
	}
	switch {
	case american && c.USAQI != nil:
		air.Scale, air.Index, air.Level = "us", int(*c.USAQI+0.5), band(*c.USAQI, usBands)
	case c.EuropeanAQI != nil:
		air.Scale, air.Index, air.Level = "eu", int(*c.EuropeanAQI+0.5), band(*c.EuropeanAQI, euBands)
	case c.PM25 != nil || c.PM10 != nil:
		index := 0.0
		if c.PM25 != nil {
			index = indexFrom(*c.PM25, pm25Bands)
		}
		if c.PM10 != nil {
			index = max(index, indexFrom(*c.PM10, pm10Bands))
		}
		air.Scale, air.Index, air.Level = "eu", int(index+0.5), band(index, euBands)
	default:
		return nil
	}

	// Grains per cubic metre; what counts as a lot differs between plants.
	kinds := []struct {
		name       string
		v          *float64
		some, lots float64
	}{
		{"alder", c.Alder, 15, 90}, {"birch", c.Birch, 15, 90}, {"olive", c.Olive, 15, 90},
		{"grass", c.Grass, 5, 20},
		{"mugwort", c.Mugwort, 10, 50}, {"ragweed", c.Ragweed, 10, 50},
	}
	for _, k := range kinds {
		if k.v == nil || *k.v < 1 {
			continue
		}
		level := 1
		if *k.v >= k.lots {
			level = 3
		} else if *k.v >= k.some {
			level = 2
		}
		air.Pollen = append(air.Pollen, Pollen{Kind: k.name, Level: level})
	}
	sort.SliceStable(air.Pollen, func(i, j int) bool { return air.Pollen[i].Level > air.Pollen[j].Level })
	return air
}
