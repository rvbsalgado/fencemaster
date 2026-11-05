// Package controller assigns namespaces to Rancher projects by watching them
// from inside the downstream cluster, instead of via an admission webhook.
//
// It needs no route from the API server to fencemaster (EKS control planes
// can't reach an external webhook) and no Rancher lookups: project IDs come
// from a ConfigMap of project name -> "<cluster-id>:<project-id>" kept on the
// cluster (e.g. cattle-system/rancher-project-ids, written by Terraform).
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/rvbsalgado/fencemaster/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

// Operation is the "operation" label value on fencemaster_requests_total.
const Operation = "RECONCILE"

type Config struct {
	DryRun            bool
	ProjectLabel      string
	ProjectAnnotation string
	IsExcluded        func(string) bool
	// ConfigMap holding project name -> annotation value.
	ProjectIDsNamespace string
	ProjectIDsName      string
	// Full re-list interval; also retries failed patches.
	Resync time.Duration
}

type Controller struct {
	client    kubernetes.Interface
	logger    *slog.Logger
	cfg       Config
	nsFactory informers.SharedInformerFactory
	cmFactory informers.SharedInformerFactory
	nsLister  corelisters.NamespaceLister
	cmLister  corelisters.ConfigMapLister
	nsSynced  cache.InformerSynced
	cmSynced  cache.InformerSynced
	ctx       context.Context
}

func New(client kubernetes.Interface, logger *slog.Logger, cfg Config) *Controller {
	nsFactory := informers.NewSharedInformerFactory(client, cfg.Resync)
	cmFactory := informers.NewSharedInformerFactoryWithOptions(client, cfg.Resync,
		informers.WithNamespace(cfg.ProjectIDsNamespace),
		informers.WithTweakListOptions(func(o *metav1.ListOptions) {
			o.FieldSelector = "metadata.name=" + cfg.ProjectIDsName
		}),
	)
	nsInformer := nsFactory.Core().V1().Namespaces()
	cmInformer := cmFactory.Core().V1().ConfigMaps()

	c := &Controller{
		client:    client,
		logger:    logger,
		cfg:       cfg,
		nsFactory: nsFactory,
		cmFactory: cmFactory,
		nsLister:  nsInformer.Lister(),
		cmLister:  cmInformer.Lister(),
		nsSynced:  nsInformer.Informer().HasSynced,
		cmSynced:  cmInformer.Informer().HasSynced,
	}

	// ponytail: reconciles inline in the informer callback, no workqueue - a
	// failed patch is retried on the next resync. Add a rate-limited queue if
	// namespace churn ever makes that too slow.
	_, _ = nsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { c.reconcile(obj.(*corev1.Namespace)) },
		UpdateFunc: func(_, obj any) { c.reconcile(obj.(*corev1.Namespace)) },
	})
	// A new/changed project ID must reach namespaces that were already skipped.
	_, _ = cmInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { c.reconcileAll() },
		UpdateFunc: func(_, _ any) { c.reconcileAll() },
	})

	return c
}

// Run blocks until ctx is done. The ConfigMap cache syncs before namespaces
// are watched, so the initial namespace list isn't reconciled against an
// empty project map.
func (c *Controller) Run(ctx context.Context) {
	c.ctx = ctx
	c.cmFactory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), c.cmSynced)
	c.nsFactory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), c.nsSynced)
	c.logger.Info("Controller caches synced")
	<-ctx.Done()
}

// HasSynced reports whether both caches are synced (readiness).
func (c *Controller) HasSynced() bool {
	return c.cmSynced() && c.nsSynced()
}

func (c *Controller) reconcileAll() {
	if !c.nsSynced() {
		return
	}
	namespaces, err := c.nsLister.List(labels.Everything())
	if err != nil {
		c.logger.Error("Failed to list namespaces", slog.String("error", err.Error()))
		return
	}
	for _, ns := range namespaces {
		c.reconcile(ns)
	}
}

func (c *Controller) reconcile(ns *corev1.Namespace) {
	status := c.sync(ns)
	metrics.RequestsTotal.WithLabelValues(Operation, status).Inc()
}

func (c *Controller) sync(ns *corev1.Namespace) string {
	logger := c.logger.With(slog.String("namespace", ns.Name))

	if c.cfg.IsExcluded(ns.Name) {
		return metrics.StatusSkipped
	}
	// No label = not ours to manage (e.g. moved by hand in the Rancher UI).
	projectName, ok := ns.Labels[c.cfg.ProjectLabel]
	if !ok {
		return metrics.StatusSkipped
	}

	want, err := c.projectAnnotationValue(projectName)
	if err != nil {
		logger.Warn("Project ID not found, leaving namespace unassigned",
			slog.String("project", projectName),
			slog.String("configmap", c.cfg.ProjectIDsNamespace+"/"+c.cfg.ProjectIDsName),
			slog.String("error", err.Error()),
		)
		metrics.ProjectLookupErrorsTotal.WithLabelValues(metrics.ErrorTypeNotFound).Inc()
		return metrics.StatusAllowed
	}
	if ns.Annotations[c.cfg.ProjectAnnotation] == want {
		return metrics.StatusSkipped
	}

	logger = logger.With(
		slog.String("project", projectName),
		slog.String("annotation", want),
		slog.String("previous", ns.Annotations[c.cfg.ProjectAnnotation]),
	)
	if c.cfg.DryRun {
		logger.Info("[DRY-RUN] Would set project annotation on namespace")
		return metrics.StatusDryRun
	}

	patch, _ := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]string{c.cfg.ProjectAnnotation: want},
		},
	})
	if _, err := c.client.CoreV1().Namespaces().Patch(c.ctx, ns.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		logger.Error("Failed to patch namespace", slog.String("error", err.Error()))
		return metrics.StatusError
	}
	logger.Info("Set project annotation on namespace")
	return metrics.StatusMutated
}

func (c *Controller) projectAnnotationValue(projectName string) (string, error) {
	cm, err := c.cmLister.ConfigMaps(c.cfg.ProjectIDsNamespace).Get(c.cfg.ProjectIDsName)
	if err != nil {
		return "", err
	}
	value, ok := cm.Data[projectName]
	if !ok || value == "" {
		return "", errProjectNotInConfigMap
	}
	return value, nil
}

var errProjectNotInConfigMap = errors.New("project not in configmap")
