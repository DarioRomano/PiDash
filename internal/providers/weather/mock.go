package weather

import (
	"context"
	"math"
	"time"
)

// Mock returns a plausible made-up forecast for development (`pidash -mock`).
type Mock struct{}

// Name implements providers.Provider.
func (Mock) Name() string { return "weather" }

// Interval implements providers.Provider.
func (Mock) Interval() time.Duration { return time.Minute }

// Fetch implements providers.Provider.
func (Mock) Fetch(context.Context) (any, error) {
	now := time.Now().UTC()
	midnight := now.Truncate(24 * time.Hour)
	sunrise, sunset := 7, 17
	isDay := func(t time.Time) bool { return t.Hour() >= sunrise && t.Hour() < sunset }
	// A mild autumn day: coolest before dawn, warmest mid-afternoon.
	temp := func(t time.Time, base float64) float64 {
		h := float64(t.Hour()) + float64(t.Minute())/60
		return base + 4.5*math.Sin((h-9)/24*2*math.Pi)
	}
	codes := []int{2, 3, 61, 63, 80, 1, 0, 71, 95, 45}

	st := State{
		Location: "Sampleville",
		Timezone: "UTC",
		Units:    Units{Temperature: "°C", Wind: "km/h", Precipitation: "mm"},
		Current: Current{
			Time: now.Format(time.RFC3339), Temperature: temp(now, 9), FeelsLike: temp(now, 9) - 2.1,
			Humidity: 71, WindSpeed: 14, WindGusts: 31, WindDirection: 250, Precipitation: 0,
			Code: 2, IsDay: isDay(now),
		},
	}
	for i := 0; i < 24; i++ {
		t := now.Truncate(time.Hour).Add(time.Duration(i) * time.Hour)
		code, prob := 2, 5
		if i >= 6 && i < 12 {
			code, prob = 61, 35+i*4
		}
		st.Hourly = append(st.Hourly, Hour{
			Time: t.Format(time.RFC3339), Temperature: temp(t, 9), PrecipitationProbability: prob,
			Precipitation: float64(prob) / 100, Code: code, IsDay: isDay(t),
		})
	}
	for i := 0; i < 7; i++ {
		day := midnight.AddDate(0, 0, i)
		base := 9 + 3*math.Sin(float64(i)*1.1)
		st.Daily = append(st.Daily, Day{
			Date: day.Format("2006-01-02"), Code: codes[i%len(codes)],
			Max: base + 4.5, Min: base - 4.5, PrecipitationProbability: (i * 23) % 90,
			PrecipitationSum: float64((i*7)%12) / 2,
			Sunrise:          day.Add(time.Duration(sunrise)*time.Hour + 12*time.Minute).Format(time.RFC3339),
			Sunset:           day.Add(time.Duration(sunset)*time.Hour - 9*time.Minute).Format(time.RFC3339),
			UVIndexMax:       1.8, WindSpeedMax: 22 + float64(i),
		})
	}
	// Keep today's summary consistent with the rainy hours above.
	st.Daily[0].PrecipitationProbability, st.Daily[0].PrecipitationSum = 75, 2.4
	return st, nil
}
