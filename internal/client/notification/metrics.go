package notification

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	kindConfirmation = "confirmation"
	kindRelease      = "release"

	outcomeSent    = "sent"
	outcomeDeduped = "deduped"
	outcomeFailed  = "failed"
)

// notificationRequestsTotal records the outcome of each outbound delivery request
// to the notifier. Email delivery is an outbound side effect, so its success/
// failure is tracked from the caller's vantage point.
var notificationRequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "notification_requests_total",
		Help: "Outbound notification requests by kind and outcome (sent|deduped|failed).",
	},
	[]string{"kind", "outcome"},
)
