// Package departures reports the next trams, buses and trains from a stop
// near the house, for the dashboard: what leaves when, and whether it is
// late.
//
// The timetable comes from transport.opendata.ch, the open service for
// Swiss public transport, which needs no account. It knows every stop in
// Switzerland and those just across its borders, and nothing beyond.
package departures

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultURL is where the service is asked.
const DefaultURL = "https://transport.opendata.ch/v1"

// Departure is one tram, bus, train or boat that leaves the stop.
type Departure struct {
	// Line is what is written on its front: "4", "S12", "IC 8".
	Line string `json:"line"`
	// Kind is tram, bus, train, boat or cable (a funicular or cable car).
	Kind string `json:"kind"`
	// To is where it goes.
	To string `json:"to"`
	// At is when the timetable has it leave; Delay how many minutes later
	// than that it is expected.
	At       time.Time `json:"at"`
	Delay    int       `json:"delay,omitempty"`
	Platform string    `json:"platform,omitempty"`
}

// State is what the dashboard receives.
type State struct {
	// Stop is the stop's name, as the timetable has it.
	Stop       string      `json:"stop"`
	Departures []Departure `json:"departures"`
	// Walk is how many minutes it takes to get to the stop: what leaves
	// sooner than that is not worth showing.
	Walk int `json:"walk"`
}

// Options configure the provider.
type Options struct {
	// Stop is the stop's name or its number in the timetable.
	Stop string
	// Walk is the minutes it takes to get there.
	Walk int
	// URL is the service; Agent names this program to it.
	URL      string
	Agent    string
	Interval time.Duration
	Client   *http.Client
}

// Provider fetches the departures.
type Provider struct{ opt Options }

// New returns a provider for one stop.
func New(opt Options) *Provider {
	if opt.URL == "" {
		opt.URL = DefaultURL
	}
	opt.URL = strings.TrimRight(opt.URL, "/")
	if opt.Interval <= 0 {
		// The service allows some ten thousand questions a day; this is
		// under a thousand, and the page counts the minutes down by itself.
		opt.Interval = 90 * time.Second
	}
	if opt.Client == nil {
		opt.Client = &http.Client{Timeout: 20 * time.Second}
	}
	if opt.Walk < 0 {
		opt.Walk = 0
	}
	return &Provider{opt: opt}
}

// Name implements providers.Provider.
func (p *Provider) Name() string { return "departures" }

// Interval implements providers.Provider.
func (p *Provider) Interval() time.Duration { return p.opt.Interval }

