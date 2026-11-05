// Package hubsync publishes Rancher project IDs from the management cluster to
// downstream clusters, where controller mode reads them from a ConfigMap.
//
// It watches projects on the hub and writes one Fleet Bundle per selected
// cluster. fleet-agent then applies the ConfigMap downstream, so nothing on
// the downstream cluster needs a Rancher credential.
package hubsync

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/rvbsalgado/fencemaster/pkg/metrics"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (
	// Operation is the "operation" label value on fencemaster_requests_total.
	Operation = "SYNC"

	// DefaultClusterSelector includes every cluster not opted out with
	// fencemaster/project-sync=false.
	DefaultClusterSelector = "fencemaster/project-sync!=false"

	fieldManager = "fencemaster"
	bundlePrefix = "fencemaster-project-ids-"
)

var (
	clusterGVR = schema.GroupVersionResource{Group: "provisioning.cattle.io", Version: "v1", Resource: "clusters"}
	projectGVR = schema.GroupVersionResource{Group: "management.cattle.io", Version: "v3", Resource: "projects"}
	bundleGVR  = schema.GroupVersionResource{Group: "fleet.cattle.io", Version: "v1alpha1", Resource: "bundles"}

	invalidKeyChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

type Config struct {
	DryRun bool
	// Label selector for the provisioning clusters to sync.
	ClusterSelector string
	// Namespace holding the provisioning clusters and their Bundles.
	FleetNamespace string
	// ConfigMap to create on downstream clusters.
	ProjectIDsNamespace string
	ProjectIDsName      string
	// Full re-list interval; also restores Bundles deleted by hand.
	Resync time.Duration
}

type Syncer struct {
	client    dynamic.Interface
	logger    *slog.Logger
	cfg       Config
	clusters  dynamicinformer.DynamicSharedInformerFactory
	projects  dynamicinformer.DynamicSharedInformerFactory
	clusterIn cache.SharedIndexInformer
	projectIn cache.SharedIndexInformer
	queue     workqueue.TypedRateLimitingInterface[string]
}

func New(client dynamic.Interface, logger *slog.Logger, cfg Config) *Syncer {
	clusters := dynamicinformer.NewFilteredDynamicSharedInformerFactory(client, cfg.Resync, cfg.FleetNamespace,
		func(o *metav1.ListOptions) { o.LabelSelector = cfg.ClusterSelector })
	projects := dynamicinformer.NewDynamicSharedInformerFactory(client, cfg.Resync)

	s := &Syncer{
		client:    client,
		logger:    logger,
		cfg:       cfg,
		clusters:  clusters,
		projects:  projects,
		clusterIn: clusters.ForResource(clusterGVR).Informer(),
		projectIn: projects.ForResource(projectGVR).Informer(),
		queue:     workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
	}

	// The queue is keyed by cluster ID, so a burst of project changes only
	// rebuilds that cluster's Bundle once.
	_, _ = s.projectIn.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.enqueueProject,
		UpdateFunc: func(_, obj any) { s.enqueueProject(obj) },
		DeleteFunc: s.enqueueProject,
	})
	// There is no DeleteFunc on purpose: deleting a Bundle deletes the
	// ConfigMap downstream, so one left behind by a cluster that stops matching
	// the selector stays as it is. The ownerReference removes it with the cluster.
	_, _ = s.clusterIn.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    s.enqueueCluster,
		UpdateFunc: func(_, obj any) { s.enqueueCluster(obj) },
	})

	return s
}

// Run blocks until ctx is done.
func (s *Syncer) Run(ctx context.Context) {
	s.clusters.Start(ctx.Done())
	s.projects.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), s.clusterIn.HasSynced, s.projectIn.HasSynced)
	s.logger.Info("Hub sync caches synced")

	go func() {
		for s.processNext(ctx) {
		}
	}()
	<-ctx.Done()
	s.queue.ShutDown()
}

// HasSynced reports whether both caches are synced (readiness).
func (s *Syncer) HasSynced() bool {
	return s.clusterIn.HasSynced() && s.projectIn.HasSynced()
}

func (s *Syncer) enqueueProject(obj any) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return
	}
	if clusterID, _, err := cache.SplitMetaNamespaceKey(key); err == nil {
		s.queue.Add(clusterID)
	}
}

func (s *Syncer) enqueueCluster(obj any) {
	if cluster, ok := obj.(*unstructured.Unstructured); ok {
		if clusterID := clusterIDOf(cluster); clusterID != "" {
			s.queue.Add(clusterID)
		}
	}
}

func (s *Syncer) processNext(ctx context.Context) bool {
	clusterID, shutdown := s.queue.Get()
	if shutdown {
		return false
	}
	defer s.queue.Done(clusterID)

	if err := s.sync(ctx, clusterID); err != nil {
		s.logger.Error("Failed to sync project IDs",
			slog.String("cluster_id", clusterID),
			slog.String("error", err.Error()),
		)
		metrics.RequestsTotal.WithLabelValues(Operation, metrics.StatusError).Inc()
		s.queue.AddRateLimited(clusterID)
		return true
	}
	s.queue.Forget(clusterID)
	return true
}

