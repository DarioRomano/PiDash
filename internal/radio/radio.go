// Package radio finds internet radio stations by name, using the community
// directory at radio-browser.info, so that nobody has to type the address of
// a stream with a TV remote.
package radio

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Station is a radio station: what to call it and where its stream is.
type Station struct {
	Name string `json:"name" toml:"name"`
	URL  string `json:"url" toml:"url"`
	// Detail is a note for choosing between search results, e.g. "Switzerland · MP3 128".
	Detail string `json:"detail,omitempty" toml:"-"`
}

// Servers of the directory. The first is a name shared by all of them; the
// others are tried when it does not answer.
var Servers = []string{
	"https://all.api.radio-browser.info",
	"https://de1.api.radio-browser.info",
	"https://nl1.api.radio-browser.info",
	"https://at1.api.radio-browser.info",
}

// playable are the formats a browser plays without help.
var playable = map[string]bool{"MP3": true, "AAC": true, "AAC+": true, "OGG": true, "FLAC": true}

// Search returns up to 25 stations whose name contains query, the most
// listened-to first.
func Search(ctx context.Context, query string) ([]Station, error) {
	query = strings.TrimSpace(query)
	if len(query) < 2 {
		return nil, errors.New("type at least two letters of the station's name")
	}
	params := url.Values{"name": {query}, "limit": {"60"}, "hidebroken": {"true"}, "order": {"clickcount"}, "reverse": {"true"}}
	client := &http.Client{Timeout: 10 * time.Second}
	var lastErr error
	for _, server := range Servers {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/json/stations/search?"+params.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "PiDash")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			lastErr = errors.New("unexpected answer")
			continue
		}
		var raw []struct {
			Name        string `json:"name"`
			URL         string `json:"url"`
			URLResolved string `json:"url_resolved"`
			Country     string `json:"country"`
			Codec       string `json:"codec"`
			Bitrate     int    `json:"bitrate"`
			HLS         int    `json:"hls"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			lastErr = err
			continue
		}
		out := []Station{}
		seen := map[string]bool{}
		for _, r := range raw {
			stream := r.URLResolved
			if stream == "" {
				stream = r.URL
			}
			name := strings.Join(strings.Fields(r.Name), " ")
			// Playlists of the HLS kind need a player the dashboard does not have.
			if r.HLS != 0 || !playable[strings.ToUpper(r.Codec)] || name == "" || seen[stream] {
				continue
			}
			if u, err := url.Parse(stream); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				continue
			}
			seen[stream] = true
			detail := []string{}
			if r.Country != "" {
				detail = append(detail, r.Country)
			}
			if r.Bitrate > 0 {
				detail = append(detail, strings.ToUpper(r.Codec)+" "+itoa(r.Bitrate))
			}
			out = append(out, Station{Name: name, URL: stream, Detail: strings.Join(detail, " · ")})
			if len(out) == 25 {
				break
			}
		}
		return out, nil
	}
	if lastErr != nil {
		return nil, errors.New("the station directory could not be reached")
	}
	return []Station{}, nil
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