func (p *Provider) get(ctx context.Context, path string, query url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.opt.URL+path+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	if p.opt.Agent != "" {
		req.Header.Set("User-Agent", p.opt.Agent)
	}
	resp, err := p.opt.Client.Do(req)
	if err != nil {
		return errors.New("the timetable service could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return errors.New("the timetable service was asked too often; it answers again later")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the timetable service answered HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return errors.New("the timetable service's answer broke off")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return errors.New("the timetable service gave an answer that could not be read")
	}
	return nil
}

// apiStop is a stop as the service describes it.
type apiStop struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// apiBoard mirrors the service's "stationboard" answer.
type apiBoard struct {
	Station      *apiStop `json:"station"`
	Stationboard []struct {
		Stop struct {
			Departure          string `json:"departure"`
			DepartureTimestamp *int64 `json:"departureTimestamp"`
			Delay              *int   `json:"delay"`
			Platform           string `json:"platform"`
			Prognosis          struct {
				Platform string `json:"platform"`
			} `json:"prognosis"`
		} `json:"stop"`
		Category string `json:"category"`
		Number   string `json:"number"`
		To       string `json:"to"`
	} `json:"stationboard"`
}

// Fetch implements providers.Provider.
func (p *Provider) Fetch(ctx context.Context) (any, error) {
	stop := strings.TrimSpace(p.opt.Stop)
	if stop == "" {
		return nil, errors.New("no stop is set")
	}
	q := url.Values{"limit": {"30"}}
	// A stop can be named, or given by its number in the timetable.
	if _, err := strconv.ParseUint(stop, 10, 64); err == nil {
		q.Set("id", stop)
	} else {
		q.Set("station", stop)
	}
	// Only what is shown: without this, the answer lists every stop of every journey.
	for _, f := range []string{"station/id", "station/name", "stationboard/stop/departure", "stationboard/stop/departureTimestamp", "stationboard/stop/delay",
		"stationboard/stop/platform", "stationboard/stop/prognosis/platform", "stationboard/category", "stationboard/number", "stationboard/to"} {
		q.Add("fields[]", f)
	}
	var board apiBoard
	if err := p.get(ctx, "/stationboard", q, &board); err != nil {
		return nil, err
	}
	if board.Station == nil || board.Station.Name == "" {
		return nil, fmt.Errorf("the timetable knows no stop called %q", stop)
	}
	st := State{Stop: board.Station.Name, Walk: p.opt.Walk, Departures: []Departure{}}
	for _, b := range board.Stationboard {
		var at time.Time
		switch {
		case b.Stop.DepartureTimestamp != nil && *b.Stop.DepartureTimestamp > 0:
			at = time.Unix(*b.Stop.DepartureTimestamp, 0)
		default:
			t, err := time.Parse("2006-01-02T15:04:05-0700", b.Stop.Departure)
			if err != nil {
				continue // an arrival only, or nothing to go by
			}
			at = t
		}
		d := Departure{To: strings.TrimSpace(b.To), At: at.UTC(), Platform: strings.TrimSpace(b.Stop.Platform)}
		d.Line, d.Kind = lineOf(b.Category, b.Number)
		if b.Stop.Delay != nil && *b.Stop.Delay > 0 && *b.Stop.Delay < 600 {
			d.Delay = *b.Stop.Delay
		}
		// A platform that was changed is the one to go to.
		if changed := strings.TrimSpace(b.Stop.Prognosis.Platform); changed != "" {
			d.Platform = changed
		}
		if d.To == "" {
			continue
		}
		st.Departures = append(st.Departures, d)
	}
	return st, nil
}

// lineOf turns the timetable's kind and number into what is written on the
// vehicle, and what kind of vehicle it is.
func lineOf(category, number string) (line, kind string) {
	category, number = strings.ToUpper(strings.TrimSpace(category)), strings.TrimSpace(number)
	switch category {
	case "T", "TRAM", "NFT":
		kind = "tram"
	case "B", "BUS", "NFB", "NFO", "KB", "EXB", "CAR":
		kind = "bus"
	case "BAT", "FAE", "SHIP":
		kind = "boat"
	case "FUN", "GB", "LB", "SL", "CC", "PB":
		kind = "cable"
	default:
		kind = "train"
	}
	switch {
	case kind != "train":
		// Trams and buses go by their number alone.
		if number != "" {
			return number, kind
		}
		return category, kind
	case number == "" || strings.HasPrefix(strings.ToUpper(number), category):
		if number != "" {
			return number, kind
		}
		return category, kind
	case category == "S" || category == "SN" || category == "R":
		return category + number, kind // S12, as on the train
	}
	return category + " " + number, kind // IC 8, IR 36
}

// Lookup finds the stop that a name means, for the settings page to confirm:
// its name as the timetable has it.
func (p *Provider) Lookup(ctx context.Context, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", errors.New("enter the name of a stop first")
	}
	var found struct {
		Stations []apiStop `json:"stations"`
	}
	if err := p.get(ctx, "/locations", url.Values{"query": {query}, "type": {"station"}}, &found); err != nil {
		return "", err
	}
	for _, s := range found.Stations {
		// Addresses and sights come without a number: only stops count.
		if s.ID != "" && s.Name != "" {
			return s.Name, nil
		}
	}
	return "", fmt.Errorf("the timetable knows no stop called %q; it covers Switzerland", query)
}
