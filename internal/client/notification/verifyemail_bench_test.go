package notification_test

import (
	"context"
	notificationclient "github-release-notifier/internal/client/notification"
	notificationv1 "github-release-notifier/internal/gen/notification/v1"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification"
	"github-release-notifier/services/notification/grpcserver"
	"github-release-notifier/services/notification/resthttp"
	"net"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
)

// noopSender isolates transport cost: it returns immediately without touching SMTP.
type noopSender struct{}

func (noopSender) SendConfirmation(context.Context, notification.Confirmation) (bool, error) {
	return true, nil
}

func (noopSender) SendReleaseNotification(
	context.Context, string, string, *notification.ReleaseInfo,
) (bool, error) {
	return true, nil
}

// BenchmarkVerifyEmail_GRPC and _REST drive the same operation over each
// transport against an in-process server, so the delta is the transport cost
// (HTTP/2 + protobuf vs HTTP/1.1 + JSON). Run: go test -run=^$ -bench=VerifyEmail.
func BenchmarkVerifyEmail_GRPC(b *testing.B) {
	srv, err := grpcserver.New(noopSender{}, logger.Nop())
	if err != nil {
		b.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	grpcSrv := grpc.NewServer()
	notificationv1.RegisterNotificationServiceServer(grpcSrv, srv)
	go func() { _ = grpcSrv.Serve(lis) }()
	defer grpcSrv.Stop()

	conn, client, err := notificationclient.Dial(lis.Addr().String(), logger.Nop())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // benchmark cleanup

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.VerifyEmail(context.Background(), "a@b.c", "https://x/confirm/tok", "o/r"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyEmail_REST(b *testing.B) {
	h, err := resthttp.NewHandler(noopSender{}, logger.Nop())
	if err != nil {
		b.Fatal(err)
	}
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	client, err := notificationclient.NewRESTClient(ts.URL, logger.Nop())
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.VerifyEmail(context.Background(), "a@b.c", "https://x/confirm/tok", "o/r"); err != nil {
			b.Fatal(err)
		}
	}
}

// The _Parallel variants measure throughput under concurrent callers, where
// gRPC multiplexes RPCs over one HTTP/2 connection while REST spreads load
// across a pooled set of HTTP/1.1 connections.
func BenchmarkVerifyEmail_GRPC_Parallel(b *testing.B) {
	srv, err := grpcserver.New(noopSender{}, logger.Nop())
	if err != nil {
		b.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	grpcSrv := grpc.NewServer()
	notificationv1.RegisterNotificationServiceServer(grpcSrv, srv)
	go func() { _ = grpcSrv.Serve(lis) }()
	defer grpcSrv.Stop()

	conn, client, err := notificationclient.Dial(lis.Addr().String(), logger.Nop())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close() //nolint:errcheck // benchmark cleanup

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := client.VerifyEmail(context.Background(), "a@b.c", "https://x/confirm/tok", "o/r"); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkVerifyEmail_REST_Parallel(b *testing.B) {
	h, err := resthttp.NewHandler(noopSender{}, logger.Nop())
	if err != nil {
		b.Fatal(err)
	}
	ts := httptest.NewServer(h.Routes())
	defer ts.Close()

	client, err := notificationclient.NewRESTClient(ts.URL, logger.Nop())
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := client.VerifyEmail(context.Background(), "a@b.c", "https://x/confirm/tok", "o/r"); err != nil {
				b.Fatal(err)
			}
		}
	})
}
