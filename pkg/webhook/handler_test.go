package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rvbsalgado/fencemaster/pkg/metrics"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestHandleMutate_MissingClusterName(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(nil, logger, testHandlerConfig())

	req := httptest.NewRequest(http.MethodPost, "/mutate/", nil)
	w := httptest.NewRecorder()

	handler.HandleMutate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestHandleMutate_InvalidBody(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(nil, logger, testHandlerConfig())

	req := httptest.NewRequest(http.MethodPost, "/mutate/test-cluster", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	handler.HandleMutate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestMutate_NonNamespaceResource(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(nil, logger, testHandlerConfig())

	req := &admissionv1.AdmissionRequest{
		Kind: metav1.GroupVersionKind{
			Kind: "Pod",
		},
	}

	response, status := handler.mutate(context.Background(), req, "test-cluster", logger)

	if !response.Allowed {
		t.Error("expected request to be allowed for non-namespace resource")
	}
	if status != metrics.StatusSkipped {
		t.Errorf("expected status '%s', got '%s'", metrics.StatusSkipped, status)
	}
}

func TestMutate_NamespaceWithoutProjectLabel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(nil, logger, testHandlerConfig())

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{},
		},
	}
	nsBytes, _ := json.Marshal(ns)

	req := &admissionv1.AdmissionRequest{
		Kind: metav1.GroupVersionKind{
			Kind: "Namespace",
		},
		Object: runtime.RawExtension{
			Raw: nsBytes,
		},
		Operation: admissionv1.Create,
	}

	response, status := handler.mutate(context.Background(), req, "test-cluster", logger)

	if !response.Allowed {
		t.Error("expected request to be allowed for namespace without project label")
	}
	if status != metrics.StatusSkipped {
		t.Errorf("expected status '%s', got '%s'", metrics.StatusSkipped, status)
	}
}

func TestMutate_InvalidNamespaceJSON(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(nil, logger, testHandlerConfig())

	req := &admissionv1.AdmissionRequest{
		Kind: metav1.GroupVersionKind{
			Kind: "Namespace",
		},
		Object: runtime.RawExtension{
			Raw: []byte("invalid json"),
		},
		Operation: admissionv1.Create,
	}

	response, status := handler.mutate(context.Background(), req, "test-cluster", logger)

	if response.Allowed {
		t.Error("expected request to be denied for invalid namespace JSON")
	}
	if status != metrics.StatusError {
		t.Errorf("expected status '%s', got '%s'", metrics.StatusError, status)
	}
}

func TestEscapeJSONPointer(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with/slash", "with~1slash"},
		{"with~tilde", "with~0tilde"},
		{"field.cattle.io/projectId", "field.cattle.io~1projectId"},
		{"multiple/slashes/here", "multiple~1slashes~1here"},
		{"~and/mixed", "~0and~1mixed"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapeJSONPointer(tt.input)
			if result != tt.expected {
				t.Errorf("escapeJSONPointer(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHandleMutate_ExtractClusterName(t *testing.T) {
	tests := []struct {
		path            string
		expectedCluster string
		expectError     bool
	}{
		{"/mutate/cluster-a", "cluster-a", false},
		{"/mutate/cluster-a/", "cluster-a", false},
		{"/mutate/my-downstream-cluster", "my-downstream-cluster", false},
		{"/mutate/", "", true},
		// Note: /mutate without trailing slash results in path "mutate" after TrimPrefix
		// which is caught by the "mutate" check
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := NewHandler(nil, logger, testHandlerConfig())

			// Create a valid admission review for non-error cases
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-ns",
				},
			}
			nsBytes, _ := json.Marshal(ns)

			admReview := admissionv1.AdmissionReview{
				Request: &admissionv1.AdmissionRequest{
					UID: "test-uid",
					Kind: metav1.GroupVersionKind{
						Kind: "Namespace",
					},
					Object: runtime.RawExtension{
						Raw: nsBytes,
					},
					Operation: admissionv1.Create,
				},
			}
			body, _ := json.Marshal(admReview)

			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(body))
			w := httptest.NewRecorder()

			handler.HandleMutate(w, req)

			if tt.expectError {
				if w.Code != http.StatusBadRequest {
					t.Errorf("expected status %d for path %s, got %d", http.StatusBadRequest, tt.path, w.Code)
				}
			} else {
				if w.Code != http.StatusOK {
					t.Errorf("expected status %d for path %s, got %d", http.StatusOK, tt.path, w.Code)
				}
			}
		})
	}
}

func createAdmissionReview(ns *corev1.Namespace, operation admissionv1.Operation) *admissionv1.AdmissionReview {
	nsBytes, _ := json.Marshal(ns)
	return &admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Kind: "Namespace",
			},
			Object: runtime.RawExtension{
				Raw: nsBytes,
			},
			Operation: operation,
		},
	}
}

func createAdmissionReviewWithOld(ns, oldNs *corev1.Namespace, operation admissionv1.Operation) *admissionv1.AdmissionReview {
	nsBytes, _ := json.Marshal(ns)
	oldNsBytes, _ := json.Marshal(oldNs)
	return &admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Kind: "Namespace",
			},
			Object: runtime.RawExtension{
				Raw: nsBytes,
			},
			OldObject: runtime.RawExtension{
				Raw: oldNsBytes,
			},
			Operation: operation,
		},
	}
}

