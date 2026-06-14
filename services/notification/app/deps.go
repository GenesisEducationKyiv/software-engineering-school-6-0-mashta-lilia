package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github-release-notifier/internal/platform/logger"
	"github-release-notifier/services/notification"
	"github-release-notifier/services/notification/config"
	"github-release-notifier/services/notification/grpcserver"
	"github-release-notifier/services/notification/smtp"
	"github-release-notifier/services/notification/store"

	notificationv1 "github-release-notifier/internal/gen/notification/v1"
)

type dependencies struct {
	notificationServer notificationv1.NotificationServiceServer
	closers            []func() error
}

func buildDependencies(
	ctx context.Context, cfg *config.Config, db *sql.DB, log *logger.Logger,
) (*dependencies, error) {
	ledger, err := store.NewWithContext(ctx, db, log.With("component", "notification_store"))
	if err != nil {
		return nil, fmt.Errorf("creating notification store: %w", err)
	}

	templates := smtp.NewTemplateBuilder(cfg.BaseURL)
	mail, err := smtp.NewSMTPMailer(
		cfg.SMTPHost, cfg.SMTPPort,
		cfg.SMTPUser, cfg.SMTPPassword,
		cfg.SMTPFrom, templates,
	)
	if err != nil {
		closeQuietly(ctx, log, "notification store", ledger.Close)
		return nil, fmt.Errorf("creating SMTP mailer: %w", err)
	}

	service, err := notification.NewService(mail, ledger, log.With("component", "notification_service"))
	if err != nil {
		closeQuietly(ctx, log, "notification store", ledger.Close)
		return nil, fmt.Errorf("creating notification service: %w", err)
	}

	return &dependencies{
		notificationServer: grpcserver.New(service, log.With("component", "notification_server")),
		closers:            []func() error{ledger.Close},
	}, nil
}

func (d *dependencies) Close() error {
	if d == nil {
		return nil
	}
	var err error
	for _, closeFn := range d.closers {
		err = errors.Join(err, closeFn())
	}
	return err
}
