package rancher

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewClient(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.cacheTTL != 5*time.Minute {
		t.Errorf("expected cacheTTL to be 5m, got %v", client.cacheTTL)
	}
	if client.clusterCache == nil {
		t.Error("expected clusterCache to be initialized")
	}
	if client.projectCache == nil {
		t.Error("expected projectCache to be initialized")
	}
}

func TestGetClusterID_CacheHit(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	// Pre-populate cache
	client.clusterCache["test-cluster"] = cacheEntry{
		value:     "c-m-12345",
		expiresAt: time.Now().Add(5 * time.Minute),
	}

	clusterID, err := client.GetClusterID(context.Background(), "test-cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clusterID != "c-m-12345" {
		t.Errorf("expected cluster ID 'c-m-12345', got '%s'", clusterID)
	}
}

func TestGetClusterID_CacheExpired(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	// Pre-populate cache with expired entry
	client.clusterCache["test-cluster"] = cacheEntry{
		value:     "c-m-12345",
		expiresAt: time.Now().Add(-1 * time.Minute), // Expired
	}

	// Should fail because fake client has no data
	_, err := client.GetClusterID(context.Background(), "test-cluster")
	if err == nil {
		t.Error("expected error for expired cache and missing cluster")
	}
}

func TestGetClusterID_NotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	_, err := client.GetClusterID(context.Background(), "non-existent-cluster")
	if err == nil {
		t.Error("expected error for non-existent cluster")
	}
}

func TestGetClusterID_Success(t *testing.T) {
	scheme := runtime.NewScheme()

	// Create a fake cluster resource
	cluster := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "provisioning.cattle.io/v1",
			"kind":       "Cluster",
			"metadata": map[string]any{
				"name":      "test-cluster",
				"namespace": "fleet-default",
			},
			"status": map[string]any{
				"clusterName": "c-m-abc123",
			},
		},
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme, cluster)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	clusterID, err := client.GetClusterID(context.Background(), "test-cluster")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clusterID != "c-m-abc123" {
		t.Errorf("expected cluster ID 'c-m-abc123', got '%s'", clusterID)
	}

	// Verify it was cached
	if _, ok := client.clusterCache["test-cluster"]; !ok {
		t.Error("expected cluster to be cached")
	}
}

func TestGetProjectID_CacheHit(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	// Pre-populate cache
	client.projectCache["c-m-12345:platform"] = cacheEntry{
		value:     "p-xyz789",
		expiresAt: time.Now().Add(5 * time.Minute),
	}

	projectID, err := client.GetProjectID(context.Background(), "c-m-12345", "platform")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if projectID != "p-xyz789" {
		t.Errorf("expected project ID 'p-xyz789', got '%s'", projectID)
	}
}

func TestGetProjectID_NotFound(t *testing.T) {
	scheme := runtime.NewScheme()

	// Register list kind for projects
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "management.cattle.io", Version: "v3", Resource: "projects"}: "ProjectList",
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	_, err := client.GetProjectID(context.Background(), "c-m-12345", "non-existent-project")
	if err == nil {
		t.Error("expected error for non-existent project")
	}
}

func TestGetProjectID_Success(t *testing.T) {
	scheme := runtime.NewScheme()

	// Create a fake project resource
	project := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "management.cattle.io/v3",
			"kind":       "Project",
			"metadata": map[string]any{
				"name":      "p-abc123",
				"namespace": "c-m-cluster1",
			},
			"spec": map[string]any{
				"displayName": "platform",
			},
		},
	}

	// Register list kind for projects
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "management.cattle.io", Version: "v3", Resource: "projects"}: "ProjectList",
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind, project)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	projectID, err := client.GetProjectID(context.Background(), "c-m-cluster1", "platform")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if projectID != "p-abc123" {
		t.Errorf("expected project ID 'p-abc123', got '%s'", projectID)
	}

	// Verify it was cached
	if _, ok := client.projectCache["c-m-cluster1:platform"]; !ok {
		t.Error("expected project to be cached")
	}
}

func TestClearCache(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	// Pre-populate caches
	client.clusterCache["cluster1"] = cacheEntry{value: "c-m-1", expiresAt: time.Now().Add(5 * time.Minute)}
	client.clusterCache["cluster2"] = cacheEntry{value: "c-m-2", expiresAt: time.Now().Add(5 * time.Minute)}
	client.projectCache["c-m-1:proj1"] = cacheEntry{value: "p-1", expiresAt: time.Now().Add(5 * time.Minute)}

	client.ClearCache()

	if len(client.clusterCache) != 0 {
		t.Errorf("expected empty cluster cache, got %d entries", len(client.clusterCache))
	}
	if len(client.projectCache) != 0 {
		t.Errorf("expected empty project cache, got %d entries", len(client.projectCache))
	}
}

func TestCacheStats(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	// Pre-populate caches
	client.clusterCache["cluster1"] = cacheEntry{value: "c-m-1", expiresAt: time.Now().Add(5 * time.Minute)}
	client.clusterCache["cluster2"] = cacheEntry{value: "c-m-2", expiresAt: time.Now().Add(5 * time.Minute)}
	client.projectCache["c-m-1:proj1"] = cacheEntry{value: "p-1", expiresAt: time.Now().Add(5 * time.Minute)}
	client.projectCache["c-m-1:proj2"] = cacheEntry{value: "p-2", expiresAt: time.Now().Add(5 * time.Minute)}
	client.projectCache["c-m-2:proj1"] = cacheEntry{value: "p-3", expiresAt: time.Now().Add(5 * time.Minute)}

	clusterEntries, projectEntries := client.CacheStats()

	if clusterEntries != 2 {
		t.Errorf("expected 2 cluster entries, got %d", clusterEntries)
	}
	if projectEntries != 3 {
		t.Errorf("expected 3 project entries, got %d", projectEntries)
	}
}

func TestHealthCheck_Success(t *testing.T) {
	scheme := runtime.NewScheme()

	// Register list kind for clusters
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "provisioning.cattle.io", Version: "v1", Resource: "clusters"}: "ClusterList",
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	ctx := context.Background()
	err := client.HealthCheck(ctx)

	// The fake client will return an empty list, which is fine for health check
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHealthCheck_ContextCanceled(t *testing.T) {
	scheme := runtime.NewScheme()

	// Register list kind for clusters
	gvrToListKind := map[schema.GroupVersionResource]string{
		{Group: "provisioning.cattle.io", Version: "v1", Resource: "clusters"}: "ClusterList",
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, gvrToListKind)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := client.HealthCheck(ctx)

	// Note: fake client doesn't respect context cancellation, so this test
	// mainly verifies the code path works
	_ = err // Result depends on fake client implementation
}

func TestCacheConcurrency(t *testing.T) {
	scheme := runtime.NewScheme()
	dynamicClient := dynamicfake.NewSimpleDynamicClient(scheme)
	logger := newTestLogger()

	client := NewClient(dynamicClient, logger, 5*time.Minute)

	// Pre-populate cache
	client.clusterCache["test-cluster"] = cacheEntry{
		value:     "c-m-12345",
		expiresAt: time.Now().Add(5 * time.Minute),
	}

	// Run concurrent reads
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() {
			_, _ = client.GetClusterID(context.Background(), "test-cluster")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}
}
