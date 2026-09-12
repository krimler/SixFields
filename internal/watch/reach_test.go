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

// The escape hatch is only an escape if the file stands on its own. A rendered
// Cluster keeps spec.topology, so it points at a ClusterClass; without the class
// and the templates the class points at, the file depends on the very thing it
// is supposed to let you leave. A control study found all three arms of a
// control group shipping those objects and this tool omitting them.
func TestWatch_ClassAndItsTemplatesAreReachable(t *testing.T) {
	cluster := obj("Cluster", "dev-1", func(o snapshot.Object) {
		o["spec"].(map[string]any)["topology"] = map[string]any{
			"classRef": map[string]any{"name": "std-inmemory"},
		}
	})
	class := obj("ClusterClass", "std-inmemory", func(o snapshot.Object) {
		o["spec"].(map[string]any)["infrastructure"] = map[string]any{
			"templateRef": map[string]any{"kind": "DevClusterTemplate", "name": "std-inmemory-cluster"},
		}
		o["spec"].(map[string]any)["controlPlane"] = map[string]any{
			"templateRef":           map[string]any{"kind": "KubeadmControlPlaneTemplate", "name": "std-kubeadm-cp"},
			"machineInfrastructure": map[string]any{"templateRef": map[string]any{"kind": "DevMachineTemplate", "name": "std-inmemory-cp-machine"}},
		}
		o["spec"].(map[string]any)["workers"] = map[string]any{
			"machineDeployments": []any{map[string]any{
				"class":          "default",
				"bootstrap":      map[string]any{"templateRef": map[string]any{"kind": "KubeadmConfigTemplate", "name": "std-kubeadm-boot"}},
				"infrastructure": map[string]any{"templateRef": map[string]any{"kind": "DevMachineTemplate", "name": "std-inmemory-default-machine"}},
			}},
		}
	})
	objects := []snapshot.Object{
		cluster, class,
		obj("DevClusterTemplate", "std-inmemory-cluster"),
		obj("KubeadmControlPlaneTemplate", "std-kubeadm-cp"),
		obj("DevMachineTemplate", "std-inmemory-cp-machine"),
		obj("KubeadmConfigTemplate", "std-kubeadm-boot"),
		obj("DevMachineTemplate", "std-inmemory-default-machine"),
		// Another class in the same namespace, and its template. Neither belongs
		// to dev-1, and reaching the class must not mean reaching every class.
		obj("ClusterClass", "std-hosted"),
		obj("DevClusterTemplate", "std-docker-cluster"),
	}

	got := names(watch.Reachable(objects, "dev-1"))
	for _, want := range []string{
		"Cluster/dev-1", "ClusterClass/std-inmemory",
		"DevClusterTemplate/std-inmemory-cluster",
		"KubeadmControlPlaneTemplate/std-kubeadm-cp",
		"DevMachineTemplate/std-inmemory-cp-machine",
		"KubeadmConfigTemplate/std-kubeadm-boot",
		"DevMachineTemplate/std-inmemory-default-machine",
	} {
		require.Contains(t, got, want)
	}
	require.NotContains(t, got, "ClusterClass/std-hosted")
	require.NotContains(t, got, "DevClusterTemplate/std-docker-cluster")
}

// A ClusterClass's children are the templates it names, and nothing else. CAPI
// adds an owner reference to every template a class has ever referenced and does
// not remove it when the class stops referencing it: on a live management cluster
// std-control-plane-machine was owned by both `std` and `std-inmemory`, though
// only `std` still named it. Expanding owner references from a class therefore
// pulled another class's templates into the render.
func TestWatch_StaleClassOwnershipDoesNotWidenTheRender(t *testing.T) {
	cluster := obj("Cluster", "dev-1", func(o snapshot.Object) {
		o["spec"].(map[string]any)["topology"] = map[string]any{
			"classRef": map[string]any{"name": "std-inmemory"},
		}
	})
	class := obj("ClusterClass", "std-inmemory", func(o snapshot.Object) {
		o["spec"].(map[string]any)["infrastructure"] = map[string]any{
			"templateRef": map[string]any{"kind": "DevClusterTemplate", "name": "std-inmemory-cluster"},
		}
	})
	objects := []snapshot.Object{
		cluster, class,
		obj("DevClusterTemplate", "std-inmemory-cluster"),
		// Named by the docker class, still carrying a stale owner reference from
		// std-inmemory because CAPI never took it off.
		obj("DevMachineTemplate", "std-control-plane-machine", ownedBy("ClusterClass", "std-inmemory")),
	}

	got := names(watch.Reachable(objects, "dev-1"))
	require.Contains(t, got, "DevClusterTemplate/std-inmemory-cluster")
	require.NotContains(t, got, "DevMachineTemplate/std-control-plane-machine")
}

// A ClusterResourceSetBinding records which resource sets have been applied to a
// cluster; the set itself is what defines them. Rendering the binding without the
// set leaves a dangling reference, which is how three readers of a rendered file
// independently described it: the add-on would not be reinstalled from this file.
// The set carries no cluster label and does not own the binding, so only
// spec.bindings[].clusterResourceSetName connects them.
func TestWatch_ResourceSetBehindABindingIsReachable(t *testing.T) {
	binding := obj("ClusterResourceSetBinding", "dev-1", func(o snapshot.Object) {
		o["spec"].(map[string]any)["clusterName"] = "dev-1"
		o["spec"].(map[string]any)["bindings"] = []any{
			map[string]any{"clusterResourceSetName": "calico"},
		}
	})
	objects := []snapshot.Object{
		obj("Cluster", "dev-1"),
		binding,
		obj("ClusterResourceSet", "calico", func(o snapshot.Object) {
			o["spec"].(map[string]any)["resources"] = []any{
				map[string]any{"kind": "ConfigMap", "name": "calico-v3.32.2"},
			}
		}),
		obj("ClusterResourceSet", "cilium"), // applied to some other cluster
		// One resource set applies to every cluster and owns every binding. Those
		// bindings belong to other clusters, and following the ownership out of
		// the set pulled all of them in: on a live cluster a render went from 24
		// objects to 37, the extra thirteen being other clusters' bindings.
		obj("ClusterResourceSetBinding", "dev-2", ownedBy("ClusterResourceSet", "calico"),
			func(o snapshot.Object) { o["spec"].(map[string]any)["clusterName"] = "dev-2" }),
	}

	got := names(watch.Reachable(objects, "dev-1"))
	require.Contains(t, got, "ClusterResourceSet/calico")
	require.NotContains(t, got, "ClusterResourceSet/cilium")
	require.NotContains(t, got, "ClusterResourceSetBinding/dev-2")
}
