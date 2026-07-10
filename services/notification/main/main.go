package main

import (
	"context"
	"flag"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification/config"
	"os"
	"os/signal"
	"syscall"

	notificationapp "github-release-notifier/services/notification/app"
)

var (
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
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
	l := logger.New(logger.Config{Level: cfg.LogLevel, ServiceName: cfg.ServiceName})
	l.Info(context.Background(), "starting", "commit", commit, "build_time", buildTime)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := notificationapp.New(cfg, l).Run(ctx); err != nil {
		l.Error(ctx, "notification_app_stopped_unexpectedly", "err", fmt.Errorf("app: run: %w", err))
		return 1
	}
	return 0
}