func (s *Syncer) sync(ctx context.Context, clusterID string) error {
	cluster := s.findCluster(clusterID)
	if cluster == nil {
		// Not selected, as projects are watched for every cluster.
		return nil
	}
	logger := s.logger.With(slog.String("cluster", cluster.GetName()), slog.String("cluster_id", clusterID))

	projects, err := s.projects.ForResource(projectGVR).Lister().ByNamespace(clusterID).List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	unstructuredProjects := make([]*unstructured.Unstructured, 0, len(projects))
	for _, obj := range projects {
		if project, ok := obj.(*unstructured.Unstructured); ok {
			unstructuredProjects = append(unstructuredProjects, project)
		}
	}

	ids, conflicts := ProjectIDs(clusterID, unstructuredProjects)
	for _, key := range conflicts {
		logger.Error("Projects share a ConfigMap key, skipping them", slog.String("key", key))
		metrics.RequestsTotal.WithLabelValues(Operation, metrics.StatusSkipped).Inc()
	}
	// Rancher clusters always have projects, so an empty map means something is
	// wrong and must not wipe the ConfigMap downstream.
	if len(ids) == 0 {
		logger.Warn("No projects found, leaving the Bundle unchanged")
		metrics.RequestsTotal.WithLabelValues(Operation, metrics.StatusSkipped).Inc()
		return nil
	}

	bundle, err := s.buildBundle(cluster, clusterID, ids)
	if err != nil {
		return err
	}
	if s.cfg.DryRun {
		logger.Info("[DRY-RUN] Would apply project IDs Bundle",
			slog.String("bundle", bundle.GetName()),
			slog.Any("project_ids", ids),
		)
		metrics.RequestsTotal.WithLabelValues(Operation, metrics.StatusDryRun).Inc()
		return nil
	}

	if _, err := s.client.Resource(bundleGVR).Namespace(s.cfg.FleetNamespace).Apply(ctx, bundle.GetName(), bundle,
		metav1.ApplyOptions{FieldManager: fieldManager, Force: true}); err != nil {
		return fmt.Errorf("apply bundle %s: %w", bundle.GetName(), err)
	}
	logger.Debug("Applied project IDs Bundle", slog.String("bundle", bundle.GetName()), slog.Int("projects", len(ids)))
	metrics.RequestsTotal.WithLabelValues(Operation, metrics.StatusMutated).Inc()
	return nil
}

func (s *Syncer) findCluster(clusterID string) *unstructured.Unstructured {
	for _, obj := range s.clusterIn.GetStore().List() {
		if cluster, ok := obj.(*unstructured.Unstructured); ok && clusterIDOf(cluster) == clusterID {
			return cluster
		}
	}
	return nil
}

func clusterIDOf(cluster *unstructured.Unstructured) string {
	clusterID, _, _ := unstructured.NestedString(cluster.Object, "status", "clusterName")
	return clusterID
}

// ProjectIDs maps project display names to "<cluster-id>:<project-id>". Display
// names become ConfigMap keys with invalid characters replaced by "-", keeping
// case so they still match existing project labels. Projects that end up with
// the same key are left out and returned in conflicts, since assigning a
// namespace to the wrong project is worse than not assigning it.
func ProjectIDs(clusterID string, projects []*unstructured.Unstructured) (ids map[string]string, conflicts []string) {
	ids = make(map[string]string, len(projects))
	conflicted := make(map[string]bool)

	for _, project := range projects {
		displayName, _, _ := unstructured.NestedString(project.Object, "spec", "displayName")
		key := ConfigMapKey(displayName)
		if key == "" {
			continue
		}
		if _, taken := ids[key]; taken || conflicted[key] {
			if !conflicted[key] {
				conflicts = append(conflicts, key)
			}
			conflicted[key] = true
			delete(ids, key)
			continue
		}
		ids[key] = clusterID + ":" + project.GetName()
	}
	return ids, conflicts
}

// ConfigMapKey converts a project display name to a valid ConfigMap key.
func ConfigMapKey(displayName string) string {
	return strings.Trim(invalidKeyChars.ReplaceAllString(displayName, "-"), "-_.")
}

func (s *Syncer) buildBundle(cluster *unstructured.Unstructured, clusterID string, ids map[string]string) (*unstructured.Unstructured, error) {
	manifest, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": s.cfg.ProjectIDsName, "namespace": s.cfg.ProjectIDsNamespace},
		"data":       ids,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal configmap: %w", err)
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": bundleGVR.GroupVersion().String(),
		"kind":       "Bundle",
		"metadata": map[string]any{
			"name":      bundlePrefix + clusterID,
			"namespace": s.cfg.FleetNamespace,
			"ownerReferences": []any{map[string]any{
				"apiVersion": clusterGVR.GroupVersion().String(),
				"kind":       "Cluster",
				"name":       cluster.GetName(),
				"uid":        string(cluster.GetUID()),
			}},
		},
		"spec": map[string]any{
			"namespace": s.cfg.ProjectIDsNamespace,
			// Adopts a ConfigMap that was created by hand before the sync.
			"helm": map[string]any{"takeOwnership": true},
			"resources": []any{map[string]any{
				"name":    s.cfg.ProjectIDsName + ".yaml",
				"content": string(manifest),
			}},
			"targets": []any{map[string]any{"clusterName": cluster.GetName()}},
		},
	}}, nil
}
