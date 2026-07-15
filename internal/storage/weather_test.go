package storage

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
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
	cache, err := repo.Load(ctx, hour.Add(-time.Hour), hour.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.Hours) != 2 || cache.Hours[0].Time.Location() != time.UTC || cache.Hours[0].Time.Minute() != 0 || cache.Hours[0].CloudCoverPct != 30 || cache.Source != "open-meteo" || !cache.FetchedAt.Equal(fetched.Add(time.Minute).UTC()) || !cache.Hours[0].FetchedAt.Equal(fetched.Add(time.Minute).UTC()) {
		t.Fatalf("cache=%+v", cache)
	}
}

type controlledWeatherProvider struct {
	hours   []weather.Hour
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *controlledWeatherProvider) Hourly(ctx context.Context, _ weather.Request) ([]weather.Hour, error) {
	p.once.Do(func() { close(p.started) })
	select {
	case <-p.release:
		return p.hours, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *controlledWeatherProvider) Source() string { return "test-provider" }

type staticWeatherProvider struct {
	hours []weather.Hour
	err   error
}

func (p staticWeatherProvider) Hourly(context.Context, weather.Request) ([]weather.Hour, error) {
	return p.hours, p.err
}

func (p staticWeatherProvider) Source() string { return "test-provider" }

func TestWeatherCacheOlderCompletionCannotOverwriteNewer(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "weather-order.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewWeatherRepository(db)
	hour := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	newFetched := hour.Add(20 * time.Minute)
	if err := repo.Upsert(ctx, []weather.Hour{{Time: hour, CloudCoverPct: 10, IrradianceWM2: 700}}, "new", newFetched); err != nil {
		t.Fatal(err)
	}
	oldDone := make(chan error, 1)
	go func() {
		oldDone <- repo.Upsert(ctx, []weather.Hour{{Time: hour, CloudCoverPct: 90, IrradianceWM2: 50}}, "old", newFetched.Add(-time.Minute))
	}()
	if err := <-oldDone; err != nil {
		t.Fatal(err)
	}
	cache, err := repo.Load(ctx, hour, hour.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.Hours) != 1 || cache.Hours[0].CloudCoverPct != 10 || cache.Source != "new" || !cache.FetchedAt.Equal(newFetched) {
		t.Fatalf("older completion overwrote cache: %+v", cache)
	}
}

func TestWeatherCacheConcurrentOlderRefreshReturnsNewerDatabaseWinner(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "weather-concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewWeatherRepository(db)
	hour := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	request := weather.Request{Start: hour, End: hour.Add(time.Hour)}
	oldProvider := &controlledWeatherProvider{hours: []weather.Hour{{Time: hour, CloudCoverPct: 90, IrradianceWM2: 50}}, started: make(chan struct{}), release: make(chan struct{})}
	newProvider := &controlledWeatherProvider{hours: []weather.Hour{{Time: hour, CloudCoverPct: 10, IrradianceWM2: 700}}, started: make(chan struct{}), release: make(chan struct{})}
	close(newProvider.release)
	oldService := weather.NewService(repo, oldProvider, sequenceClock(hour.Add(10*time.Minute), hour.Add(21*time.Minute)))
	newService := weather.NewService(repo, newProvider, sequenceClock(hour.Add(20*time.Minute), hour.Add(21*time.Minute)))
	oldResult := make(chan weather.Result, 1)
	go func() { oldResult <- oldService.Get(ctx, request) }()
	<-oldProvider.started
	newResult := newService.Get(ctx, request)
	close(oldProvider.release)
	lateResult := <-oldResult
	for name, result := range map[string]weather.Result{"new": newResult, "late-old": lateResult} {
		if len(result.Hours) != 1 || result.Hours[0].CloudCoverPct != 10 || result.Hours[0].IrradianceWM2 != 700 || !result.FetchedAt.Equal(hour.Add(20*time.Minute)) {
			t.Fatalf("%s result did not use database winner: %+v", name, result)
		}
		if result.Stale || result.ErrorClass != "" {
			t.Fatalf("%s coherent winner marked degraded: %+v", name, result)
		}
	}
}

func TestWeatherCacheFuturePersistedRowDoesNotDefeatSuccessfulRefresh(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "weather-future-success.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewWeatherRepository(db)
	hour := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	if err := repo.Upsert(ctx, []weather.Hour{{Time: hour, CloudCoverPct: 99, IrradianceWM2: 1}}, "corrupt", hour.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	provider := staticWeatherProvider{hours: []weather.Hour{{Time: hour, CloudCoverPct: 10, IrradianceWM2: 700}}}
	service := weather.NewService(repo, provider, sequenceClock(hour, hour.Add(time.Second)))
	result := service.Get(ctx, weather.Request{Start: hour, End: hour.Add(time.Hour)})
	if !result.Available || result.Stale || result.ErrorClass != weather.ErrorClassCache || result.Source != "test-provider" || len(result.Hours) != 1 || result.Hours[0].CloudCoverPct != 10 || result.Hours[0].IrradianceWM2 != 700 {
		t.Fatalf("future persisted row defeated valid refresh: %+v", result)
	}
}

func TestWeatherCacheFuturePersistedRowIsUnavailableOnProviderFailure(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "weather-future-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewWeatherRepository(db)
	hour := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	if err := repo.Upsert(ctx, []weather.Hour{{Time: hour, CloudCoverPct: 99, IrradianceWM2: 1}}, "corrupt", hour.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	service := weather.NewService(repo, staticWeatherProvider{err: errors.New("offline")}, func() time.Time { return hour })
	result := service.Get(ctx, weather.Request{Start: hour, End: hour.Add(time.Hour)})
	if result.Available || result.Stale || result.ErrorClass != weather.ErrorClassProvider || len(result.Hours) != 0 {
		t.Fatalf("future persisted row served during outage: %+v", result)
	}
}

func sequenceClock(times ...time.Time) func() time.Time {
	var mu sync.Mutex
	index := 0
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		if index >= len(times) {
			return times[len(times)-1]
		}
		value := times[index]
		index++
		return value
	}
}
