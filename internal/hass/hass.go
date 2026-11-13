// Package hass talks to Home Assistant through its REST API: it reads the
// state of chosen devices and switches them.
package hass

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Entity is one thing in Home Assistant, as a tile shows it.
type Entity struct {
	// ID is the entity id, e.g. "light.sofa".
	ID   string `json:"id"`
	Name string `json:"name"`
	// Domain is the kind of thing: "light", "switch", "sensor", …
	Domain string `json:"domain"`
	// Text is the state for people: "On", "Off", "21.5 °C".
	Text string `json:"text"`
	// On is true for something that is switched on or open.
	On bool `json:"on"`
	// Actionable is true when pressing OK does something: switch it, or run it.
	Actionable bool `json:"actionable"`
	// Available is false when Home Assistant cannot reach the device.
	Available bool `json:"available"`
}

// Client is a connection to one Home Assistant.
type Client struct {
	url, token string
	hc         *http.Client
}

// New returns a client. url is the address of Home Assistant, token a
// long-lived access token from its profile page.
func New(url, token string) *Client {
	return &Client{url: strings.TrimRight(strings.TrimSpace(url), "/"), token: strings.TrimSpace(token), hc: &http.Client{Timeout: 15 * time.Second}}
}

// What pressing OK does, by kind of thing. Locks and alarm panels are left
// out on purpose: a TV remote lying on the sofa should not open the door.
var actions = map[string]string{
	"light": "homeassistant/toggle", "switch": "homeassistant/toggle", "input_boolean": "homeassistant/toggle",
	"fan": "homeassistant/toggle", "cover": "cover/toggle",
	"scene": "scene/turn_on", "script": "script/turn_on",
}

type rawState struct {
	EntityID   string `json:"entity_id"`
	State      string `json:"state"`
	Attributes struct {
		FriendlyName string `json:"friendly_name"`
		Unit         string `json:"unit_of_measurement"`
	} `json:"attributes"`
}

func (r rawState) entity() Entity {
	domain, _, _ := strings.Cut(r.EntityID, ".")
	e := Entity{ID: r.EntityID, Name: r.Attributes.FriendlyName, Domain: domain, Available: r.State != "unavailable" && r.State != "unknown"}
	if e.Name == "" {
		e.Name = r.EntityID
	}
	_, e.Actionable = actions[domain]
	e.Actionable = e.Actionable && e.Available
	switch {
	case !e.Available:
		e.Text = "Not available"
	case domain == "scene" || domain == "script":
		e.Text = "" // nothing to report; OK runs it
	case r.State == "on":
		e.On, e.Text = true, "On"
	case r.State == "off":
		e.Text = "Off"
	case r.State == "open" || r.State == "opening":
		e.On, e.Text = true, "Open"
	case r.State == "closed" || r.State == "closing":
		e.Text = "Closed"
	case r.Attributes.Unit != "":
		e.Text = r.State + " " + r.Attributes.Unit
	default:
		e.Text = strings.ToUpper(r.State[:min(1, len(r.State))]) + strings.ReplaceAll(r.State[min(1, len(r.State)):], "_", " ")
	}
	return e
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url+path, rd)
	if err != nil {
		return errors.New("the Home Assistant address is not valid")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("Home Assistant cannot be reached at %s", c.url)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errors.New("Home Assistant did not accept the access token")
	case resp.StatusCode == http.StatusNotFound:
		return errors.New("Home Assistant does not know this; was the device removed?")
	case resp.StatusCode >= 300:
		return fmt.Errorf("Home Assistant answered with error %d", resp.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("%s does not answer like Home Assistant", c.url)
		}
	}
	return nil
}

// All returns everything Home Assistant has that can be shown as a tile,
// sorted by name: for the list on the settings page.
func (c *Client) All(ctx context.Context) ([]Entity, error) {
	var raw []rawState
	if err := c.do(ctx, http.MethodGet, "/api/states", nil, &raw); err != nil {
		return nil, err
	}
	shown := map[string]bool{"sensor": true, "binary_sensor": true, "climate": true, "weather": true, "media_player": true, "person": true}
	var out []Entity
	for _, r := range raw {
		e := r.entity()
		if _, ok := actions[e.Domain]; ok || shown[e.Domain] {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Get returns the given entities in the order asked for. One that no longer
// exists is reported as not available rather than failing the rest.
func (c *Client) Get(ctx context.Context, ids []string) ([]Entity, error) {
	var raw []rawState
	if err := c.do(ctx, http.MethodGet, "/api/states", nil, &raw); err != nil {
		return nil, err
	}
	byID := make(map[string]rawState, len(raw))
	for _, r := range raw {
		byID[r.EntityID] = r
	}
	out := make([]Entity, 0, len(ids))
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			out = append(out, r.entity())
			continue
		}
		domain, _, _ := strings.Cut(id, ".")
		out = append(out, Entity{ID: id, Name: id, Domain: domain, Text: "Not available"})
	}
	return out, nil
}

// ErrNoAction is returned for things that only show a value.
var ErrNoAction = errors.New("this only shows a value; there is nothing to switch")

// Activate switches a light or plug, opens or closes a cover, or runs a
// scene or script.
func (c *Client) Activate(ctx context.Context, id string) error {
	domain, _, _ := strings.Cut(id, ".")
	service, ok := actions[domain]
	if !ok {
		return ErrNoAction
	}
	return c.do(ctx, http.MethodPost, "/api/services/"+service, map[string]string{"entity_id": id}, nil)
}

// Provider reports the state of the chosen entities to the dashboard.
type Provider struct {
	Client   *Client
	Entities []string
	Every    time.Duration
}

// State is what the dashboard receives.
type State struct {
	Entities []Entity `json:"entities"`
}

// Name implements providers.Provider.
func (p *Provider) Name() string { return "home" }

// Interval implements providers.Provider.
func (p *Provider) Interval() time.Duration {
	if p.Every > 0 {
		return p.Every
	}
	return 15 * time.Second
}

// Fetch implements providers.Provider.
func (p *Provider) Fetch(ctx context.Context) (any, error) {
	entities, err := p.Client.Get(ctx, p.Entities)
	if err != nil {
		return nil, err
	}
	return State{Entities: entities}, nil
}
