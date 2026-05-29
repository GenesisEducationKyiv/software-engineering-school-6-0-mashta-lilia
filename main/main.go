package main

import (
	"context"
	"flag"
	"fmt"
	"github-release-notifier/internal/app"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/platform/logger"
	"os"
	"os/signal"
	"syscall"
)

// Populated via -ldflags "-X main.commit=... -X main.buildTime=...".
var (
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	// Wrap so `defer stop()` runs before os.Exit (gocritic: exitAfterDefer).
	os.Exit(run())
}

func run() int {
	showVersion := flag.Bool("version", false, "print build info and exit")
	flag.Parse()
	if *showVersion {
		if _, err := fmt.Fprintf(os.Stdout, "commit=%s build_time=%s\n", commit, buildTime); err != nil {
			return 1
		}
		return 0
	}

	cfg, err := config.NewFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	l := logger.New(cfg.LogLevel)
	l.Info("Starting", "commit", commit, "build_time", buildTime)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := app.New(cfg, l).Run(ctx); err != nil {
		l.Error("App stopped unexpectedly", "err", fmt.Errorf("app: run: %w", err))
		return 1
	}
	return 0
}
