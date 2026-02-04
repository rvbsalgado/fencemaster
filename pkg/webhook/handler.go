package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/rvbsalgado/fencemaster/pkg/metrics"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	projectLabel      = "project"
	projectAnnotation = "field.cattle.io/projectId"
	// maxRequestBodySize limits the request body to 1MB to prevent DoS attacks
	maxRequestBodySize = 1 << 20 // 1MB
)

// RancherClient defines the interface for Rancher API operations
type RancherClient interface {
	GetClusterID(ctx context.Context, clusterName string) (string, error)
	GetProjectID(ctx context.Context, clusterID, projectName string) (string, error)
	HealthCheck(ctx context.Context) error
}

type Handler struct {
	rancherClient RancherClient
	logger        *slog.Logger
	strictMode    bool
	dryRun        bool
}

func NewHandler(rancherClient RancherClient, logger *slog.Logger, strictMode, dryRun bool) *Handler {
	return &Handler{
		rancherClient: rancherClient,
		logger:        logger,
		strictMode:    strictMode,
		dryRun:        dryRun,
	}
}

// HandleMutate handles admission requests at /mutate/{cluster-name}
func (h *Handler) HandleMutate(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Extract cluster name from URL path: /mutate/{cluster-name}
	path := strings.TrimPrefix(r.URL.Path, "/mutate/")
	clusterName := strings.TrimSuffix(path, "/")

	if clusterName == "" || clusterName == "mutate" {
		h.logger.Error("No cluster name in URL path", slog.String("path", r.URL.Path))
		metrics.RequestsTotal.WithLabelValues("unknown", metrics.StatusError).Inc()
		http.Error(w, "cluster name required in URL path: /mutate/{cluster-name}", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		h.logger.Error("Failed to read request body", slog.String("error", err.Error()))
		metrics.RequestsTotal.WithLabelValues("unknown", metrics.StatusError).Inc()
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	var admissionReview admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &admissionReview); err != nil {
		h.logger.Error("Failed to unmarshal admission review", slog.String("error", err.Error()))
		metrics.RequestsTotal.WithLabelValues("unknown", metrics.StatusError).Inc()
		http.Error(w, "failed to unmarshal admission review", http.StatusBadRequest)
		return
	}

	operation := string(admissionReview.Request.Operation)
	response, status := h.mutate(r.Context(), admissionReview.Request, clusterName)
	admissionReview.Response = response
	admissionReview.Response.UID = admissionReview.Request.UID

	// Record metrics
	metrics.RequestsTotal.WithLabelValues(operation, status).Inc()
	metrics.RequestDuration.WithLabelValues(operation).Observe(time.Since(start).Seconds())

	respBytes, err := json.Marshal(admissionReview)
	if err != nil {
		h.logger.Error("Failed to marshal response", slog.String("error", err.Error()))
		http.Error(w, "failed to marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(respBytes)
}

func (h *Handler) mutate(ctx context.Context, req *admissionv1.AdmissionRequest, clusterName string) (*admissionv1.AdmissionResponse, string) {
	if req.Kind.Kind != "Namespace" {
		return &admissionv1.AdmissionResponse{Allowed: true}, metrics.StatusSkipped
	}

	var namespace corev1.Namespace
	if err := json.Unmarshal(req.Object.Raw, &namespace); err != nil {
		h.logger.Error("Failed to unmarshal namespace", slog.String("error", err.Error()))
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: fmt.Sprintf("failed to unmarshal namespace: %v", err),
			},
		}, metrics.StatusError
	}

	// Get the project label from the new namespace
	projectName, hasProjectLabel := namespace.Labels[projectLabel]
	if !hasProjectLabel {
		h.logger.Debug("Namespace has no project label, skipping",
			slog.String("namespace", namespace.Name),
			slog.String("cluster", clusterName),
			slog.String("operation", string(req.Operation)),
		)
		return &admissionv1.AdmissionResponse{Allowed: true}, metrics.StatusSkipped
	}

	// For UPDATE operations, check if we need to do anything
	if req.Operation == admissionv1.Update {
		// Check current annotation value
		currentAnnotation := namespace.Annotations[projectAnnotation]

		// Parse old object to see if project label changed
		var oldNamespace corev1.Namespace
		if req.OldObject.Raw != nil {
			if err := json.Unmarshal(req.OldObject.Raw, &oldNamespace); err == nil {
				oldProjectName := oldNamespace.Labels[projectLabel]

				// If project label hasn't changed and annotation exists, skip
				if oldProjectName == projectName && currentAnnotation != "" {
					h.logger.Debug("Project label unchanged and annotation exists, skipping",
						slog.String("namespace", namespace.Name),
						slog.String("cluster", clusterName),
						slog.String("project", projectName),
					)
					return &admissionv1.AdmissionResponse{Allowed: true}, metrics.StatusSkipped
				}
			}
		}
	}

	clusterID, err := h.rancherClient.GetClusterID(ctx, clusterName)
	if err != nil {
		h.logger.Error("Failed to get cluster ID",
			slog.String("cluster", clusterName),
			slog.String("error", err.Error()),
		)
		// Error type is recorded by the rancher client, not here
		if h.strictMode {
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf("failed to get cluster ID: %v", err),
				},
			}, metrics.StatusDenied
		}
		h.logger.Warn("Allowing namespace without project annotation (strict mode disabled)",
			slog.String("namespace", namespace.Name),
			slog.String("cluster", clusterName),
			slog.String("reason", "cluster_not_found"),
		)
		return &admissionv1.AdmissionResponse{Allowed: true}, metrics.StatusAllowed
	}

	projectID, err := h.rancherClient.GetProjectID(ctx, clusterID, projectName)
	if err != nil {
		h.logger.Error("Failed to get project ID",
			slog.String("project", projectName),
			slog.String("cluster_id", clusterID),
			slog.String("error", err.Error()),
		)
		// Error type is recorded by the rancher client, not here
		if h.strictMode {
			return &admissionv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: fmt.Sprintf("failed to get project ID for '%s': %v", projectName, err),
				},
			}, metrics.StatusDenied
		}
		h.logger.Warn("Allowing namespace without project annotation (strict mode disabled)",
			slog.String("namespace", namespace.Name),
			slog.String("cluster", clusterName),
			slog.String("project", projectName),
			slog.String("reason", "project_not_found"),
		)
		return &admissionv1.AdmissionResponse{Allowed: true}, metrics.StatusAllowed
	}

	projectAnnotationValue := fmt.Sprintf("%s:%s", clusterID, projectID)

	// Check if annotation already has the correct value (avoid unnecessary patches)
	if namespace.Annotations != nil && namespace.Annotations[projectAnnotation] == projectAnnotationValue {
		h.logger.Debug("Annotation already has correct value, skipping",
			slog.String("namespace", namespace.Name),
			slog.String("cluster", clusterName),
			slog.String("annotation", projectAnnotationValue),
		)
		return &admissionv1.AdmissionResponse{Allowed: true}, metrics.StatusSkipped
	}

	// Dry-run mode: log what would happen but don't apply the patch
	if h.dryRun {
		h.logger.Info("[DRY-RUN] Would add project annotation to namespace",
			slog.String("namespace", namespace.Name),
			slog.String("cluster", clusterName),
			slog.String("cluster_id", clusterID),
			slog.String("project", projectName),
			slog.String("project_id", projectID),
			slog.String("annotation", projectAnnotationValue),
			slog.String("operation", string(req.Operation)),
		)
		return &admissionv1.AdmissionResponse{Allowed: true}, metrics.StatusDryRun
	}

	patch := []map[string]any{
		{
			"op":    "add",
			"path":  "/metadata/annotations",
			"value": map[string]string{},
		},
		{
			"op":    "add",
			"path":  fmt.Sprintf("/metadata/annotations/%s", escapeJSONPointer(projectAnnotation)),
			"value": projectAnnotationValue,
		},
	}

	// If annotations already exist, only add/replace the project annotation
	if namespace.Annotations != nil {
		patch = []map[string]any{
			{
				"op":    "add",
				"path":  fmt.Sprintf("/metadata/annotations/%s", escapeJSONPointer(projectAnnotation)),
				"value": projectAnnotationValue,
			},
		}
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		h.logger.Error("Failed to marshal patch", slog.String("error", err.Error()))
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Message: fmt.Sprintf("failed to marshal patch: %v", err),
			},
		}, metrics.StatusError
	}

	h.logger.Info("Adding project annotation to namespace",
		slog.String("namespace", namespace.Name),
		slog.String("cluster", clusterName),
		slog.String("cluster_id", clusterID),
		slog.String("project", projectName),
		slog.String("project_id", projectID),
		slog.String("annotation", projectAnnotationValue),
		slog.String("operation", string(req.Operation)),
	)

	patchType := admissionv1.PatchTypeJSONPatch
	return &admissionv1.AdmissionResponse{
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &patchType,
	}, metrics.StatusMutated
}

func escapeJSONPointer(s string) string {
	// JSON Pointer escaping: ~ becomes ~0, / becomes ~1
	result := ""
	for _, c := range s {
		switch c {
		case '~':
			result += "~0"
		case '/':
			result += "~1"
		default:
			result += string(c)
		}
	}
	return result
}