// mockRancherClient implements a mock for testing the full mutation flow
type mockRancherClient struct {
	clusterID   string
	clusterErr  error
	projectID   string
	projectErr  error
}

func (m *mockRancherClient) GetClusterID(ctx context.Context, clusterName string) (string, error) {
	if m.clusterErr != nil {
		return "", m.clusterErr
	}
	return m.clusterID, nil
}

func (m *mockRancherClient) GetProjectID(ctx context.Context, clusterID, projectName string) (string, error) {
	if m.projectErr != nil {
		return "", m.projectErr
	}
	return m.projectID, nil
}

func (m *mockRancherClient) HealthCheck(ctx context.Context) error {
	return nil
}

// testHandlerConfig returns default config for tests
func testHandlerConfig() HandlerConfig {
	return HandlerConfig{
		StrictMode:        false,
		DryRun:            false,
		ProjectLabel:      "project",
		ProjectAnnotation: "field.cattle.io/projectId",
	}
}

func TestMutate_SuccessfulMutation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockClient := &mockRancherClient{
		clusterID: "c-m-abc123",
		projectID: "p-xyz789",
	}

	handler := NewHandler(mockClient, logger, testHandlerConfig())

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"project": "platform",
			},
		},
	}

	review := createAdmissionReview(ns, admissionv1.Create)
	response, status := handler.mutate(context.Background(), review.Request, "test-cluster", logger)

	if !response.Allowed {
		t.Error("expected request to be allowed")
	}
	if status != metrics.StatusMutated {
		t.Errorf("expected status '%s', got '%s'", metrics.StatusMutated, status)
	}
	if response.Patch == nil {
		t.Error("expected patch to be set")
	}

	// Verify patch contains the correct annotation
	var patch []map[string]any
	if err := json.Unmarshal(response.Patch, &patch); err != nil {
		t.Fatalf("failed to unmarshal patch: %v", err)
	}

	// Find the annotation patch operation
	found := false
	for _, op := range patch {
		if value, ok := op["value"].(string); ok && value == "c-m-abc123:p-xyz789" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected patch to contain annotation value 'c-m-abc123:p-xyz789', got: %s", string(response.Patch))
	}
}

func TestMutate_DryRunMode(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockClient := &mockRancherClient{
		clusterID: "c-m-abc123",
		projectID: "p-xyz789",
	}

	cfg := testHandlerConfig()
	cfg.DryRun = true
	handler := NewHandler(mockClient, logger, cfg)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"project": "platform",
			},
		},
	}

	review := createAdmissionReview(ns, admissionv1.Create)
	response, status := handler.mutate(context.Background(), review.Request, "test-cluster", logger)

	if !response.Allowed {
		t.Error("expected request to be allowed in dry-run mode")
	}
	if status != metrics.StatusDryRun {
		t.Errorf("expected status '%s', got '%s'", metrics.StatusDryRun, status)
	}
	if response.Patch != nil {
		t.Error("expected no patch in dry-run mode")
	}
}

func TestMutate_StrictModeClusterNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockClient := &mockRancherClient{
		clusterErr: fmt.Errorf("cluster not found"),
	}

	cfg := testHandlerConfig()
	cfg.StrictMode = true
	handler := NewHandler(mockClient, logger, cfg)

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"project": "platform",
			},
		},
	}

	review := createAdmissionReview(ns, admissionv1.Create)
	response, status := handler.mutate(context.Background(), review.Request, "test-cluster", logger)

	if response.Allowed {
		t.Error("expected request to be denied in strict mode when cluster not found")
	}
	if status != metrics.StatusDenied {
		t.Errorf("expected status '%s', got '%s'", metrics.StatusDenied, status)
	}
}

func TestMutate_PermissiveModeClusterNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockClient := &mockRancherClient{
		clusterErr: fmt.Errorf("cluster not found"),
	}

	handler := NewHandler(mockClient, logger, testHandlerConfig())

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"project": "platform",
			},
		},
	}

	review := createAdmissionReview(ns, admissionv1.Create)
	response, status := handler.mutate(context.Background(), review.Request, "test-cluster", logger)

	if !response.Allowed {
		t.Error("expected request to be allowed in permissive mode")
	}
	if status != metrics.StatusAllowed {
		t.Errorf("expected status '%s', got '%s'", metrics.StatusAllowed, status)
	}
}

func TestMutate_UpdateWithUnchangedLabel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mockClient := &mockRancherClient{
		clusterID: "c-m-abc123",
		projectID: "p-xyz789",
	}

	handler := NewHandler(mockClient, logger, testHandlerConfig())

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"project": "platform",
			},
			Annotations: map[string]string{
				"field.cattle.io/projectId": "c-m-abc123:p-xyz789",
			},
		},
	}

	oldNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"project": "platform",
			},
			Annotations: map[string]string{
				"field.cattle.io/projectId": "c-m-abc123:p-xyz789",
			},
		},
	}

	review := createAdmissionReviewWithOld(ns, oldNs, admissionv1.Update)
	response, status := handler.mutate(context.Background(), review.Request, "test-cluster", logger)

	if !response.Allowed {
		t.Error("expected request to be allowed")
	}
	if status != metrics.StatusSkipped {
		t.Errorf("expected status '%s', got '%s'", metrics.StatusSkipped, status)
	}
}
