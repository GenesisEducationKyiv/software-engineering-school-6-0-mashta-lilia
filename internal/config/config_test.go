package config_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github-release-notifier/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setBaseEnv populates the minimum env so NewFromEnv() succeeds. Tests
// then override individual keys to exercise specific code paths.
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("API_KEY", "test-api-key")
	// Make sure parsed values fall back to defaults unless test overrides them.
	for _, k := range []string{
		"SERVER_PORT", "DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE",
		"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASSWORD", "SMTP_FROM",
		"GITHUB_TOKEN", "SCAN_INTERVAL", "BASE_URL", "REDIS_ADDR", "REDIS_PASSWORD",
		"REDIS_DB", "REDIS_CACHE_TTL", "TRUSTED_PROXY", "LOG_LEVEL",
	} {
		t.Setenv(k, "")
	}
}

func TestNewFromEnv_DefaultsAreApplied(t *testing.T) {
	setBaseEnv(t)

	cfg, err := config.NewFromEnv()
	require.NoError(t, err)

	assert.Equal(t, "8080", cfg.ServerPort)
	assert.Equal(t, "localhost", cfg.DBHost)
	assert.Equal(t, "5432", cfg.DBPort)
	assert.Equal(t, "require", cfg.DBSSLMode)
	assert.Equal(t, 587, cfg.SMTPPort)
	assert.Equal(t, 5*time.Minute, cfg.ScanInterval)
	assert.Equal(t, 10*time.Minute, cfg.RedisCacheTTL)
	assert.False(t, cfg.TrustedProxy)
	assert.Equal(t, slog.LevelInfo, cfg.LogLevel)
	assert.Equal(t, "test-api-key", cfg.APIKey)
}

func TestNewFromEnv_RequiresAPIKey(t *testing.T) {
	t.Setenv("API_KEY", "")

	cfg, err := config.NewFromEnv()
	assert.Nil(t, cfg)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "api_key")
}

func TestNewFromEnv_RejectsUnparseableSMTPPort(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SMTP_PORT", "not-a-number")

	cfg, err := config.NewFromEnv()
	assert.Nil(t, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SMTP_PORT")
}

func TestNewFromEnv_RejectsUnparseableScanInterval(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SCAN_INTERVAL", "10minutos")

	_, err := config.NewFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SCAN_INTERVAL")
}

func TestNewFromEnv_RejectsUnparseableTrustedProxy(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("TRUSTED_PROXY", "kinda")

	_, err := config.NewFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TRUSTED_PROXY")
}

func TestNewFromEnv_RejectsUnknownLogLevel(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("LOG_LEVEL", "shout")

	_, err := config.NewFromEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOG_LEVEL")
}

func TestNewFromEnv_AcceptsAllLogLevels(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	for level, want := range cases {
		t.Run(level, func(t *testing.T) {
			setBaseEnv(t)
			t.Setenv("LOG_LEVEL", level)

			cfg, err := config.NewFromEnv()
			require.NoError(t, err)
			assert.Equal(t, want, cfg.LogLevel)
		})
	}
}

func TestConfig_DatabaseURL_EscapesUserAndPassword(t *testing.T) {
	cfg := &config.Config{
		DBUser:     "user@name",
		DBPassword: "p@ss:word",
		DBHost:     "localhost",
		DBPort:     "5432",
		DBName:     "release_notifier",
		DBSSLMode:  "disable",
	}

	url := cfg.DatabaseURL()
	// '@' and ':' inside user/password must be URL-encoded so the lib/pq
	// driver parses the URL unambiguously.
	assert.Contains(t, url, "user%40name")
	assert.Contains(t, url, "p%40ss%3Aword")
	assert.True(t, strings.HasSuffix(url, "@localhost:5432/release_notifier?sslmode=disable"),
		"got %s", url)
}
