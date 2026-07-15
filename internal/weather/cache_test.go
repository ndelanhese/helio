package weather

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRepository struct {
	hours              []Hour
	fetchedAt          time.Time
	source             string
	loadErr, upsertErr error
	upserts            int
}

func (r *fakeRepository) Load(context.Context, time.Time, time.Time) ([]Hour, time.Time, string, error) {
	return r.hours, r.fetchedAt, r.source, r.loadErr
}
func (r *fakeRepository) Upsert(context.Context, []Hour, string, time.Time) error {
	r.upserts++
	return r.upsertErr
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

func TestWeatherCacheFreshHitAvoidsHTTP(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{hours: []Hour{{Time: now}}, fetchedAt: now.Add(-59 * time.Minute), source: "open-meteo"}
	provider := &fakeProvider{err: errors.New("must not be called")}
	result := NewService(repo, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if !result.Available || result.Stale || result.Source != "open-meteo" || provider.calls != 0 || result.ErrorClass != "" {
		t.Fatalf("result=%+v calls=%d", result, provider.calls)
	}
}

func TestWeatherCacheExpiredRefreshesAndUpserts(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{hours: []Hour{{Time: now}}, fetchedAt: now.Add(-time.Hour)}
	provider := &fakeProvider{hours: []Hour{{Time: now.Add(time.Hour), CloudCoverPct: 20}}}
	result := NewService(repo, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(2 * time.Hour)})
	if !result.Available || result.Stale || result.Source != "open-meteo" || provider.calls != 1 || repo.upserts != 1 || !result.FetchedAt.Equal(now) {
		t.Fatalf("result=%+v calls=%d upserts=%d", result, provider.calls, repo.upserts)
	}
}

func TestWeatherCacheFailedRefreshReturnsHonestStale(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{hours: []Hour{{Time: now}}, fetchedAt: now.Add(-5 * time.Hour), source: "open-meteo"}
	provider := &fakeProvider{err: errors.New("private upstream detail")}
	result := NewService(repo, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if !result.Available || !result.Stale || result.ErrorClass != ErrorClassProvider || result.Source != "open-meteo" {
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

func TestWeatherCachePreservesProviderNeutralSource(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{}
	provider := &fakeProvider{source: "local-sky", hours: []Hour{{Time: now}}}
	result := NewService(repo, provider, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if result.Source != "local-sky" {
		t.Fatalf("source = %q", result.Source)
	}
}

func TestWeatherCacheEmptyRefreshFallsBackToStale(t *testing.T) {
	now := time.Date(2024, 6, 21, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepository{hours: []Hour{{Time: now}}, fetchedAt: now.Add(-2 * time.Hour), source: "open-meteo"}
	result := NewService(repo, &fakeProvider{}, func() time.Time { return now }).Get(context.Background(), Request{Start: now, End: now.Add(time.Hour)})
	if !result.Available || !result.Stale || result.ErrorClass != ErrorClassProvider || len(result.Hours) != 1 {
		t.Fatalf("result=%+v", result)
	}
}
