package controller

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rvbsalgado/fencemaster/pkg/webhook"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const annotation = "field.cattle.io/projectId"

func ns(name string, labels, annotations map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels, Annotations: annotations}}
}

func TestController(t *testing.T) {
	projectIDs := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "cattle-system", Name: "rancher-project-ids"},
		Data:       map[string]string{"platform": "c-abc:p-plat"},
	}
	client := fake.NewSimpleClientset(
		projectIDs,
		ns("labeled", map[string]string{"project": "platform"}, nil),
		ns("unlabeled", nil, nil),
		ns("kube-system", map[string]string{"project": "platform"}, nil),
		ns("wrong", map[string]string{"project": "platform"}, map[string]string{annotation: "c-abc:p-old", "keep": "me"}),
		ns("later", map[string]string{"project": "security"}, nil),
	)

	c := New(client, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		ProjectLabel:        "project",
		ProjectAnnotation:   annotation,
		IsExcluded:          webhook.ExcludeMatcher([]string{"kube-*"}),
		ProjectIDsNamespace: "cattle-system",
		ProjectIDsName:      "rancher-project-ids",
		Resync:              time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	get := func(name string) map[string]string {
		n, err := client.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return n.Annotations
	}
	waitFor := func(name, want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for get(name)[annotation] != want {
			if time.Now().After(deadline) {
				t.Fatalf("%s: annotation = %q, want %q", name, get(name)[annotation], want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	waitFor("labeled", "c-abc:p-plat")
	waitFor("wrong", "c-abc:p-plat")
	if get("wrong")["keep"] != "me" {
		t.Error("wrong: other annotations must be preserved")
	}
	for _, name := range []string{"unlabeled", "kube-system", "later"} {
		if v, ok := get(name)[annotation]; ok {
			t.Errorf("%s: must not be annotated, got %q", name, v)
		}
	}

	// A project ID added later reaches namespaces that were skipped.
	projectIDs.Data["security"] = "c-abc:p-sec"
	if _, err := client.CoreV1().ConfigMaps("cattle-system").Update(ctx, projectIDs, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor("later", "c-abc:p-sec")

	// A changed project label moves the namespace.
	moved := ns("labeled", map[string]string{"project": "security"}, map[string]string{annotation: "c-abc:p-plat"})
	if _, err := client.CoreV1().Namespaces().Update(ctx, moved, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor("labeled", "c-abc:p-sec")
}
