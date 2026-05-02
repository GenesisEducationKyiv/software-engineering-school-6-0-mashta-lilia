package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
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
}

func (c *Config) DatabaseURL() string {
	userInfo := url.UserPassword(c.DBUser, c.DBPassword)
	return fmt.Sprintf("postgres://%s@%s:%s/%s?sslmode=%s",
		userInfo.String(), c.DBHost, c.DBPort, c.DBName, c.DBSSLMode)
}

func Load() *Config {
	return &Config{
		ServerPort: envOrDefault("SERVER_PORT", "8080"),
		DBHost:     envOrDefault("DB_HOST", "localhost"),
		DBPort:     envOrDefault("DB_PORT", "5432"),
		DBUser:     envOrDefault("DB_USER", "postgres"),
		DBPassword: envOrDefault("DB_PASSWORD", "postgres"),
		DBName:     envOrDefault("DB_NAME", "release_notifier"),
		DBSSLMode:  envOrDefault("DB_SSLMODE", "disable"),

		SMTPHost:     envOrDefault("SMTP_HOST", "localhost"),
		SMTPPort:     envOrDefaultInt("SMTP_PORT", 587),
		SMTPUser:     envOrDefault("SMTP_USER", ""),
		SMTPPassword: envOrDefault("SMTP_PASSWORD", ""),
		SMTPFrom:     envOrDefault("SMTP_FROM", "noreply@example.com"),

		GitHubToken: envOrDefault("GITHUB_TOKEN", ""),

		ScanInterval: envOrDefaultDuration("SCAN_INTERVAL", 5*time.Minute),
		BaseURL:      envOrDefault("BASE_URL", "http://localhost:8080"),

		APIKey: envOrDefault("API_KEY", ""),

		RedisAddr:     envOrDefault("REDIS_ADDR", "localhost:6379"),
		RedisPassword: envOrDefault("REDIS_PASSWORD", ""),
		RedisDB:       envOrDefaultInt("REDIS_DB", 0),
		RedisCacheTTL: envOrDefaultDuration("REDIS_CACHE_TTL", 10*time.Minute),

		TrustedProxy: envOrDefault("TRUSTED_PROXY", "false") == "true",
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envOrDefaultDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
