package app

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification/config"
	"github-release-notifier/services/notification/inbound/grpcserver"
	"net"
	"time"

	notificationv1 "github-release-notifier/internal/gen/notification/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const shutdownTimeout = 10 * time.Second

func runGRPCServer(
	ctx context.Context, cfg *config.Config, deps *dependencies, log *logger.Logger,
) error {
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen grpc addr=%s: %w", cfg.GRPCAddr, err)
	}

	server := grpc.NewServer(grpc.UnaryInterceptor(grpcserver.TraceUnaryServerInterceptor()))
	notificationv1.RegisterNotificationServiceServer(server, deps.notificationServer)
	// Health service lets the compose healthcheck verify the listener is serving.
	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(server, healthSrv)

	serverErr := make(chan error, 1)
	go func() {
		log.Info(ctx, "grpc_server_starting", "addr", cfg.GRPCAddr)
		if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		server.Stop()
		return fmt.Errorf("grpc server error: %w", err)
	case <-ctx.Done():
	}

	log.Info(ctx, "grpc_server_shutting_down")
	healthSrv.Shutdown()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-shutdownCtx.Done():
		log.Warn(shutdownCtx, "grpc_server_shutdown_timeout")
		server.Stop()
		<-stopped
	}

	log.Info(ctx, "grpc_server_stopped")
	return nil
}
