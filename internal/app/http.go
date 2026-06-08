package app

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/config"
	"github-release-notifier/internal/platform/logger"
	"net/http"
	"time"
)

const (
	httpReadTimeout  = 10 * time.Second
	httpWriteTimeout = 10 * time.Second
	httpIdleTimeout  = 60 * time.Second
	shutdownTimeout  = 10 * time.Second
)

func runHTTPServer(ctx context.Context, cfg *config.Config, deps *dependencies, log *logger.Logger) error {
	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      deps.router,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
		IdleTimeout:  httpIdleTimeout,
	}

	pollerCtx, cancelPoller := context.WithCancel(ctx)
	defer cancelPoller()
	// Deferred so server-error exits also stop the rate-limiter goroutine.
	defer deps.subscribeLimiter.Stop()

	go deps.poller.Start(pollerCtx)

	serverErr := make(chan error, 1)
	go func() {
		log.Info(ctx, "server_starting", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		cancelPoller()
		waitForPoller(deps, log)
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
	}

	log.Info(ctx, "server_shutting_down")
	cancelPoller()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	// Drain poller regardless of Shutdown outcome to avoid mid-scan SMTP/Postgres leaks.
	shutdownErr := srv.Shutdown(shutdownCtx)
	waitForPollerWithContext(shutdownCtx, deps, log)
	if shutdownErr != nil {
		return fmt.Errorf("server shutdown: %w", shutdownErr)
	}
	log.Info(ctx, "server_stopped")
	return nil
}

func waitForPoller(deps *dependencies, log *logger.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	waitForPollerWithContext(ctx, deps, log)
}

func waitForPollerWithContext(ctx context.Context, deps *dependencies, log *logger.Logger) {
	select {
	case <-deps.poller.Done():
	case <-ctx.Done():
		log.Warn(ctx, "poller_shutdown_timeout")
	}
}
