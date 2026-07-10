package app

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification/config"
	"net"
	"net/http"
	"time"
)

const (
	restReadHeaderTimeout = 5 * time.Second
	restShutdownTimeout   = 10 * time.Second
)

// runRESTServer serves the notification REST API (the HTTP/JSON transport kept
// alongside gRPC) until ctx is canceled, then drains gracefully.
func runRESTServer(ctx context.Context, cfg *config.Config, deps *dependencies, log *logger.Logger) error {
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", cfg.RESTAddr)
	if err != nil {
		return fmt.Errorf("listen rest addr=%s: %w", cfg.RESTAddr, err)
	}
	srv := &http.Server{
		Handler:           deps.restHandler.Routes(),
		ReadHeaderTimeout: restReadHeaderTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info(ctx, "rest_server_starting", "addr", cfg.RESTAddr)
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return fmt.Errorf("rest server error: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), restShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("rest server shutdown: %w", err)
	}
	log.Info(ctx, "rest_server_stopped")
	return nil
}
