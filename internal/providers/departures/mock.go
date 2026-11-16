package departures

import (
	"context"
	"time"
)

// Mock is a stop where something always leaves soon, for development.
type Mock struct {
	Stop string
	Walk int
}

// Name implements providers.Provider.
func (Mock) Name() string { return "departures" }

// Interval implements providers.Provider.
func (Mock) Interval() time.Duration { return time.Minute }

// Fetch implements providers.Provider.
func (m Mock) Fetch(context.Context) (any, error) {
	now := time.Now().Truncate(time.Minute)
	at := func(minutes int) time.Time { return now.Add(time.Duration(minutes) * time.Minute).UTC() }
	return State{Stop: m.Stop, Walk: m.Walk, Departures: []Departure{
		{Line: "4", Kind: "tram", To: "Sampleville, Station", At: at(1), Platform: "A"},
		{Line: "11", Kind: "tram", To: "Sampleville, Lake", At: at(4), Delay: 2},
		{Line: "31", Kind: "bus", To: "Northfield", At: at(7)},
		{Line: "S12", Kind: "train", To: "Brookton", At: at(12), Platform: "2"},
		{Line: "4", Kind: "tram", To: "Sampleville, Station", At: at(16), Platform: "A"},
		{Line: "IC 8", Kind: "train", To: "Harbour City", At: at(25), Platform: "7"},
		{Line: "11", Kind: "tram", To: "Sampleville, Lake", At: at(34)},
		{Line: "31", Kind: "bus", To: "Northfield", At: at(67)},
	}}, nil
}
