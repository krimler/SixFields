package watch_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sixfields/internal/fixture"
	"sixfields/internal/snapshot"
	"sixfields/internal/watch"
)

func obj(kind, name string, mutate ...func(snapshot.Object)) snapshot.Object {
	o := snapshot.Object{
		"apiVersion": "cluster.x-k8s.io/v1beta2",
		"kind":       kind,
		"metadata":   map[string]any{"name": name, "namespace": "default", "labels": map[string]any{}},
		"spec":       map[string]any{},
	}
	for _, m := range mutate {
		m(o)
	}
	return o
}

func labelled(cluster string) func(snapshot.Object) {
	return func(o snapshot.Object) {
		o["metadata"].(map[string]any)["labels"].(map[string]any)[watch.ClusterNameLabel] = cluster
	}
}

func ownedBy(kind, name string) func(snapshot.Object) {
	return func(o snapshot.Object) {
		o["metadata"].(map[string]any)["ownerReferences"] = []any{
			map[string]any{"apiVersion": "cluster.x-k8s.io/v1beta2", "kind": kind, "name": name},
		}
	}
}

func ref(field, kind, name string) func(snapshot.Object) {
	return func(o snapshot.Object) {
		o["spec"].(map[string]any)[field] = map[string]any{
			"kind": kind, "name": name, "apiGroup": "infrastructure.cluster.x-k8s.io",
		}
	}
}

func names(objs []snapshot.Object) []string {
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		out = append(out, o.Kind()+"/"+o.Name())
	}
	return out
}

// The envelope holds exactly the objects behind one cluster: what it references,
// what is labelled with its name, and what those own, and nothing else.
func TestWatch_ReachableSetIsExactlyOneCluster(t *testing.T) {
	objects := []snapshot.Object{
		obj("Cluster", "dev-1", ref("infrastructureRef", "DevCluster", "dev-1")),
		obj("DevCluster", "dev-1"),
		obj("KubeadmControlPlane", "dev-1-cp", labelled("dev-1")),
		obj("Machine", "dev-1-cp-a", ownedBy("KubeadmControlPlane", "dev-1-cp")),

		obj("Cluster", "other"),
		obj("DevCluster", "other"),
		obj("Machine", "other-cp-a", labelled("other")),
		obj("DevMachineTemplate", "unused"),
	}
	got := names(watch.Reachable(objects, "dev-1"))
	require.ElementsMatch(t, []string{
		"Cluster/dev-1", "DevCluster/dev-1", "KubeadmControlPlane/dev-1-cp", "Machine/dev-1-cp-a",
	}, got)
}

// An unlabelled object reached only through an owner reference still belongs: CAPD
// does not label every object it creates.
func TestWatch_OwnerReferencesPullInUnlabelledChildren(t *testing.T) {
	objects := []snapshot.Object{
		obj("Cluster", "dev-1"),
		obj("Machine", "dev-1-cp-a", labelled("dev-1")),
		obj("DevMachine", "dev-1-cp-a", ownedBy("Machine", "dev-1-cp-a")),
		obj("KubeadmConfig", "dev-1-cp-a", ownedBy("Machine", "dev-1-cp-a")),
	}
	require.ElementsMatch(t,
		[]string{"Cluster/dev-1", "Machine/dev-1-cp-a", "DevMachine/dev-1-cp-a", "KubeadmConfig/dev-1-cp-a"},
		names(watch.Reachable(objects, "dev-1")))
}

// A recorded fixture is already exactly one cluster's objects, so reachability
// over it is the identity. If that stops being true, either the recorder is
// picking up too much or the reachability rule has drifted from it.
func TestWatch_ReachableOverAFixtureIsTheWholeFixture(t *testing.T) {
	for _, tl := range fixture.All() {
		env := tl.Envelopes[len(tl.Envelopes)-1]
		cluster, ok := env.Cluster()
		require.True(t, ok, tl.Name)
		require.Len(t, watch.Reachable(env.Objects, cluster.Name()), len(env.Objects), tl.Name)
	}
}

// A ClusterResourceSetBinding carries no cluster label and is owned by the
// ClusterResourceSet, not by the Cluster. spec.clusterName is its only link, and
// missing it made the add-ons phase report "none" on a cluster that had just had
// a CNI applied.
func TestWatch_ResourceSetBindingIsReachedByClusterName(t *testing.T) {
	binding := obj("ClusterResourceSetBinding", "dev-1", func(o snapshot.Object) {
		o["apiVersion"] = "addons.cluster.x-k8s.io/v1beta2"
		o["spec"].(map[string]any)["clusterName"] = "dev-1"
	})
	other := obj("ClusterResourceSetBinding", "other", func(o snapshot.Object) {
		o["apiVersion"] = "addons.cluster.x-k8s.io/v1beta2"
		o["spec"].(map[string]any)["clusterName"] = "other"
	})
	got := names(watch.Reachable([]snapshot.Object{obj("Cluster", "dev-1"), binding, other}, "dev-1"))
	require.ElementsMatch(t, []string{"Cluster/dev-1", "ClusterResourceSetBinding/dev-1"}, got)
}
