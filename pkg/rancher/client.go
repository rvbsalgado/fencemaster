package rancher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rvbsalgado/fencemaster/pkg/metrics"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/apimachinery/pkg/api/errors"
)

const (
	// Retry configuration
	maxRetries     = 3
	initialBackoff = 100 * time.Millisecond
	maxBackoff     = 2 * time.Second
)

var (
	clusterGVR = schema.GroupVersionResource{
		Group:    "provisioning.cattle.io",
		Version:  "v1",
		Resource: "clusters",
	}

	projectGVR = schema.GroupVersionResource{
		Group:    "management.cattle.io",
		Version:  "v3",
		Resource: "projects",
	}
)

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

type Client struct {
	dynamicClient dynamic.Interface
	logger        *slog.Logger
	cacheTTL      time.Duration

	clusterCache map[string]cacheEntry
	clusterMu    sync.RWMutex

	// projectCache key is "clusterID:projectDisplayName"
	projectCache map[string]cacheEntry
	projectMu    sync.RWMutex
}

func NewClient(dynamicClient dynamic.Interface, logger *slog.Logger, cacheTTL time.Duration) *Client {
	c := &Client{
		dynamicClient: dynamicClient,
		logger:        logger,
		cacheTTL:      cacheTTL,
		clusterCache:  make(map[string]cacheEntry),
		projectCache:  make(map[string]cacheEntry),
	}

	// Start background goroutine to evict expired cache entries
	go c.startCacheEviction()

	return c
}

// startCacheEviction periodically removes expired entries from caches
func (c *Client) startCacheEviction() {
	ticker := time.NewTicker(c.cacheTTL)
	defer ticker.Stop()

	for range ticker.C {
		c.evictExpiredEntries()
	}
}

// evictExpiredEntries removes all expired entries from both caches
func (c *Client) evictExpiredEntries() {
	now := time.Now()

	c.clusterMu.Lock()
	for key, entry := range c.clusterCache {
		if now.After(entry.expiresAt) {
			delete(c.clusterCache, key)
		}
	}
	c.clusterMu.Unlock()

	c.projectMu.Lock()
	for key, entry := range c.projectCache {
		if now.After(entry.expiresAt) {
			delete(c.projectCache, key)
		}
	}
	c.projectMu.Unlock()
}

// isRetryableError returns true if the error is transient and should be retried
func isRetryableError(err error) bool {
	if errors.IsServerTimeout(err) || errors.IsTimeout(err) {
		return true
	}
	if errors.IsTooManyRequests(err) {
		return true
	}
	if errors.IsServiceUnavailable(err) {
		return true
	}
	if errors.IsInternalError(err) {
		return true
	}
	return false
}

// GetClusterID returns the management cluster ID (e.g., c-m-xxxxx) for a given cluster name
func (c *Client) GetClusterID(ctx context.Context, clusterName string) (string, error) {
	// Check cache first
	c.clusterMu.RLock()
	if entry, ok := c.clusterCache[clusterName]; ok && time.Now().Before(entry.expiresAt) {
		c.clusterMu.RUnlock()
		c.logger.Debug("Cluster ID cache hit",
			slog.String("cluster", clusterName),
			slog.String("cluster_id", entry.value),
		)
		metrics.CacheHitsTotal.WithLabelValues(metrics.CacheTypeCluster).Inc()
		return entry.value, nil
	}
	c.clusterMu.RUnlock()

	// Cache miss - query API with retries
	metrics.CacheMissesTotal.WithLabelValues(metrics.CacheTypeCluster).Inc()

	var cluster *unstructured.Unstructured
	var err error
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		cluster, err = c.dynamicClient.Resource(clusterGVR).Namespace("fleet-default").Get(
			ctx,
			clusterName,
			metav1.GetOptions{},
		)
		if err == nil {
			break
		}

		if !isRetryableError(err) || attempt == maxRetries {
			metrics.ClusterLookupErrorsTotal.WithLabelValues(metrics.ErrorTypeAPI).Inc()
			return "", fmt.Errorf("failed to get cluster %s: %w", clusterName, err)
		}

		c.logger.Debug("Retrying cluster lookup",
			slog.String("cluster", clusterName),
			slog.Int("attempt", attempt+1),
			slog.Duration("backoff", backoff),
			slog.String("error", err.Error()),
		)

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}

	clusterID, found, err := unstructured.NestedString(cluster.Object, "status", "clusterName")
	if err != nil {
		return "", fmt.Errorf("failed to get clusterName from status: %w", err)
	}
	if !found {
		metrics.ClusterLookupErrorsTotal.WithLabelValues(metrics.ErrorTypeNotFound).Inc()
		return "", fmt.Errorf("clusterName not found in cluster %s status", clusterName)
	}

	// Store in cache
	c.clusterMu.Lock()
	c.clusterCache[clusterName] = cacheEntry{
		value:     clusterID,
		expiresAt: time.Now().Add(c.cacheTTL),
	}
	c.clusterMu.Unlock()

	c.logger.Debug("Cluster ID cached",
		slog.String("cluster", clusterName),
		slog.String("cluster_id", clusterID),
		slog.Duration("ttl", c.cacheTTL),
	)

	return clusterID, nil
}

// GetProjectID returns the project ID (e.g., p-xxxxx) for a given project display name in a cluster
func (c *Client) GetProjectID(ctx context.Context, clusterID, projectDisplayName string) (string, error) {
	cacheKey := clusterID + ":" + projectDisplayName

	// Check cache first
	c.projectMu.RLock()
	if entry, ok := c.projectCache[cacheKey]; ok && time.Now().Before(entry.expiresAt) {
		c.projectMu.RUnlock()
		c.logger.Debug("Project ID cache hit",
			slog.String("cluster_id", clusterID),
			slog.String("project", projectDisplayName),
			slog.String("project_id", entry.value),
		)
		metrics.CacheHitsTotal.WithLabelValues(metrics.CacheTypeProject).Inc()
		return entry.value, nil
	}
	c.projectMu.RUnlock()

	// Cache miss - query API with retries
	metrics.CacheMissesTotal.WithLabelValues(metrics.CacheTypeProject).Inc()

	var projects *unstructured.UnstructuredList
	var err error
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		projects, err = c.dynamicClient.Resource(projectGVR).Namespace(clusterID).List(
			ctx,
			metav1.ListOptions{},
		)
		if err == nil {
			break
		}

		if !isRetryableError(err) || attempt == maxRetries {
			metrics.ProjectLookupErrorsTotal.WithLabelValues(metrics.ErrorTypeAPI).Inc()
			return "", fmt.Errorf("failed to list projects in cluster %s: %w", clusterID, err)
		}

		c.logger.Debug("Retrying project lookup",
			slog.String("cluster_id", clusterID),
			slog.String("project", projectDisplayName),
			slog.Int("attempt", attempt+1),
			slog.Duration("backoff", backoff),
			slog.String("error", err.Error()),
		)

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}

	for _, project := range projects.Items {
		displayName, found, err := unstructured.NestedString(project.Object, "spec", "displayName")
		if err != nil {
			continue
		}
		if found && displayName == projectDisplayName {
			projectID := project.GetName()

			// Store in cache
			c.projectMu.Lock()
			c.projectCache[cacheKey] = cacheEntry{
				value:     projectID,
				expiresAt: time.Now().Add(c.cacheTTL),
			}
			c.projectMu.Unlock()

			c.logger.Debug("Project ID cached",
				slog.String("cluster_id", clusterID),
				slog.String("project", projectDisplayName),
				slog.String("project_id", projectID),
				slog.Duration("ttl", c.cacheTTL),
			)

			return projectID, nil
		}
	}

	metrics.ProjectLookupErrorsTotal.WithLabelValues(metrics.ErrorTypeNotFound).Inc()
	return "", fmt.Errorf("project %s not found in cluster %s", projectDisplayName, clusterID)
}

// ClearCache clears all cached entries
func (c *Client) ClearCache() {
	c.clusterMu.Lock()
	c.clusterCache = make(map[string]cacheEntry)
	c.clusterMu.Unlock()

	c.projectMu.Lock()
	c.projectCache = make(map[string]cacheEntry)
	c.projectMu.Unlock()

	c.logger.Info("Cache cleared")
}

// CacheStats returns the current cache statistics
func (c *Client) CacheStats() (clusterEntries, projectEntries int) {
	c.clusterMu.RLock()
	clusterEntries = len(c.clusterCache)
	c.clusterMu.RUnlock()

	c.projectMu.RLock()
	projectEntries = len(c.projectCache)
	c.projectMu.RUnlock()

	return
}

// HealthCheck verifies connectivity to the Kubernetes API and access to Rancher CRDs
func (c *Client) HealthCheck(ctx context.Context) error {
	// Check if we can list clusters (verifies API connectivity and RBAC for clusters.provisioning.cattle.io)
	if err := c.healthCheckResource(ctx, clusterGVR, "fleet-default", "clusters.provisioning.cattle.io"); err != nil {
		return err
	}

	// Check if we can access projects API (verifies RBAC for projects.management.cattle.io)
	// We use "local" cluster namespace as it always exists in Rancher
	if err := c.healthCheckResource(ctx, projectGVR, "local", "projects.management.cattle.io"); err != nil {
		return err
	}

	return nil
}

// healthCheckResource verifies access to a specific Kubernetes resource with retries
func (c *Client) healthCheckResource(ctx context.Context, gvr schema.GroupVersionResource, namespace, resourceName string) error {
	var err error
	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		_, err = c.dynamicClient.Resource(gvr).Namespace(namespace).List(
			ctx,
			metav1.ListOptions{Limit: 1},
		)
		if err == nil {
			return nil
		}

		if !isRetryableError(err) || attempt == maxRetries {
			return fmt.Errorf("failed to access %s: %w", resourceName, err)
		}

		c.logger.Debug("Retrying health check",
			slog.String("resource", resourceName),
			slog.Int("attempt", attempt+1),
			slog.Duration("backoff", backoff),
			slog.String("error", err.Error()),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}

	return nil
}
