package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ndelanhese/helio/internal/auth"
	"github.com/ndelanhese/helio/internal/collector"
	"github.com/ndelanhese/helio/internal/domain"
	"github.com/ndelanhese/helio/internal/storage"
)

type Store interface {
	GetSettings(context.Context, ...bool) (domain.Settings, error)
	PutSettings(context.Context, domain.Settings, ...bool) error
	PrepareBackup(context.Context) (io.ReadCloser, error)
}

type HistoryStore interface {
	History(context.Context, time.Time, time.Time) ([]domain.HistoryPoint, error)
}

type InsightsStore interface {
	Load(context.Context, string) (domain.DailyAnalysis, bool, error)
}

type AlertStore interface {
	List(context.Context, string) ([]storage.AlertRecord, error)
}

type SummaryStore interface {
	DailyHistory(context.Context, time.Time, time.Time) ([]domain.AggregatePoint, error)
}

type FinanceStore interface {
	SaveCycle(context.Context, domain.BillingCycle, string) (domain.BillingCycle, domain.FinancialProjection, error)
	ListCycles(context.Context, int) ([]domain.BillingCycle, error)
	LatestProjection(context.Context, time.Time) (domain.FinancialProjection, bool, error)
	ListTariffProposals(context.Context) ([]domain.TariffProposal, error)
	ApproveProposal(context.Context, int64, string) (domain.TariffVersion, error)
}

type Dependencies struct {
	Auth              *auth.Manager
	Store             Store
	History           HistoryStore
	Insights          InsightsStore
	Alerts            AlertStore
	Summaries         SummaryStore
	Finance           FinanceStore
	Latest            func() collector.State
	Hub               *collector.Hub
	Reconfigure       func(context.Context, domain.Settings) error
	ApplySettings     func(context.Context, domain.Settings, string) error
	Components        func(context.Context) ComponentStatus
	ShutdownContext   context.Context
	AllowPublicLogger bool
	Now               func() time.Time
	SSEHeartbeat      time.Duration
}

type API struct{ dependencies Dependencies }

func New(d Dependencies) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Latest == nil {
		d.Latest = func() collector.State { return collector.State{} }
	}
	if d.Hub == nil {
		d.Hub = collector.NewHub()
	}
	if d.SSEHeartbeat <= 0 {
		d.SSEHeartbeat = 15 * time.Second
	}
	if d.ShutdownContext == nil {
		d.ShutdownContext = context.Background()
	}
	a := &API{dependencies: d}
	r := chi.NewRouter()
	r.Use(requestMetadata)
	r.Get("/api/v1/bootstrap/status", a.bootstrapStatus)
	r.With(auth.RequireSameOrigin).Post("/api/v1/bootstrap", a.bootstrap)
	r.With(auth.RequireSameOrigin).Post("/api/v1/auth/login", a.login)
	r.Get("/health/components", a.componentHealth)
	if d.Auth != nil {
		private := chi.NewRouter()
		private.Use(func(next http.Handler) http.Handler { return auth.RequireSession(d.Auth, next) })
		private.Use(func(next http.Handler) http.Handler { return auth.BootstrapGate(d.Auth, next) })
		private.Get("/auth/session", a.session)
		private.With(auth.RequireCSRF).Post("/auth/confirm-password", a.confirmPassword)
		private.With(auth.RequireCSRF).Post("/auth/logout", a.logout)
		private.Get("/live", a.live)
		private.Get("/live/events", a.events)
		private.Get("/history", a.history)
		private.Get("/history.csv", a.csv)
		private.Get("/insights", a.insights)
		private.Get("/alerts", a.alerts)
		private.Get("/settings", a.getSettings)
		private.With(auth.RequireCSRF).Put("/settings", a.putSettings)
		private.Get("/data/backup", a.backup)
		private.Get("/finance/summary", a.financeSummary)
		private.Get("/finance/cycles", a.financeCycles)
		private.With(auth.RequireCSRF).Post("/finance/cycles", a.createFinanceCycle)
		private.Get("/finance/tariff-proposals", a.tariffProposals)
		private.With(auth.RequireCSRF).Post("/finance/tariff-proposals/{id}/approve", a.approveTariffProposal)
		r.Mount("/api/v1", private)
	}
	return r
}

func requestMetadata(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			var raw [16]byte
			if _, err := rand.Read(raw[:]); err == nil {
				id = hex.EncodeToString(raw[:])
			} else {
				id = "unavailable"
			}
		}
		w.Header().Set("X-Request-ID", id)
		if len(r.URL.Path) >= len("/api/") && r.URL.Path[:len("/api/")] == "/api/" {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
