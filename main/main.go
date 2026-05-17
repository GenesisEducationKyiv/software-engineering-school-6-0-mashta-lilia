package main

import (
	"context"
	"fmt"
	"github-release-notifier/internal/app"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/platform/logger"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg, err := config.NewFromEnv()
	if err != nil {
		panic(fmt.Errorf("config: %w", err))
	}
	l := logger.New(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.New(cfg, l).Run(ctx); err != nil {
		l.Error("App stopped unexpectedly", "err", fmt.Errorf("app: run: %w", err))
		os.Exit(1)
	}
}
