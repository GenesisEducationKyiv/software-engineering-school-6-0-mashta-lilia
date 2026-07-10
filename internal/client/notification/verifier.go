package notification

import (
	"context"
	"fmt"
	"github-release-notifier/internal/platform/logger"
)

// Transport selects the synchronous notifier transport for email verification.
const (
	TransportGRPC = "grpc"
	TransportREST = "rest"
)

// Verifier sends a subscription confirmation ("verification") email
// synchronously. Both the gRPC Client and the RESTClient satisfy it, so the
// transport is swappable for the HW10 migration/comparison.
type Verifier interface {
	VerifyEmail(ctx context.Context, email, confirmURL, repo string) (bool, error)
}

// NewVerifier builds the verifier for the chosen transport: "grpc" dials the
// server at target (host:port), "rest" posts to the REST base URL. The returned
// closer releases the gRPC connection (a no-op for REST).
func NewVerifier( //nolint:ireturn // factory intentionally returns the transport-agnostic Verifier
	transport, target string, log *logger.Logger,
) (Verifier, func() error, error) {
	switch transport {
	case TransportGRPC:
		conn, client, err := Dial(target, log)
		if err != nil {
			return nil, nil, err
		}
		return client, conn.Close, nil
	case TransportREST:
		client, err := NewRESTClient(target, log)
		if err != nil {
			return nil, nil, err
		}
		return client, func() error { return nil }, nil
	default:
		return nil, nil, fmt.Errorf("notification verifier: unknown transport %q (want grpc|rest)", transport)
	}
}
