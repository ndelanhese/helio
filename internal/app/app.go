package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ndelanhese/helio/internal/config"
	"github.com/ndelanhese/helio/internal/httpserver"
)

type App struct{ server *http.Server }

func New(cfg config.Config) *App {
	return &App{server: &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpserver.New(httpserver.Dependencies{}),
		ReadHeaderTimeout: 5 * time.Second,
	}}
}

func (a *App) Run(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() { errc <- a.server.ListenAndServe() }()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return a.server.Shutdown(shutdown)
	}
}
