package weather

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct {
	cache              Cache
	loadErr, upsertErr error
	upserts            int
	keepWinner         bool
}

func (r *fakeRepository) Load(context.Context, time.Time, time.Time) (Cache, error) {
	return r.cache, r.loadErr
}

func (r *fakeRepository) Upsert(_ context.Context, hours []Hour, source string, fetchedAt time.Time) error {
	r.upserts++
	if r.upsertErr != nil || r.keepWinner {
		return r.upsertErr
	}
	stored := make([]Hour, len(hours))
	for i, hour := range hours {
		hour.Time = hour.Time.UTC().Truncate(time.Hour)
		hour.FetchedAt = fetchedAt.UTC()
		hour.Source = source
		stored[i] = hour
	}
	r.cache = Cache{Hours: stored, Source: source, FetchedAt: fetchedAt.UTC()}
	return nil
}

type fakeProvider struct {
	hours  []Hour
	err    error
	calls  int
	source string
}

func (p *fakeProvider) Hourly(context.Context, Request) ([]Hour, error) {
	p.calls++
	return p.hours, p.err
}

func (p *fakeProvider) Source() string {
	if p.source == "" {
		return "open-meteo"
	}
	return p.source
}

func cachedHour(at, fetchedAt time.Time) Hour {
	return Hour{Time: at, FetchedAt: fetchedAt, Source: "open-meteo"}
}

func TestWeatherCacheFreshHitAvoidsHTTP(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{cache: Cache{Hours: []Hour{cachedHour(now, now.Add(-59*time.Minute))}}}
	provider := &fakeProvider{err: errors.New("must not be called")}
	result := NewService(repo, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if !result.Available || result.Stale || result.Source != "open-meteo" || provider.calls != 0 || result.ErrorClass != "" {
		t.Fatalf("result=%+v calls=%d", result, provider.calls)
	}
}

func TestWeatherCacheAtSixtyMinutesRefreshes(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{cache: Cache{Hours: []Hour{cachedHour(now, now.Add(-60*time.Minute))}}}
	provider := &fakeProvider{hours: []Hour{{Time: now, CloudCoverPct: 20}}}
	result := NewService(repo, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if provider.calls != 1 || !result.Available || result.Stale || !result.FetchedAt.Equal(now) {
		t.Fatalf("result=%+v calls=%d", result, provider.calls)
	}
}

func TestWeatherCacheRefreshReturnsPersistedNormalizedWinner(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{}
	brt := time.FixedZone("BRT", -3*60*60)
	provider := &fakeProvider{hours: []Hour{{Time: time.Date(2024, 6, 21, 9, 0, 0, 0, brt), CloudCoverPct: 20}}}
	result := NewService(repo, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if repo.upserts != 1 || len(result.Hours) != 1 || result.Hours[0].Time.Location() != time.UTC || !result.Hours[0].FetchedAt.Equal(now) {
		t.Fatalf("result=%+v upserts=%d", result, repo.upserts)
	}
}

func TestWeatherCacheLateOlderRefreshReturnsDatabaseWinner(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	winner := cachedHour(now, now.Add(time.Minute))
	winner.CloudCoverPct = 10
	repo := &fakeRepository{cache: Cache{Hours: []Hour{winner}}, keepWinner: true}
	provider := &fakeProvider{hours: []Hour{{Time: now, CloudCoverPct: 90}}}
	result := NewService(repo, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if len(result.Hours) != 1 || result.Hours[0].CloudCoverPct != 10 || !result.FetchedAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("service returned losing refresh: %+v", result)
	}
}

func TestWeatherCacheMixedAgesNeverBecomeFresh(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{cache: Cache{Hours: []Hour{
		cachedHour(now, now.Add(-10*time.Minute)),
		cachedHour(now.Add(time.Hour), now.Add(-5*time.Hour)),
	}}}
	provider := &fakeProvider{err: errors.New("offline")}
	result := NewService(repo, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(2 * time.Hour)})
	if provider.calls != 1 || !result.Available || !result.Stale || !result.FetchedAt.Equal(now.Add(-5*time.Hour)) || len(result.Hours) != 2 {
		t.Fatalf("mixed cache promoted: result=%+v calls=%d", result, provider.calls)
	}
}

func TestWeatherCacheAtSixHoursIsUnavailable(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{cache: Cache{Hours: []Hour{cachedHour(now, now.Add(-6*time.Hour))}}}
	result := NewService(repo, &fakeProvider{err: errors.New("offline")}, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if result.Available || result.Stale || len(result.Hours) != 0 || result.ErrorClass != ErrorClassProvider {
		t.Fatalf("result=%+v", result)
	}
}

func TestWeatherCacheFutureDatedRowDoesNotBypassProvider(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{cache: Cache{Hours: []Hour{cachedHour(now, now.Add(24*time.Hour))}}}
	provider := &fakeProvider{err: errors.New("offline")}
	result := NewService(repo, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if provider.calls != 1 || result.Available || result.Stale || result.ErrorClass != ErrorClassProvider {
		t.Fatalf("future cache bypassed provider: result=%+v calls=%d", result, provider.calls)
	}
}

func TestWeatherCacheExpiredRowsAreRemovedFromStaleFallback(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{cache: Cache{Hours: []Hour{
		cachedHour(now, now.Add(-7*time.Hour)),
		cachedHour(now.Add(time.Hour), now.Add(-2*time.Hour)),
	}}}
	result := NewService(repo, &fakeProvider{err: errors.New("offline")}, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(2 * time.Hour)})
	if !result.Available || !result.Stale || len(result.Hours) != 1 || !result.Hours[0].Time.Equal(now.Add(time.Hour)) {
		t.Fatalf("result=%+v", result)
	}
}

func TestWeatherCacheEmptyRefreshFallsBackToStale(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{cache: Cache{Hours: []Hour{cachedHour(now, now.Add(-2*time.Hour))}}}
	result := NewService(repo, &fakeProvider{}, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if !result.Available || !result.Stale || result.ErrorClass != ErrorClassProvider || len(result.Hours) != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestWeatherCachePreservesProviderNeutralSource(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	provider := &fakeProvider{source: "local-sky", hours: []Hour{{Time: now}}}
	result := NewService(&fakeRepository{}, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if result.Source != "local-sky" || result.Hours[0].Source != "local-sky" {
		t.Fatalf("result=%+v", result)
	}
}

func TestWeatherCacheEmptyFailureIsUnavailableWithoutCallerError(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	result := NewService(&fakeRepository{}, &fakeProvider{err: errors.New("secret")}, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now})
	if result.Available || result.Stale || result.ErrorClass != ErrorClassProvider || len(result.Hours) != 0 {
		t.Fatalf("result=%+v", result)
	}
}
