package photos

import (
	"context"
	"strings"
	"time"
)

// Detail is what is known about one photo: for the page that says where and
// when it was taken.
type Detail struct {
	Key    string    `json:"key"`
	Name   string    `json:"name"`
	Folder string    `json:"folder"`
	Taken  time.Time `json:"taken"`
	// WallClock says that Taken is the time on the camera's clock, to be
	// shown as it stands rather than moved into the viewer's time zone.
	WallClock bool   `json:"wallClock"`
	Place     string `json:"place,omitempty"`
	// Located says that Latitude and Longitude are set.
	Located   bool    `json:"located"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Favourite bool    `json:"favourite"`
	Video     bool    `json:"video,omitempty"`
}

// Detailer is implemented by sources that find out more about a photo by
// looking into its file, which is too slow to do for a whole library.
type Detailer interface {
	Details(ctx context.Context, item Item) (Extra, error)
}

// Info returns what is known about the photos of one slide; key may name two
// photos shown side by side ("a+b").
func (s *Service) Info(ctx context.Context, key string) ([]Detail, error) {
	var out []Detail
	for _, k := range strings.Split(key, pairSep) {
		s.mu.Lock()
		item, ok := s.byKey[k]
		favourite := s.favourites[k]
		s.mu.Unlock()
		if !ok {
			return nil, ErrNotFound
		}
		d := Detail{
			Key: item.Key, Name: item.Name, Folder: item.Folder, Taken: item.Taken, WallClock: item.WallClock, Place: item.Place,
			Located: item.Located, Latitude: item.Latitude, Longitude: item.Longitude, Favourite: favourite, Video: item.Video,
		}
		if src, ok := s.opt.Source.(Detailer); ok && !item.Video {
			// What the file says counts for more than what its folder does.
			if extra, err := src.Details(ctx, item); err == nil {
				if !extra.Taken.IsZero() {
					d.Taken, d.WallClock = extra.Taken, true
				}
				if extra.Located {
					d.Located, d.Latitude, d.Longitude = true, extra.Latitude, extra.Longitude
				}
			}
		}
		out = append(out, d)
	}
	return out, nil
}
