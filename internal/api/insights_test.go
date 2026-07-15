package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ndelanhese/helio/internal/alerts"
	"github.com/ndelanhese/helio/internal/api"
	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/storage"
)

func TestInsightsContractIsVersionedHonestAndTariffDerived(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	analysisStore := storage.NewAnalysisRepository(f.db)
	if err := analysisStore.Save(context.Background(), domain.DailyAnalysis{
		Day: "2026-07-14", ExpectedWh: 15_000, ActualWh: 12_340, Ratio: 12_340.0 / 15_000,
		Confidence: domain.ConfidenceMedium, Qualifying: true, AnalyzedAt: time.Date(2026, 7, 15, 3, 5, 0, 0, time.UTC),
		Evidence: []domain.Evidence{{Code: "history_days", Label: "Histórico qualificável", Value: 12, Unit: "days"}, {Code: "weather_stale", Label: "Contexto meteorológico desatualizado", Value: 2, Unit: "hours"}},
	}); err != nil {
		t.Fatal(err)
	}
	handler := api.New(api.Dependencies{
		Auth: auth.NewManager(f.db), Store: f.db, History: f.repo, Insights: analysisStore,
		Now: func() time.Time { return time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC) },
	})
	rec := request(t, handler, http.MethodGet, "/api/v1/insights?day=2026-07-14", "", cookie, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("insights: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Version    string            `json:"version"`
		Day        string            `json:"day"`
		ActualWh   float64           `json:"actualWh"`
		ExpectedWh float64           `json:"expectedWh"`
		Ratio      float64           `json:"ratio"`
		Confidence domain.Confidence `json:"confidence"`
		Evidence   []struct {
			Code, Label, Unit string
			Value             float64
		} `json:"evidence"`
		Trends struct {
			PeakPower struct {
				Direction  string `json:"direction"`
				WindowDays int    `json:"windowDays"`
			} `json:"peakPower"`
			ProductiveMinutes struct {
				Direction  string `json:"direction"`
				WindowDays int    `json:"windowDays"`
			} `json:"productiveMinutes"`
		} `json:"trends"`
		GeneratedEnergyValue struct {
			Minor    int64  `json:"minor"`
			Currency string `json:"currency"`
			Label    string `json:"label"`
			Estimate bool   `json:"estimate"`
		} `json:"generatedEnergyValue"`
		ObservationWindow struct{ QualifyingDays, MinimumDays int } `json:"observationWindow"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != "v1" || body.Day != "2026-07-14" || body.ActualWh != 12_340 || body.ExpectedWh != 15_000 || body.Confidence != domain.ConfidenceMedium || len(body.Evidence) != 2 {
		t.Fatalf("unexpected contract: %#v", body)
	}
	if body.GeneratedEnergyValue.Minor != 1_172 || body.GeneratedEnergyValue.Currency != "BRL" || body.GeneratedEnergyValue.Label != "valor estimado da energia gerada" || !body.GeneratedEnergyValue.Estimate {
		t.Fatalf("generated value: %#v", body.GeneratedEnergyValue)
	}
	if body.Trends.PeakPower.Direction == "" || body.Trends.ProductiveMinutes.Direction == "" {
		t.Fatalf("trend contract missing: %#v", body.Trends)
	}
	if body.ObservationWindow.QualifyingDays != 12 || body.ObservationWindow.MinimumDays != 7 {
		t.Fatalf("observation window: %#v", body.ObservationWindow)
	}
	prohibited := []string{"loggerHost", "loggerSerial", "error", "self" + "-consumption", "auto" + "consumo", "grid " + "import", "grid " + "export", "econo" + "mia", "pou" + "pança"}
	lower := strings.ToLower(rec.Body.String())
	for _, phrase := range prohibited {
		if strings.Contains(lower, strings.ToLower(phrase)) {
			t.Fatalf("response contains prohibited %q: %s", phrase, rec.Body.String())
		}
	}

	for _, day := range []string{"", "2026-02-30", "2026-7-14", "2026-07-14T00:00:00Z"} {
		bad := request(t, handler, http.MethodGet, "/api/v1/insights?day="+day, "", cookie, "")
		if bad.Code != http.StatusUnprocessableEntity {
			t.Fatalf("day %q: %d %s", day, bad.Code, bad.Body.String())
		}
	}
}

func TestAlertsContractUsesExactStateEnumAndSanitizedEvidence(t *testing.T) {
	f := newFixture(t)
	cookie, _ := bootstrap(t, f)
	repository := storage.NewAlertRepository(f.db)
	engine, err := alerts.NewEngine(repository, alerts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	for i := range 3 {
		if _, err := engine.Evaluate(context.Background(), alerts.Input{At: base.Add(time.Duration(i) * time.Minute), PollObserved: true}); err != nil {
			t.Fatal(err)
		}
	}
	handler := api.New(api.Dependencies{Auth: auth.NewManager(f.db), Store: f.db, History: f.repo, Alerts: repository})
	open := request(t, handler, http.MethodGet, "/api/v1/alerts?state=open", "", cookie, "")
	if open.Code != http.StatusOK {
		t.Fatalf("open alerts: %d %s", open.Code, open.Body.String())
	}
	var body struct {
		Version string `json:"version"`
		State   string `json:"state"`
		Alerts  []struct {
			Kind, State, Severity, Title, Summary string
			OpenedAt                              string  `json:"openedAt"`
			ResolvedAt                            *string `json:"resolvedAt"`
			Evidence                              []struct {
				Label, Unit string
				Value       float64
			}
		} `json:"alerts"`
	}
	if err := json.Unmarshal(open.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != "v1" || body.State != "open" || len(body.Alerts) != 1 || body.Alerts[0].State != "open" || body.Alerts[0].Kind != alerts.RuleLoggerOffline || body.Alerts[0].ResolvedAt != nil {
		t.Fatalf("alert contract: %#v", body)
	}
	if strings.Contains(open.Body.String(), `"id"`) || strings.Contains(open.Body.String(), `"values"`) || strings.Contains(open.Body.String(), `"timestamps"`) {
		t.Fatalf("raw alert internals leaked: %s", open.Body.String())
	}
	if _, err := engine.Evaluate(context.Background(), alerts.Input{At: base.Add(3 * time.Minute), PollObserved: true, PollSucceeded: true}); err != nil {
		t.Fatal(err)
	}
	resolved := request(t, handler, http.MethodGet, "/api/v1/alerts?state=resolved", "", cookie, "")
	if resolved.Code != http.StatusOK || !strings.Contains(resolved.Body.String(), `"state":"resolved"`) || !strings.Contains(resolved.Body.String(), `"resolvedAt":"`) || !strings.Contains(resolved.Body.String(), "A comunicação com o logger foi restabelecida.") {
		t.Fatalf("resolved alert recovery contract: %d %s", resolved.Code, resolved.Body.String())
	}
	for _, state := range []string{"", "active", "OPEN", "all"} {
		bad := request(t, handler, http.MethodGet, "/api/v1/alerts?state="+state, "", cookie, "")
		if bad.Code != http.StatusUnprocessableEntity {
			t.Fatalf("state %q: %d %s", state, bad.Code, bad.Body.String())
		}
	}
}
