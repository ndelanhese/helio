package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/ndelanhese/helio/internal/app"
	"github.com/ndelanhese/helio/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := app.New(config.Load()).Run(ctx); err != nil {
		log.Fatal(err)
	}
}
