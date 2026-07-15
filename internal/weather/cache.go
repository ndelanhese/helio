package weather

import (
	"context"
	"time"
)

const (
	ErrorClassProvider = "provider_unavailable"
	ErrorClassCache    = "cache_unavailable"
	freshFor           = 60 * time.Minute
	staleFor           = 6 * time.Hour
)

type Cache struct {
	Hours     []Hour
	Source    string
	FetchedAt time.Time
}

type Repository interface {
	Load(context.Context, time.Time, time.Time) (Cache, error)
	Upsert(context.Context, []Hour, string, time.Time) error
}

type Result struct {
	Hours      []Hour
	Source     string
	FetchedAt  time.Time
	Stale      bool
	Available  bool
	ErrorClass string
}

type Service struct {
	repository Repository
	provider   Provider
	now        func() time.Time
}

func NewService(repository Repository, provider Provider, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{repository: repository, provider: provider, now: clock}
}

func (s *Service) Get(ctx context.Context, request Request) Result {
	now := s.now().UTC()
	cache, cacheErr := s.repository.Load(ctx, request.Start.UTC(), request.End.UTC())
	if cacheErr == nil && completeCoverage(cache.Hours, request.Start, request.End) && allYoungerThan(cache.Hours, now, freshFor) {
		return resultFromHours(cache.Hours, false, "")
	}

	refreshed, providerErr := s.provider.Hourly(ctx, request)
	if providerErr == nil && completeCoverage(refreshed, request.Start, request.End) {
		source := providerSource(s.provider)
		if err := s.repository.Upsert(ctx, refreshed, source, now); err != nil {
			return resultFromHours(withMetadata(refreshed, source, now), false, ErrorClassCache)
		}
		persisted, err := s.repository.Load(ctx, request.Start.UTC(), request.End.UTC())
		if err != nil {
			return resultFromHours(withMetadata(refreshed, source, now), false, ErrorClassCache)
		}
		completedAt := s.now().UTC()
		stale := !completeCoverage(persisted.Hours, request.Start, request.End) || !allYoungerThan(persisted.Hours, completedAt, freshFor)
		return resultFromHours(persisted.Hours, stale, "")
	}

	usable := make([]Hour, 0, len(cache.Hours))
	for _, hour := range cache.Hours {
		age := now.Sub(hour.FetchedAt.UTC())
		if !hour.FetchedAt.IsZero() && age >= 0 && age < staleFor {
			usable = append(usable, hour)
		}
	}
	if len(usable) > 0 {
		return resultFromHours(usable, true, ErrorClassProvider)
	}
	errorClass := ErrorClassProvider
	if cacheErr != nil {
		errorClass = ErrorClassCache
	}
	return Result{ErrorClass: errorClass}
}

func completeCoverage(hours []Hour, start, end time.Time) bool {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return false
	}
	next := start.UTC().Truncate(time.Hour)
	if next.Before(start.UTC()) {
		next = next.Add(time.Hour)
	}
	index := 0
	for next.Before(end.UTC()) {
		if index >= len(hours) || !hours[index].Time.UTC().Equal(next) {
			return false
		}
		index++
		next = next.Add(time.Hour)
	}
	return index == len(hours)
}

func allYoungerThan(hours []Hour, now time.Time, maximum time.Duration) bool {
	if len(hours) == 0 {
		return false
	}
	for _, hour := range hours {
		age := now.Sub(hour.FetchedAt.UTC())
		if hour.FetchedAt.IsZero() || age < 0 || age >= maximum {
			return false
		}
	}
	return true
}

func withMetadata(hours []Hour, source string, fetchedAt time.Time) []Hour {
	result := make([]Hour, len(hours))
	for i, hour := range hours {
		hour.Source = source
		hour.FetchedAt = fetchedAt.UTC()
		result[i] = hour
	}
	return result
}

func resultFromHours(hours []Hour, stale bool, errorClass string) Result {
	if len(hours) == 0 {
		return Result{ErrorClass: errorClass}
	}
	fetchedAt := hours[0].FetchedAt.UTC()
	source := hours[0].Source
	for _, hour := range hours[1:] {
		if hour.FetchedAt.Before(fetchedAt) {
			fetchedAt = hour.FetchedAt.UTC()
		}
		if hour.Source != source {
			source = "mixed"
		}
	}
	return Result{Hours: hours, Source: source, FetchedAt: fetchedAt, Stale: stale, Available: true, ErrorClass: errorClass}
}

func providerSource(provider Provider) string {
	if sourced, ok := provider.(interface{ Source() string }); ok && sourced.Source() != "" {
		return sourced.Source()
	}
	return "weather-provider"
}
