package tariffs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/tariffs"
)

func TestRefreshKeepsApprovedTariffWhenFetchFails(t *testing.T) {
	repo := &proposalRepository{approved: true}
	clock := fixedClock{now: time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)}
	status, err := tariffs.NewService(failingFetcher{}, repo, clock).Refresh(context.Background())
	if err == nil {
		t.Fatal("Refresh() error = nil, want fetch error")
	}
	if status.State != "stale" {
		t.Errorf("status.State = %q, want stale", status.State)
	}
	if len(repo.proposals) != 0 {
		t.Errorf("created %d proposals after a failed fetch, want none", len(repo.proposals))
	}
}

func TestRefreshPersistsOnlyAValidPendingCandidate(t *testing.T) {
	repo := &proposalRepository{}
	clock := fixedClock{now: time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)}
	status, err := tariffs.NewService(staticFetcher{body: fixture(t, "copel-group-b.html")}, repo, clock).Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if status.State != "available" || !status.FetchedAt.Equal(clock.now) {
		t.Errorf("status = %#v, want available at %s", status, clock.now)
	}
	if len(repo.proposals) != 1 {
		t.Fatalf("created %d proposals, want 1", len(repo.proposals))
	}
	if !repo.proposals[0].ApprovedAt.IsZero() {
		t.Fatal("Refresh() approved a discovered tariff proposal")
	}
}

func TestOfficialSourceAllowlistUsesExactApprovedPaths(t *testing.T) {
	for _, source := range []string{
		tariffs.CopelGroupBURL,
		tariffs.ANEELTariffsURL,
	} {
		if err := tariffs.ValidateOfficialURL(source); err != nil {
			t.Errorf("ValidateOfficialURL(%q) error = %v", source, err)
		}
	}
	for _, source := range []string{
		"https://www.gov.br/",
		"https://www.gov.br/aneel/pt-br/assuntos/tarifas/other",
		"https://www.copel.com/site/copel-distribuicao/tarifas-de-energia-eletrica/other",
		"https://copel.com/site/copel-distribuicao/tarifas-de-energia-eletrica/",
		"https://www.copel.com/site/copel-distribuicao/tarifas-de-energia-eletrica/?source=test",
	} {
		if err := tariffs.ValidateOfficialURL(source); err == nil {
			t.Errorf("ValidateOfficialURL(%q) error = nil, want rejection", source)
		}
	}
}

type failingFetcher struct{}

func (failingFetcher) Fetch(context.Context, string) ([]byte, error) {
	return nil, errors.New("source down")
}

type staticFetcher struct{ body []byte }

func (f staticFetcher) Fetch(context.Context, string) ([]byte, error) { return f.body, nil }

type proposalRepository struct {
	approved  bool
	proposals []domain.TariffProposal
}

func (r *proposalRepository) CreateProposal(_ context.Context, proposal domain.TariffProposal) (domain.TariffProposal, error) {
	r.proposals = append(r.proposals, proposal)
	return proposal, nil
}

func (r *proposalRepository) HasApprovedTariff(context.Context) (bool, error) { return r.approved, nil }

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }
