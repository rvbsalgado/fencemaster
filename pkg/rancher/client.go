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

	// Cache miss - query API
	metrics.CacheMissesTotal.WithLabelValues(metrics.CacheTypeCluster).Inc()

	cluster, err := c.dynamicClient.Resource(clusterGVR).Namespace("fleet-default").Get(
		ctx,
		clusterName,
		metav1.GetOptions{},
	)
	if err != nil {
		metrics.ClusterLookupErrorsTotal.WithLabelValues(metrics.ErrorTypeAPI).Inc()
		return "", fmt.Errorf("failed to get cluster %s: %w", clusterName, err)
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

	// Cache miss - query API
	metrics.CacheMissesTotal.WithLabelValues(metrics.CacheTypeProject).Inc()

	projects, err := c.dynamicClient.Resource(projectGVR).Namespace(clusterID).List(
		ctx,
		metav1.ListOptions{},
	)
	if err != nil {
		metrics.ProjectLookupErrorsTotal.WithLabelValues(metrics.ErrorTypeAPI).Inc()
		return "", fmt.Errorf("failed to list projects in cluster %s: %w", clusterID, err)
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
	// Check if we can list clusters (verifies API connectivity and RBAC)
	_, err := c.dynamicClient.Resource(clusterGVR).Namespace("fleet-default").List(
		ctx,
		metav1.ListOptions{Limit: 1},
	)
	if err != nil {
		return fmt.Errorf("failed to access clusters.provisioning.cattle.io: %w", err)
	}

	return nil
}
