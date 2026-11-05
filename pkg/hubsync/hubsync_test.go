package hubsync

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

func project(clusterID, id, displayName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "management.cattle.io/v3",
		"kind":       "Project",
		"metadata":   map[string]any{"name": id, "namespace": clusterID},
		"spec":       map[string]any{"displayName": displayName},
	}}
}

func cluster(name, clusterID string, labels map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "provisioning.cattle.io/v1",
		"kind":       "Cluster",
		"metadata":   map[string]any{"name": name, "namespace": "fleet-default", "labels": labels, "uid": "uid-" + name},
		"status":     map[string]any{"clusterName": clusterID},
	}}
}

func TestConfigMapKey(t *testing.T) {
	tests := []struct{ name, want string }{
		{"platform", "platform"},
		{"Platform", "Platform"},
		{"Team A", "Team-A"},
		{"  team / a!  ", "team-a"},
		{"a.b_c-d", "a.b_c-d"},
		{"???", ""},
	}
	for _, tt := range tests {
		if got := ConfigMapKey(tt.name); got != tt.want {
			t.Errorf("ConfigMapKey(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestProjectIDs(t *testing.T) {
	ids, conflicts := ProjectIDs("c-abc", []*unstructured.Unstructured{
		project("c-abc", "p-1", "platform"),
		project("c-abc", "p-2", "Team A"),
		project("c-abc", "p-3", "Team-A"),
		project("c-abc", "p-4", "Team_A"),
		project("c-abc", "p-5", "Dup Dup"),
		project("c-abc", "p-6", "Dup-Dup"),
		project("c-abc", "p-7", "Dup Dup"),
		project("c-abc", "p-8", "???"),
	})

	want := map[string]string{"platform": "c-abc:p-1", "Team_A": "c-abc:p-4"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if !reflect.DeepEqual(conflicts, []string{"Team-A", "Dup-Dup"}) {
		t.Errorf("conflicts = %v", conflicts)
	}
}

// applied records the Bundles sent by server-side apply, which the dynamic fake
// can't serve itself.
type applied struct {
	sync.Mutex
	bundles map[string]map[string]any
}

func (a *applied) get(name string) map[string]any {
	a.Lock()
	defer a.Unlock()
	return a.bundles[name]
}

func (a *applied) count() int {
	a.Lock()
	defer a.Unlock()
	return len(a.bundles)
}

func start(t *testing.T, cfg Config, objs ...runtime.Object) (*dynamicfake.FakeDynamicClient, *applied) {
	t.Helper()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		clusterGVR: "ClusterList",
		projectGVR: "ProjectList",
		bundleGVR:  "BundleList",
	}, objs...)

	a := &applied{bundles: map[string]map[string]any{}}
	client.PrependReactor("patch", "bundles", func(action k8stesting.Action) (bool, runtime.Object, error) {
		patch := action.(k8stesting.PatchAction)
		var obj map[string]any
		if err := json.Unmarshal(patch.GetPatch(), &obj); err != nil {
			t.Error(err)
		}
		a.Lock()
		defer a.Unlock()
		a.bundles[patch.GetName()] = obj
		return true, &unstructured.Unstructured{Object: obj}, nil
	})

	if cfg.ClusterSelector == "" {
		cfg.ClusterSelector = "fencemaster/project-sync=true"
	}
	cfg.FleetNamespace = "fleet-default"
	cfg.ProjectIDsNamespace = "cattle-system"
	cfg.ProjectIDsName = "rancher-project-ids"
	cfg.Resync = time.Hour
	s := New(client, slog.New(slog.NewTextHandler(io.Discard, nil)), cfg)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Run(ctx)
	return client, a
}

func configMap(t *testing.T, bundle map[string]any) map[string]any {
	t.Helper()
	spec := bundle["spec"].(map[string]any)
	content := spec["resources"].([]any)[0].(map[string]any)["content"].(string)
	var cm map[string]any
	if err := json.Unmarshal([]byte(content), &cm); err != nil {
		t.Fatal(err)
	}
	return cm
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSyncWritesBundleForOptedInClusters(t *testing.T) {
	const name = "fencemaster-project-ids-c-a"
	ctx := context.Background()
	client, a := start(t, Config{},
		cluster("cluster-a", "c-a", map[string]any{"fencemaster/project-sync": "true"}),
		cluster("cluster-b", "c-b", nil),
		project("c-a", "p-1", "platform"),
		project("c-b", "p-2", "other"),
	)

	waitFor(t, "bundle", func() bool { return a.get(name) != nil })
	bundle := a.get(name)
	if got := bundle["metadata"].(map[string]any)["namespace"]; got != "fleet-default" {
		t.Errorf("bundle namespace = %v", got)
	}
	owner := bundle["metadata"].(map[string]any)["ownerReferences"].([]any)[0].(map[string]any)
	if owner["name"] != "cluster-a" || owner["kind"] != "Cluster" || owner["uid"] != "uid-cluster-a" {
		t.Errorf("ownerReference = %v", owner)
	}
	spec := bundle["spec"].(map[string]any)
	if got := spec["targets"].([]any)[0].(map[string]any)["clusterName"]; got != "cluster-a" {
		t.Errorf("target = %v", got)
	}
	if spec["namespace"] != "cattle-system" {
		t.Errorf("spec.namespace = %v", spec["namespace"])
	}
	cm := configMap(t, bundle)
	if !reflect.DeepEqual(cm["data"], map[string]any{"platform": "c-a:p-1"}) {
		t.Errorf("configmap data = %v", cm["data"])
	}

	// A new project reaches the Bundle.
	if _, err := client.Resource(projectGVR).Namespace("c-a").Create(ctx, project("c-a", "p-3", "security"), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "new project", func() bool {
		return len(configMap(t, a.get(name))["data"].(map[string]any)) == 2
	})

	// A deleted project leaves it.
	if err := client.Resource(projectGVR).Namespace("c-a").Delete(ctx, "p-3", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "deleted project", func() bool {
		return len(configMap(t, a.get(name))["data"].(map[string]any)) == 1
	})

	if a.count() != 1 {
		t.Errorf("bundles = %d, cluster-b must not be synced", a.count())
	}
}

func TestDefaultSelectorSyncsAllClustersExceptOptedOut(t *testing.T) {
	_, a := start(t, Config{ClusterSelector: DefaultClusterSelector},
		cluster("no-labels", "c-a", nil),
		cluster("opted-in", "c-b", map[string]any{"fencemaster/project-sync": "true"}),
		cluster("opted-out", "c-c", map[string]any{"fencemaster/project-sync": "false"}),
		project("c-a", "p-1", "one"),
		project("c-b", "p-2", "two"),
		project("c-c", "p-3", "three"),
	)

	waitFor(t, "bundles", func() bool { return a.count() == 2 })
	time.Sleep(200 * time.Millisecond)
	if a.get("fencemaster-project-ids-c-c") != nil {
		t.Error("cluster labelled false must not be synced")
	}
	if a.count() != 2 {
		t.Errorf("bundles = %d, want 2", a.count())
	}
}

func TestSyncKeepsBundleWhenProjectsDisappear(t *testing.T) {
	const name = "fencemaster-project-ids-c-a"
	ctx := context.Background()
	client, a := start(t, Config{},
		cluster("cluster-a", "c-a", map[string]any{"fencemaster/project-sync": "true"}),
		project("c-a", "p-1", "platform"),
	)
	waitFor(t, "bundle", func() bool { return a.get(name) != nil })

	if err := client.Resource(projectGVR).Namespace("c-a").Delete(ctx, "p-1", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := configMap(t, a.get(name))["data"].(map[string]any); len(got) != 1 {
		t.Errorf("empty project list must not wipe the Bundle, data = %v", got)
	}
}

func TestSyncDryRunWritesNothing(t *testing.T) {
	_, a := start(t, Config{DryRun: true},
		cluster("cluster-a", "c-a", map[string]any{"fencemaster/project-sync": "true"}),
		project("c-a", "p-1", "platform"),
	)
	time.Sleep(200 * time.Millisecond)
	if a.count() != 0 {
		t.Errorf("dry-run wrote %d bundles", a.count())
	}
}

func TestSyncRetriesFailedApply(t *testing.T) {
	const name = "fencemaster-project-ids-c-a"
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		clusterGVR: "ClusterList",
		projectGVR: "ProjectList",
		bundleGVR:  "BundleList",
	},
		cluster("cluster-a", "c-a", map[string]any{"fencemaster/project-sync": "true"}),
		project("c-a", "p-1", "platform"),
	)

	var mu sync.Mutex
	calls := 0
	client.PrependReactor("patch", "bundles", func(k8stesting.Action) (bool, runtime.Object, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return true, nil, context.DeadlineExceeded
		}
		return true, &unstructured.Unstructured{Object: map[string]any{"metadata": map[string]any{"name": name}}}, nil
	})

	s := New(client, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		ClusterSelector:     "fencemaster/project-sync=true",
		FleetNamespace:      "fleet-default",
		ProjectIDsNamespace: "cattle-system",
		ProjectIDsName:      "rancher-project-ids",
		Resync:              time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	waitFor(t, "retry", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return calls >= 2
	})
}
