package tariffs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
)

const maxResponseBytes = 64 * 1024

// Fetcher obtains a public source document. Service validates both source and
// response before parsing, so injected fetchers cannot broaden the source set.
type Fetcher interface {
	Fetch(context.Context, string) ([]byte, error)
}

type proposalWriter interface {
	CreateProposal(context.Context, domain.TariffProposal) (domain.TariffProposal, error)
	HasApprovedTariff(context.Context) (bool, error)
}

type clock interface{ Now() time.Time }

// Status contains only the state and freshness information safe for component
// health. It intentionally exposes neither rates nor proposal details.
type Status struct {
	State     string
	UpdatedAt time.Time
	FetchedAt time.Time
}

// Service refreshes pending tariff candidates without ever approving them.
type Service struct {
	fetcher Fetcher
	writer  proposalWriter
	clock   clock
}

func NewService(fetcher Fetcher, writer proposalWriter, clock clock) *Service {
	if clock == nil {
		clock = wallClock{}
	}
	return &Service{fetcher: fetcher, writer: writer, clock: clock}
}

func (s *Service) Refresh(ctx context.Context) (Status, error) {
	now := s.clock.Now().UTC()
	if s.fetcher == nil || s.writer == nil {
		return Status{State: "unavailable", UpdatedAt: now}, errors.New("tariff refresh is not configured")
	}
	page, err := s.fetcher.Fetch(ctx, CopelGroupBURL)
	if err == nil && len(page) > maxResponseBytes {
		err = fmt.Errorf("tariff source response exceeds %d bytes", maxResponseBytes)
	}
	if err == nil {
		parsed, parseErr := ParseCopelGroupB(page, Selection{Class: "B1", Subclass: "residential"}, now)
		if parseErr != nil {
			err = parseErr
		} else if _, writeErr := s.writer.CreateProposal(ctx, parsed.Candidate); writeErr != nil {
			err = fmt.Errorf("store tariff proposal: %w", writeErr)
		}
	}
	if err == nil {
		return Status{State: "available", UpdatedAt: now, FetchedAt: now}, nil
	}
	approved, approvedErr := s.writer.HasApprovedTariff(ctx)
	if approvedErr != nil {
		return Status{State: "unavailable", UpdatedAt: now}, fmt.Errorf("refresh tariff source: %w", errors.Join(err, approvedErr))
	}
	state := "unavailable"
	if approved {
		state = "stale"
	}
	return Status{State: state, UpdatedAt: now}, fmt.Errorf("refresh tariff source: %w", err)
}

// HTTPFetcher is the bounded production fetcher. It permits only HTTPS URLs on
// official Copel and ANEEL hosts, follows no redirects, and reads at most 64KiB.
type HTTPFetcher struct{ client *http.Client }

func NewHTTPFetcher(client *http.Client) *HTTPFetcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPFetcher{client: &copy}
}

func (f *HTTPFetcher) Fetch(ctx context.Context, source string) ([]byte, error) {
	if err := validateOfficialURL(source); err != nil {
		return nil, err
	}
	if f == nil || f.client == nil {
		return nil, errors.New("tariff HTTP fetcher is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, fmt.Errorf("create tariff request: %w", err)
	}
	response, err := f.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch tariff source: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch tariff source: unexpected HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read tariff source: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("tariff source response exceeds %d bytes", maxResponseBytes)
	}
	return body, nil
}

func validateOfficialURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Host == "" || parsed.Port() != "" {
		return errors.New("tariff source must be an official HTTPS URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "www.copel.com" && host != "copel.com" && host != "www.gov.br" && host != "gov.br" {
		return errors.New("tariff source host is not allowed")
	}
	return nil
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }
