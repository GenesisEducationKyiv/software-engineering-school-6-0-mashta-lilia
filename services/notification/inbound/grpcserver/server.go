package grpcserver

import (
	"context"
	"github-release-notifier/internal/platform/tracectx"
	"github-release-notifier/services/notification/model"
	"strings"

	notificationv1 "github-release-notifier/internal/gen/notification/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type applicationService interface {
	SendConfirmation(ctx context.Context, confirmation model.Confirmation) (bool, error)
	SendReleaseNotification(ctx context.Context, email, repo string, rel *model.ReleaseInfo) (bool, error)
}

type Server struct {
	notificationv1.UnimplementedNotificationServiceServer
	service applicationService
}

func New(service applicationService) *Server {
	return &Server{service: service}
}

func (s *Server) SendConfirmation(
	ctx context.Context,
	req *notificationv1.SendConfirmationRequest,
) (*notificationv1.SendNotificationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetEmail() == "" || req.GetToken() == "" || req.GetRepo() == "" {
		return nil, status.Error(codes.InvalidArgument, "email, token and repo are required")
	}
	delivered, err := s.service.SendConfirmation(ctx, model.Confirmation{
		Email: req.GetEmail(),
		Token: req.GetToken(),
		Repo:  req.GetRepo(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "send confirmation: %v", err)
	}
	return &notificationv1.SendNotificationResponse{Delivered: delivered}, nil
}

func (s *Server) SendReleaseNotification(
	ctx context.Context,
	req *notificationv1.SendReleaseNotificationRequest,
) (*notificationv1.SendNotificationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if req.GetEmail() == "" || req.GetRepo() == "" {
		return nil, status.Error(codes.InvalidArgument, "email and repo are required")
	}
	delivered, err := s.service.SendReleaseNotification(
		ctx, req.GetEmail(), req.GetRepo(), releaseFromProto(req.GetRelease()),
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "send release notification: %v", err)
	}
	return &notificationv1.SendNotificationResponse{Delivered: delivered}, nil
}

func releaseFromProto(rel *notificationv1.Release) *model.ReleaseInfo {
	if rel == nil {
		return nil
	}
	return &model.ReleaseInfo{
		TagName:     rel.GetTagName(),
		Name:        rel.GetName(),
		HTMLURL:     rel.GetHtmlUrl(),
		PublishedAt: rel.GetPublishedAt(),
	}
}

func TraceUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if traceID := traceIDFromMetadata(ctx); traceID != "" {
			ctx = tracectx.WithTraceID(ctx, traceID)
		}
		return handler(ctx, req)
	}
}

func traceIDFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	if ids := md.Get("x-request-id"); len(ids) > 0 {
		return ids[0]
	}
	if parents := md.Get("traceparent"); len(parents) > 0 {
		return traceIDFromTraceparent(parents[0])
	}
	return ""
}

func traceIDFromTraceparent(value string) string {
	parts := strings.Split(value, "-")
	if len(parts) < 4 || len(parts[1]) != 32 {
		return ""
	}
	return parts[1]
}
