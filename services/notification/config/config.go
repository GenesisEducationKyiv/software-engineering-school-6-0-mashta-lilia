package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	defaultGRPCAddr    = ":50051"
	defaultSMTPPort    = 587
	defaultServiceName = "github-release-notifier-notification"
)

type Config struct {
	GRPCAddr string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string

	BaseURL string

	LogLevel    string
	ServiceName string
}

func (c *Config) DatabaseURL() string {
	userInfo := url.UserPassword(c.DBUser, c.DBPassword)
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=%s",
		userInfo.String(), c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

func NewFromEnv() (*Config, error) {
	smtpPort, err := envInt("SMTP_PORT", defaultSMTPPort)
	if err != nil {
		return nil, err
	}
	logLevel, err := parseLogLevel(envOrDefault("LOG_LEVEL", "info"))
	if err != nil {
		return nil, err
	}
	dbPassword, err := envRequired("DB_PASSWORD")
	if err != nil {
		return nil, err
	}

	return &Config{
		GRPCAddr: envOrDefault("GRPC_ADDR", defaultGRPCAddr),

		DBHost:     envOrDefault("DB_HOST", "localhost"),
		DBPort:     envOrDefault("DB_PORT", "5432"),
		DBUser:     envOrDefault("DB_USER", "postgres"),
		DBPassword: dbPassword,
		DBName:     envOrDefault("DB_NAME", "notification"),
		DBSSLMode:  envOrDefault("DB_SSLMODE", "require"),

		SMTPHost:     envOrDefault("SMTP_HOST", "localhost"),
		SMTPPort:     smtpPort,
		SMTPUser:     envOrDefault("SMTP_USER", ""),
		SMTPPassword: envOrDefault("SMTP_PASSWORD", ""),
		SMTPFrom:     envOrDefault("SMTP_FROM", "noreply@example.com"),

		BaseURL: envOrDefault("BASE_URL", "http://localhost:8080"),

		LogLevel:    logLevel,
		ServiceName: envOrDefault("SERVICE_NAME", defaultServiceName),
	}, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envRequired(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("env %s must be set", key)
}

func envInt(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("env %s: %w", key, err)
	}
	return n, nil
}

func parseLogLevel(raw string) (string, error) {
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
