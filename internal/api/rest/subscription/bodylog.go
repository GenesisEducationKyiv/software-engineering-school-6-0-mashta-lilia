package subscription

import "github-release-notifier/internal/platform/logger"

func bodyForLog(body map[string]any) map[string]any {
	redacted, ok := logger.Redact("", body).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return redacted
}
