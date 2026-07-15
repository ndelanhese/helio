package solar

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestDaylightAuthoritativeVectors(t *testing.T) {
	t.Parallel()
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		date       time.Time
		lat, lon   float64
		location   *time.Location
		sunriseUTC string
		sunsetUTC  string
		tolerance  time.Duration
	}{
		{"equinox Greenwich", time.Date(2024, 3, 20, 12, 0, 0, 0, time.UTC), 51.4779, 0, time.UTC, "2024-03-20T06:02:00Z", "2024-03-20T18:14:00Z", 3 * time.Minute},
		{"southern summer Sao Paulo", time.Date(2024, 12, 21, 8, 0, 0, 0, sp), -23.5505, -46.6333, sp, "2024-12-21T08:16:00Z", "2024-12-21T21:53:00Z", 4 * time.Minute},
		{"southern winter Sao Paulo", time.Date(2024, 6, 21, 8, 0, 0, 0, sp), -23.5505, -46.6333, sp, "2024-06-21T09:48:00Z", "2024-06-21T20:28:00Z", 4 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rise, set, err := Daylight(tt.date, tt.lat, tt.lon, tt.location)
			if err != nil {
				t.Fatal(err)
			}
			wantRise, _ := time.Parse(time.RFC3339, tt.sunriseUTC)
			wantSet, _ := time.Parse(time.RFC3339, tt.sunsetUTC)
			if delta := rise.UTC().Sub(wantRise).Abs(); delta > tt.tolerance {
				t.Fatalf("sunrise %s, want %s ± %s", rise.UTC(), wantRise, tt.tolerance)
			}
			if delta := set.UTC().Sub(wantSet).Abs(); delta > tt.tolerance {
				t.Fatalf("sunset %s, want %s ± %s", set.UTC(), wantSet, tt.tolerance)
			}
			if rise.Location() != tt.location || set.Location() != tt.location {
				t.Fatalf("results must use configured location")
			}
		})
	}
}

func TestDaylightKeepsEasternHemisphereLocalDay(t *testing.T) {
	t.Parallel()
	sydney, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}
	date := time.Date(2024, 12, 21, 12, 0, 0, 0, sydney)
	rise, set, err := Daylight(date, -33.8688, 151.2093, sydney)
	if err != nil {
		t.Fatal(err)
	}
	wantRise, _ := time.Parse(time.RFC3339, "2024-12-20T18:41:00Z")
	wantSet, _ := time.Parse(time.RFC3339, "2024-12-21T09:05:00Z")
	if rise.UTC().Sub(wantRise).Abs() > 4*time.Minute || set.UTC().Sub(wantSet).Abs() > 4*time.Minute {
		t.Fatalf("rise=%s set=%s", rise.UTC(), set.UTC())
	}
	if rise.In(sydney).Day() != 21 || set.In(sydney).Day() != 21 {
		t.Fatalf("events escaped requested local day: rise=%s set=%s", rise, set)
	}
}

func TestDaylightPolarErrors(t *testing.T) {
	t.Parallel()
	_, _, err := Daylight(time.Date(2024, 12, 21, 0, 0, 0, 0, time.UTC), -80, 0, time.UTC)
	if !errors.Is(err, ErrSunNeverSets) {
		t.Fatalf("summer error = %v, want ErrSunNeverSets", err)
	}
	_, _, err = Daylight(time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC), -80, 0, time.UTC)
	if !errors.Is(err, ErrSunNeverRises) {
		t.Fatalf("winter error = %v, want ErrSunNeverRises", err)
	}
}

func TestElevationEquinoxNoon(t *testing.T) {
	t.Parallel()
	got := Elevation(time.Date(2024, 3, 20, 12, 7, 0, 0, time.UTC), 0, 0)
	if math.Abs(got-89.8) > 0.5 {
		t.Fatalf("elevation = %.3f°, want about 89.8°", got)
	}
}
