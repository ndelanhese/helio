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

type Repository interface {
	Load(context.Context, time.Time, time.Time) ([]Hour, time.Time, string, error)
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
	hours, fetchedAt, source, cacheErr := s.repository.Load(ctx, request.Start.UTC(), request.End.UTC())
	age := now.Sub(fetchedAt.UTC())
	if len(hours) > 0 && age >= 0 && age < freshFor {
		return Result{Hours: hours, Source: source, FetchedAt: fetchedAt.UTC(), Available: true}
	}
	refreshed, providerErr := s.provider.Hourly(ctx, request)
	if providerErr == nil && len(refreshed) > 0 {
		source := providerSource(s.provider)
		if err := s.repository.Upsert(ctx, refreshed, source, now); err != nil {
			return Result{Hours: refreshed, Source: source, FetchedAt: now, Available: true, ErrorClass: ErrorClassCache}
		}
		return Result{Hours: refreshed, Source: source, FetchedAt: now, Available: true}
	}
	if len(hours) > 0 && age >= 0 && age < staleFor {
		return Result{Hours: hours, Source: source, FetchedAt: fetchedAt.UTC(), Stale: true, Available: true, ErrorClass: ErrorClassProvider}
	}
	errorClass := ErrorClassProvider
	if cacheErr != nil {
		errorClass = ErrorClassCache
	}
	return Result{ErrorClass: errorClass}
}

func providerSource(provider Provider) string {
	if sourced, ok := provider.(interface{ Source() string }); ok && sourced.Source() != "" {
		return sourced.Source()
	}
	return "weather-provider"
}
