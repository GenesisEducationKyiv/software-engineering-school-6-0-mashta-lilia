package middleware

import "github-release-notifier/internal/platform/logger"

func optionalLogger(logs ...logger.Logger) logger.Logger {
	if len(logs) > 0 && logs[0] != nil {
		return logs[0]
	}
	return logger.Nop()
}
