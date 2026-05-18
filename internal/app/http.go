package app

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/config"
	"log/slog"
	"net/http"
	"time"
)

const (
	httpReadTimeout  = 10 * time.Second
	httpWriteTimeout = 10 * time.Second
	httpIdleTimeout  = 60 * time.Second
	shutdownTimeout  = 10 * time.Second
)

func runHTTPServer(ctx context.Context, cfg *config.Config, deps *dependencies) error {
	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      deps.router,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
		IdleTimeout:  httpIdleTimeout,
	}

	pollerCtx, cancelPoller := context.WithCancel(ctx)
	defer cancelPoller()
	// Deferred so the rate-limiter goroutine is stopped on server-error
	// exits, not just the shutdown path.
	defer deps.subscribeLimiter.Stop()

	go deps.poller.Start(pollerCtx)

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("Server starting", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Block until the server fails or the parent context is canceled.
	// The parent owns signal handling via signal.NotifyContext in main.
	select {
	case err := <-serverErr:
		cancelPoller()
		waitForPoller(deps)
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
	}

	slog.Info("Shutting down")
	cancelPoller()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown: %w", err)
	}

	waitForPollerWithContext(shutdownCtx, deps)
	slog.Info("Server stopped")
	return nil
}

func waitForPoller(deps *dependencies) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	waitForPollerWithContext(ctx, deps)
}

func waitForPollerWithContext(ctx context.Context, deps *dependencies) {
	// Wait for the poller goroutine to drain its in-flight scan; otherwise
	// the process can exit while a Postgres write or SMTP send is in flight.
	select {
	case <-deps.poller.Done():
	case <-ctx.Done():
		slog.Warn("Poller did not stop within shutdown timeout")
	}
}
