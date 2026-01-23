package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestsTotal counts total webhook requests by operation and status
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fencemaster_requests_total",
			Help: "Total number of webhook requests",
		},
		[]string{"operation", "status"},
	)

	// RequestDuration measures request processing duration
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "fencemaster_request_duration_seconds",
			Help:    "Duration of webhook request processing in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)

	// CacheHitsTotal counts cache hits
	CacheHitsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fencemaster_cache_hits_total",
			Help: "Total number of cache hits",
		},
		[]string{"cache_type"},
	)

	// CacheMissesTotal counts cache misses
	CacheMissesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fencemaster_cache_misses_total",
			Help: "Total number of cache misses",
		},
		[]string{"cache_type"},
	)

	// ProjectLookupErrorsTotal counts project lookup errors
	ProjectLookupErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fencemaster_project_lookup_errors_total",
			Help: "Total number of project lookup errors",
		},
		[]string{"error_type"},
	)

	// ClusterLookupErrorsTotal counts cluster lookup errors
	ClusterLookupErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fencemaster_cluster_lookup_errors_total",
			Help: "Total number of cluster lookup errors",
		},
		[]string{"error_type"},
	)
)

// Status constants for request metrics
const (
	StatusAllowed  = "allowed"
	StatusDenied   = "denied"
	StatusError    = "error"
	StatusSkipped  = "skipped"
	StatusMutated  = "mutated"
	StatusDryRun   = "dry_run"
)

// CacheType constants
const (
	CacheTypeCluster = "cluster"
	CacheTypeProject = "project"
)

// ErrorType constants
const (
	ErrorTypeNotFound = "not_found"
	ErrorTypeAPI      = "api_error"
)
