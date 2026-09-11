//go:build envtest

package tests_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	"sixfields/internal/envtest"
)

// The CEL table above evaluates the policy's expressions. This one hands the same
// YAML to a real API server: it proves the expressions compile under the server's
// own type checker, that the binding wires them up, and that a denial reaches a
// client as a message rather than as a panic.
func TestPolicy_RealAPIServerAdmitsAndDenies(t *testing.T) {
	config := envtest.Start(t)
	client, err := dynamic.NewForConfig(config)
	require.NoError(t, err)

	applyPolicies(t, client)

	t.Run("admits the six-field Cluster", func(t *testing.T) {
		err := create(client, clustersGVR, cluster())
		require.NoError(t, err, "the policy rejected a Cluster a user is allowed to write")
	})

	t.Run("denies a managed field", func(t *testing.T) {
		obj := cluster(deniedPaths["spec.clusterNetwork"])
		obj["metadata"].(map[string]any)["name"] = "denied-1"
		err := create(client, clustersGVR, obj)
		require.Error(t, err)
		require.True(t, apierrors.IsForbidden(err), "expected Forbidden, got %v", err)
		require.Contains(t, err.Error(), "ClusterClass 'std'")
		require.Contains(t, err.Error(), "break-glass")
	})

	t.Run("denies a hand-written KubeadmControlPlane", func(t *testing.T) {
		err := create(client, kcpGVR, map[string]any{
			"apiVersion": "controlplane.cluster.x-k8s.io/v1beta2",
			"kind":       "KubeadmControlPlane",
			"metadata":   map[string]any{"name": "hand-written", "namespace": "default"},
			"spec":       map[string]any{"replicas": int64(1)},
		})
		require.Error(t, err)
		require.True(t, apierrors.IsForbidden(err), "expected Forbidden, got %v", err)
		require.Contains(t, err.Error(), "KubeadmControlPlane")
		require.Contains(t, err.Error(), "break-glass")
	})
}

var (
	clustersGVR = schema.GroupVersionResource{Group: "cluster.x-k8s.io", Version: "v1beta2", Resource: "clusters"}
	kcpGVR      = schema.GroupVersionResource{
		Group: "controlplane.cluster.x-k8s.io", Version: "v1beta2", Resource: "kubeadmcontrolplanes",
	}
	vapGVR = schema.GroupVersionResource{
		Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingadmissionpolicies",
	}
	vapBindingGVR = schema.GroupVersionResource{
		Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingadmissionpolicybindings",
	}
)

func create(client dynamic.Interface, resource schema.GroupVersionResource, obj map[string]any) error {
	_, err := client.Resource(resource).Namespace("default").
		Create(context.Background(), &unstructured.Unstructured{Object: obj}, metav1.CreateOptions{})
	return err
}

// applyPolicies applies exactly the files in policy/vap, so a policy that does not
// compile fails here rather than on someone's cluster.
func applyPolicies(t *testing.T, client dynamic.Interface) {
	t.Helper()
	entries, err := os.ReadDir(vapDir)
	require.NoError(t, err)

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(vapDir, entry.Name()))
		require.NoError(t, err)

		for _, doc := range strings.Split(string(b), "\n---\n") {
			if strings.TrimSpace(doc) == "" {
				continue
			}
			var obj map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(doc), &obj))
			resource := vapGVR
			if obj["kind"] == "ValidatingAdmissionPolicyBinding" {
				resource = vapBindingGVR
			}
			_, err := client.Resource(resource).
				Create(context.Background(), &unstructured.Unstructured{Object: obj}, metav1.CreateOptions{})
			require.NoError(t, err, "%s: %v", entry.Name(), obj["metadata"])
		}
	}
	waitForPolicies(t, client)
}

// A ValidatingAdmissionPolicy is compiled and its bindings indexed asynchronously.
// Without this wait the first Create races the controller and the test is flaky.
func waitForPolicies(t *testing.T, client dynamic.Interface) {
	t.Helper()
	for i := 0; i < 100; i++ {
		err := create(client, clustersGVR, cluster(deniedPaths["spec.paused"], func(o map[string]any) {
			o["metadata"].(map[string]any)["name"] = "policy-warmup"
		}))
		if apierrors.IsForbidden(err) {
			return
		}
		if err == nil {
			_ = client.Resource(clustersGVR).Namespace("default").
				Delete(context.Background(), "policy-warmup", metav1.DeleteOptions{})
		}
		sleep()
	}
	t.Fatal("the admission policy never started enforcing")
}

func sleep() { time.Sleep(100 * time.Millisecond) }
