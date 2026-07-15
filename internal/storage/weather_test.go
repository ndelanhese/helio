package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/weather"
)

func TestWeatherCacheTransactionalUTCUpsert(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "weather.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewWeatherRepository(db)
	fetched := time.Date(2024, 6, 21, 12, 34, 56, 123, time.FixedZone("BRT", -3*3600))
	hour := time.Date(2024, 6, 21, 9, 40, 0, 0, time.FixedZone("BRT", -3*3600))
	if err := repo.Upsert(ctx, []weather.Hour{{Time: hour, CloudCoverPct: 25, IrradianceWM2: 500}}, "open-meteo", fetched); err != nil {
		t.Fatal(err)
	}
	updated := []weather.Hour{{Time: hour.Add(10 * time.Minute), CloudCoverPct: 30, IrradianceWM2: 480}, {Time: hour.Add(time.Hour), CloudCoverPct: 35, IrradianceWM2: 450}}
	if err := repo.Upsert(ctx, updated, "open-meteo", fetched.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	hours, gotFetched, source, err := repo.Load(ctx, hour.Add(-time.Hour), hour.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 2 || hours[0].Time.Location() != time.UTC || hours[0].Time.Minute() != 0 || hours[0].CloudCoverPct != 30 || source != "open-meteo" || !gotFetched.Equal(fetched.Add(time.Minute).UTC()) {
		t.Fatalf("hours=%+v fetched=%s source=%s", hours, gotFetched, source)
	}
}
