package main

import (
	"context"
	"fmt"
	"github-release-notifier/internal/app"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/platform/logger"
	"os"
)

func main() {
	cfg, err := config.NewFromEnv()
	if err != nil {
		panic(fmt.Errorf("config: %w", err))
	}
	l := logger.New(cfg.LogLevel)

	if err := app.New(cfg, l).Run(context.Background()); err != nil {
		l.Error("App stopped unexpectedly", "err", fmt.Errorf("app: run: %w", err))
		os.Exit(1)
	}
}
