package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRequestsTotal(t *testing.T) {
	// Reset the counter for testing
	RequestsTotal.Reset()

	// Increment the counter
	RequestsTotal.WithLabelValues("CREATE", StatusMutated).Inc()
	RequestsTotal.WithLabelValues("CREATE", StatusSkipped).Inc()
	RequestsTotal.WithLabelValues("CREATE", StatusSkipped).Inc()
	RequestsTotal.WithLabelValues("UPDATE", StatusMutated).Inc()

	// Verify counts
	if got := testutil.ToFloat64(RequestsTotal.WithLabelValues("CREATE", StatusMutated)); got != 1 {
		t.Errorf("expected CREATE/mutated count of 1, got %f", got)
	}
	if got := testutil.ToFloat64(RequestsTotal.WithLabelValues("CREATE", StatusSkipped)); got != 2 {
		t.Errorf("expected CREATE/skipped count of 2, got %f", got)
	}
	if got := testutil.ToFloat64(RequestsTotal.WithLabelValues("UPDATE", StatusMutated)); got != 1 {
		t.Errorf("expected UPDATE/mutated count of 1, got %f", got)
	}
}

func TestRequestDuration(t *testing.T) {
	// Reset the histogram for testing
	RequestDuration.Reset()

	// Observe some values
	RequestDuration.WithLabelValues("CREATE").Observe(0.1)
	RequestDuration.WithLabelValues("CREATE").Observe(0.2)
	RequestDuration.WithLabelValues("UPDATE").Observe(0.05)

	// Verify the histogram has observations
	// We can't easily verify histogram values, but we can check the metric exists
	count := testutil.CollectAndCount(RequestDuration)
	if count == 0 {
		t.Error("expected RequestDuration to have metrics")
	}
}

func TestCacheHitsTotal(t *testing.T) {
	// Reset the counter for testing
	CacheHitsTotal.Reset()

	// Increment cache hits
	CacheHitsTotal.WithLabelValues(CacheTypeCluster).Inc()
	CacheHitsTotal.WithLabelValues(CacheTypeCluster).Inc()
	CacheHitsTotal.WithLabelValues(CacheTypeProject).Inc()

	// Verify counts
	if got := testutil.ToFloat64(CacheHitsTotal.WithLabelValues(CacheTypeCluster)); got != 2 {
		t.Errorf("expected cluster cache hits of 2, got %f", got)
	}
	if got := testutil.ToFloat64(CacheHitsTotal.WithLabelValues(CacheTypeProject)); got != 1 {
		t.Errorf("expected project cache hits of 1, got %f", got)
	}
}

func TestCacheMissesTotal(t *testing.T) {
	// Reset the counter for testing
	CacheMissesTotal.Reset()

	// Increment cache misses
	CacheMissesTotal.WithLabelValues(CacheTypeCluster).Inc()
	CacheMissesTotal.WithLabelValues(CacheTypeProject).Inc()
	CacheMissesTotal.WithLabelValues(CacheTypeProject).Inc()
	CacheMissesTotal.WithLabelValues(CacheTypeProject).Inc()

	// Verify counts
	if got := testutil.ToFloat64(CacheMissesTotal.WithLabelValues(CacheTypeCluster)); got != 1 {
		t.Errorf("expected cluster cache misses of 1, got %f", got)
	}
	if got := testutil.ToFloat64(CacheMissesTotal.WithLabelValues(CacheTypeProject)); got != 3 {
		t.Errorf("expected project cache misses of 3, got %f", got)
	}
}

func TestProjectLookupErrorsTotal(t *testing.T) {
	// Reset the counter for testing
	ProjectLookupErrorsTotal.Reset()

	// Increment errors
	ProjectLookupErrorsTotal.WithLabelValues(ErrorTypeNotFound).Inc()
	ProjectLookupErrorsTotal.WithLabelValues(ErrorTypeNotFound).Inc()
	ProjectLookupErrorsTotal.WithLabelValues(ErrorTypeAPI).Inc()

	// Verify counts
	if got := testutil.ToFloat64(ProjectLookupErrorsTotal.WithLabelValues(ErrorTypeNotFound)); got != 2 {
		t.Errorf("expected not_found errors of 2, got %f", got)
	}
	if got := testutil.ToFloat64(ProjectLookupErrorsTotal.WithLabelValues(ErrorTypeAPI)); got != 1 {
		t.Errorf("expected api_error errors of 1, got %f", got)
	}
}

func TestClusterLookupErrorsTotal(t *testing.T) {
	// Reset the counter for testing
	ClusterLookupErrorsTotal.Reset()

	// Increment errors
	ClusterLookupErrorsTotal.WithLabelValues(ErrorTypeNotFound).Inc()
	ClusterLookupErrorsTotal.WithLabelValues(ErrorTypeAPI).Inc()
	ClusterLookupErrorsTotal.WithLabelValues(ErrorTypeAPI).Inc()

	// Verify counts
	if got := testutil.ToFloat64(ClusterLookupErrorsTotal.WithLabelValues(ErrorTypeNotFound)); got != 1 {
		t.Errorf("expected not_found errors of 1, got %f", got)
	}
	if got := testutil.ToFloat64(ClusterLookupErrorsTotal.WithLabelValues(ErrorTypeAPI)); got != 2 {
		t.Errorf("expected api_error errors of 2, got %f", got)
	}
}

func TestMetricsAreRegistered(t *testing.T) {
	// Verify that all metrics are registered with the default registry
	metrics := []prometheus.Collector{
		RequestsTotal,
		RequestDuration,
		CacheHitsTotal,
		CacheMissesTotal,
		ProjectLookupErrorsTotal,
		ClusterLookupErrorsTotal,
	}

	for _, m := range metrics {
		// Collecting metrics shouldn't panic if properly registered
		ch := make(chan prometheus.Metric, 100)
		m.Collect(ch)
		close(ch)
	}
}

func TestStatusConstants(t *testing.T) {
	// Verify status constants are defined
	statuses := []string{
		StatusAllowed,
		StatusDenied,
		StatusError,
		StatusSkipped,
		StatusMutated,
		StatusDryRun,
	}

	for _, s := range statuses {
		if s == "" {
			t.Error("status constant should not be empty")
		}
	}
}

func TestCacheTypeConstants(t *testing.T) {
	if CacheTypeCluster != "cluster" {
		t.Errorf("expected CacheTypeCluster to be 'cluster', got '%s'", CacheTypeCluster)
	}
	if CacheTypeProject != "project" {
		t.Errorf("expected CacheTypeProject to be 'project', got '%s'", CacheTypeProject)
	}
}

func TestErrorTypeConstants(t *testing.T) {
	if ErrorTypeNotFound != "not_found" {
		t.Errorf("expected ErrorTypeNotFound to be 'not_found', got '%s'", ErrorTypeNotFound)
	}
	if ErrorTypeAPI != "api_error" {
		t.Errorf("expected ErrorTypeAPI to be 'api_error', got '%s'", ErrorTypeAPI)
	}
}
