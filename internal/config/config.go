package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSMTPPort     = 587
	defaultScanInterval = 5 * time.Minute
	defaultCacheTTL     = 10 * time.Minute
)

type Config struct {
	ServerPort string
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

	GitHubToken string

	ScanInterval time.Duration
	BaseURL      string

	APIKey string

	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisCacheTTL time.Duration

	TrustedProxy bool

	LogLevel slog.Level
}

func (c *Config) DatabaseURL() string {
	userInfo := url.UserPassword(c.DBUser, c.DBPassword)
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=%s",
		userInfo.String(), c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

// NewFromEnv fails fast on a present-but-unparseable env var rather than
// falling back to the default — silent fallback hides misconfiguration.
func NewFromEnv() (*Config, error) {
	smtpPort, err := envInt("SMTP_PORT", defaultSMTPPort)
	if err != nil {
		return nil, err
	}
	redisDB, err := envInt("REDIS_DB", 0)
	if err != nil {
		return nil, err
	}
	scanInterval, err := envDuration("SCAN_INTERVAL", defaultScanInterval)
	if err != nil {
		return nil, err
	}
	cacheTTL, err := envDuration("REDIS_CACHE_TTL", defaultCacheTTL)
	if err != nil {
		return nil, err
	}
	logLevel, err := parseLogLevel(envOrDefault("LOG_LEVEL", "info"))
	if err != nil {
		return nil, err
	}
	trustedProxy, err := envBool("TRUSTED_PROXY", false)
	if err != nil {
		return nil, err
	}

	return &Config{
		ServerPort: envOrDefault("SERVER_PORT", "8080"),
		DBHost:     envOrDefault("DB_HOST", "localhost"),
		DBPort:     envOrDefault("DB_PORT", "5432"),
		DBUser:     envOrDefault("DB_USER", "postgres"),
		DBPassword: envOrDefault("DB_PASSWORD", "postgres"),
		DBName:     envOrDefault("DB_NAME", "release_notifier"),
		DBSSLMode:  envOrDefault("DB_SSLMODE", "require"),

		SMTPHost:     envOrDefault("SMTP_HOST", "localhost"),
		SMTPPort:     smtpPort,
		SMTPUser:     envOrDefault("SMTP_USER", ""),
		SMTPPassword: envOrDefault("SMTP_PASSWORD", ""),
		SMTPFrom:     envOrDefault("SMTP_FROM", "noreply@example.com"),

		GitHubToken: envOrDefault("GITHUB_TOKEN", ""),

		ScanInterval: scanInterval,
		BaseURL:      envOrDefault("BASE_URL", "http://localhost:8080"),

		APIKey: envOrDefault("API_KEY", ""),

		RedisAddr:     envOrDefault("REDIS_ADDR", "localhost:6379"),
		RedisPassword: envOrDefault("REDIS_PASSWORD", ""),
		RedisDB:       redisDB,
		RedisCacheTTL: cacheTTL,

		TrustedProxy: trustedProxy,

		LogLevel: logLevel,
	}, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("env %s: %w", key, err)
	}
	return d, nil
}

func envBool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("env %s: %w", key, err)
	}
	return b, nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("env LOG_LEVEL: unknown level %q (want debug|info|warn|error)", raw)
	}
}
