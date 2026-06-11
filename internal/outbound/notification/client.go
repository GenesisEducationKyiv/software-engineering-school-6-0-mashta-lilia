package notification

import (
	"context"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/internal/platform/tracectx"
	"github-release-notifier/internal/release"
	"time"

	notificationv1 "github-release-notifier/internal/gen/notification/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Bounds each RPC so a wedged notifier cannot park poller workers forever.
const callTimeout = 30 * time.Second

const traceIDHexLen = 32 // W3C trace-id hex length

type Client struct {
	client notificationv1.NotificationServiceClient
	log    *logger.Logger
}

func Dial(addr string, log *logger.Logger) (*grpc.ClientConn, *Client, error) {
	if addr == "" {
		return nil, nil, errors.New("notification client: addr is empty")
	}
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(TraceUnaryClientInterceptor()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("creating notification grpc client: %w", err)
	}
	client, err := NewClient(notificationv1.NewNotificationServiceClient(conn), log)
	if err != nil {
		return nil, nil, errors.Join(err, conn.Close())
	}
	return conn, client, nil
}

func NewClient(client notificationv1.NotificationServiceClient, log *logger.Logger) (*Client, error) {
	if client == nil {
		return nil, errors.New("notification client: grpc client is nil")
	}
	if log == nil {
		log = logger.Nop()
	}
	return &Client{client: client, log: log}, nil
}

func (c *Client) SendConfirmation(ctx context.Context, email, token, repo string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	resp, err := c.client.SendConfirmation(ctx, &notificationv1.SendConfirmationRequest{
		Email: email,
		Token: token,
		Repo:  repo,
	})
	if err != nil {
		return transportError("send confirmation", err)
	}
	if !resp.GetDelivered() {
		c.log.Info(ctx, "notification_deduped", "kind", "confirmation", "repo", repo)
	}
	return nil
}

func (c *Client) SendReleaseNotification(
	ctx context.Context, email, repo string, rel *release.Release,
) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	resp, err := c.client.SendReleaseNotification(ctx, &notificationv1.SendReleaseNotificationRequest{
		Email:   email,
		Repo:    repo,
		Release: releaseToProto(rel),
	})
	if err != nil {
		return transportError("send release notification", err)
	}
	if !resp.GetDelivered() {
		c.log.Info(ctx, "notification_deduped", "kind", "release", "repo", repo)
	}
	return nil
}

func TraceUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req any,
		reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		if traceID, ok := tracectx.FromContext(ctx); ok && traceID != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, "x-request-id", traceID)
			if len(traceID) == traceIDHexLen {
				ctx = metadata.AppendToOutgoingContext(
					ctx, "traceparent", "00-"+traceID+"-0000000000000000-01",
				)
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func releaseToProto(rel *release.Release) *notificationv1.Release {
	if rel == nil {
		return nil
	}
	return &notificationv1.Release{
		TagName:     rel.TagName,
		Name:        rel.Name,
		HtmlUrl:     rel.HTMLURL,
		PublishedAt: rel.PublishedAt,
	}
}

func transportError(action string, err error) error {
	if st, ok := status.FromError(err); ok {
		return fmt.Errorf("notification grpc %s failed code=%s: %w", action, st.Code(), err)
	}
	return fmt.Errorf("notification grpc %s failed: %w", action, err)
}
