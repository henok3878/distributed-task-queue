package metrics

import (
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var Registry = prometheus.NewRegistry()

var (
	EnqueueTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dq_enqueue_total",
			Help: "Total enqueue requests by type/queue/status (ok|error).",
		},
		[]string{"type", "queue", "status"},
	)
	EnqueueLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dq_enqueue_latency_seconds",
			Help:    "Enqueue handler latency.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"type", "queue"},
	)
)

func MustRegisterAll() {
	Registry.MustRegister(
		EnqueueTotal,
		EnqueueLatency,
	)
}

// register /metrics for specific methods to avoid conflicts with "GET /" subtree.
func Expose(mux *http.ServeMux, pattern string) {
	h := promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
	mux.Handle(pattern, h)
}

func LabelOrUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

func ObserveDuration(start time.Time) float64 {
	return time.Since(start).Seconds()
}
