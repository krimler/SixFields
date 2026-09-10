//go:build envtest

package watch_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"capi-distro/internal/envtest"
	"capi-distro/internal/watch"
)

func gvr(group, resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: group, Version: "v1beta2", Resource: resource}
}

func create(t *testing.T, client dynamic.Interface, resource schema.GroupVersionResource, obj map[string]any) *unstructured.Unstructured {
	t.Helper()
	created, err := client.Resource(resource).Namespace("default").
		Create(context.Background(), &unstructured.Unstructured{Object: obj}, metav1.CreateOptions{})
	require.NoError(t, err)
	return created
}

// The envelope contains exactly the objects behind one cluster and nothing else,
// against a real API server with real discovery.
func TestWatch_SnapshotIsExactlyOneCluster(t *testing.T) {
	config := envtest.Start(t)
	client := newClient(t, config)

	create(t, client, gvr("cluster.x-k8s.io", "clusters"), map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": "Cluster",
		"metadata": map[string]any{"name": "dev-1", "namespace": "default"},
		"spec": map[string]any{
			"controlPlaneRef": map[string]any{
				"kind": "KubeadmControlPlane", "name": "dev-1-cp",
				"apiGroup": "controlplane.cluster.x-k8s.io",
			},
		},
	})
	create(t, client, gvr("controlplane.cluster.x-k8s.io", "kubeadmcontrolplanes"), map[string]any{
		"apiVersion": "controlplane.cluster.x-k8s.io/v1beta2", "kind": "KubeadmControlPlane",
		"metadata": map[string]any{"name": "dev-1-cp", "namespace": "default"},
	})
	machine := create(t, client, gvr("cluster.x-k8s.io", "machines"), map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": "Machine",
		"metadata": map[string]any{
			"name": "dev-1-cp-a", "namespace": "default",
			"labels": map[string]any{watch.ClusterNameLabel: "dev-1"},
		},
	})
	// A child reached only through an owner reference, with no cluster label.
	create(t, client, gvr("infrastructure.cluster.x-k8s.io", "devmachines"), map[string]any{
		"apiVersion": "infrastructure.cluster.x-k8s.io/v1beta2", "kind": "DevMachine",
		"metadata": map[string]any{
			"name": "dev-1-cp-a", "namespace": "default",
			"ownerReferences": []any{map[string]any{
				"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": "Machine",
				"name": "dev-1-cp-a", "uid": string(machine.GetUID()),
			}},
		},
	})
	// Another cluster's machine, which must not appear.
	create(t, client, gvr("cluster.x-k8s.io", "machines"), map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": "Machine",
		"metadata": map[string]any{
			"name": "other-cp-a", "namespace": "default",
			"labels": map[string]any{watch.ClusterNameLabel: "other"},
		},
	})

	client2, err := watch.New(kubeconfigFor(t, config), "default")
	require.NoError(t, err)

	env, err := client2.Snapshot(context.Background(), "dev-1")
	require.NoError(t, err)

	var got []string
	for _, o := range env.Objects {
		got = append(got, o.Kind()+"/"+o.Name())
	}
	require.ElementsMatch(t, []string{
		"Cluster/dev-1", "KubeadmControlPlane/dev-1-cp", "Machine/dev-1-cp-a", "DevMachine/dev-1-cp-a",
	}, got)
}

// A change to any contributing object produces a new envelope, debounced.
func TestWatch_EmitsOnChange(t *testing.T) {
	config := envtest.Start(t)
	client := newClient(t, config)

	create(t, client, gvr("cluster.x-k8s.io", "clusters"), map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": "Cluster",
		"metadata": map[string]any{"name": "dev-1", "namespace": "default"},
	})

	watcher, err := watch.New(kubeconfigFor(t, config), "default")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshots, err := watcher.Watch(ctx, "dev-1", 100*time.Millisecond)
	require.NoError(t, err)

	create(t, client, gvr("cluster.x-k8s.io", "machines"), map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": "Machine",
		"metadata": map[string]any{
			"name": "dev-1-cp-a", "namespace": "default",
			"labels": map[string]any{watch.ClusterNameLabel: "dev-1"},
		},
	})

	for {
		select {
		case env := <-snapshots:
			if _, ok := env.Find("Machine", "dev-1-cp-a"); ok {
				return
			}
		case <-ctx.Done():
			t.Fatal("the watcher never reported the new Machine")
		}
	}
}

func newClient(t *testing.T, config *rest.Config) dynamic.Interface {
	t.Helper()
	client, err := dynamic.NewForConfig(config)
	require.NoError(t, err)
	return client
}

// A cluster that has settled produces no more events. Without a heartbeat the
// watcher goes silent and anything waiting on it waits for ever, which is how
// `cluster fixture record` hung on a cluster that was already ready.
func TestWatch_HeartbeatsWhenNothingChanges(t *testing.T) {
	config := envtest.Start(t)
	client := newClient(t, config)

	create(t, client, gvr("cluster.x-k8s.io", "clusters"), map[string]any{
		"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": "Cluster",
		"metadata": map[string]any{"name": "quiet", "namespace": "default"},
	})

	watcher, err := watch.New(kubeconfigFor(t, config), "default")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	snapshots, err := watcher.Watch(ctx, "quiet", 100*time.Millisecond)
	require.NoError(t, err)

	// Drain the initial burst, then wait for a beat with nothing changing.
	<-snapshots
	deadline := time.After(watch.Heartbeat * 3)
	select {
	case env := <-snapshots:
		_, ok := env.Cluster()
		require.True(t, ok, "a heartbeat envelope still describes the cluster")
	case <-deadline:
		t.Fatalf("no envelope within %s of silence", watch.Heartbeat*3)
	}
}
