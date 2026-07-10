package release

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Poller business metrics, exposed on the app's /metrics endpoint. The background
// scan is the core of the service, so these make its work visible in Grafana.
var (
	scanCyclesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poller_scan_cycles_total",
		Help: "Total number of poller scan cycles that have run to completion.",
	})

	scanDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "poller_scan_duration_seconds",
		Help:    "Duration of a full poller scan cycle in seconds.",
		Buckets: prometheus.DefBuckets,
	})

	releasesDetectedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "poller_releases_detected_total",
		Help: "Total number of new releases detected across all tracked repositories.",
	})
)
