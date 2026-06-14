package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	GRPCAddr string `envconfig:"GRPC_ADDR" default:":50051"`

	DBHost     string `envconfig:"DB_HOST" default:"localhost"`
	DBPort     string `envconfig:"DB_PORT" default:"5432"`
	DBUser     string `envconfig:"DB_USER" default:"postgres"`
	DBPassword string `envconfig:"DB_PASSWORD" required:"true"`
	DBName     string `envconfig:"DB_NAME" default:"notification"`
	DBSSLMode  string `envconfig:"DB_SSLMODE" default:"require"`

	SMTPHost     string `envconfig:"SMTP_HOST" default:"localhost"`
	SMTPPort     int    `envconfig:"SMTP_PORT" default:"587"`
	SMTPUser     string `envconfig:"SMTP_USER"`
	SMTPPassword string `envconfig:"SMTP_PASSWORD"`
	SMTPFrom     string `envconfig:"SMTP_FROM" default:"noreply@example.com"`

	BaseURL string `envconfig:"BASE_URL" default:"http://localhost:8080"`

	LogLevel    string `envconfig:"LOG_LEVEL" default:"info"`
	ServiceName string `envconfig:"SERVICE_NAME" default:"github-release-notifier-notification"`
}

func (c *Config) DatabaseURL() string {
	userInfo := url.UserPassword(c.DBUser, c.DBPassword)
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=%s",
		userInfo.String(), c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

func NewFromEnv() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("loading notification config: %w", err)
	}
	level, err := normalizeLogLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}
	cfg.LogLevel = level
	return &cfg, nil
}

func normalizeLogLevel(raw string) (string, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return "debug", nil
	case "info":
		return "info", nil
	case "warn", "warning":
		return "warn", nil
	case "error":
		return "error", nil
	default:
		return "", fmt.Errorf("env LOG_LEVEL: unknown level %q (want debug|info|warn|error)", raw)
	}
}
