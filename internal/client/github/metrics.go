package github

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// rateLimitHitsTotal counts GitHub 429 responses so rate-limit pressure is
// visible rather than hidden inside retry/backoff.
var rateLimitHitsTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "github_rate_limit_hits_total",
	Help: "Total number of GitHub API responses that returned 429 Too Many Requests.",
})
