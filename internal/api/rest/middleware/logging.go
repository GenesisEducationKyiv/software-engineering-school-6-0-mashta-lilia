package middleware

import "github-release-notifier/internal/platform/logger"

//nolint:ireturn // Accepts injected logger or a no-op fallback.
func optionalLogger(logs ...logger.Logger) logger.Logger {
	return logger.Or(logs...)
}
