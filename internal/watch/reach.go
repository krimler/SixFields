// Package watch assembles the same envelope the fixtures hold, from a live
// management cluster. Everything above it — fold, why, eta, render — cannot tell
// the difference, which is why a replayed fixture is a real test of the CLI.
package watch

import (
	"sort"

	"capi-distro/internal/snapshot"
)

// ClusterNameLabel is how CAPI marks every object it creates for a cluster. The
// name is from the pinned release (docs/api-snapshot.md, "(labels)").
const ClusterNameLabel = "cluster.x-k8s.io/cluster-name"

// Reachable returns the objects that belong to one cluster: the Cluster itself,
// everything it references, everything labelled with its name, and everything
// owned transitively by any of those. Anything else in the namespace — another
// cluster's machines, a stray template — is excluded.
//
// Pure, so the rule "exactly the reachable set and nothing else" is a unit test
// rather than an envtest.
func Reachable(objects []snapshot.Object, clusterName string) []snapshot.Object {
	byKey := map[string]snapshot.Object{}
	for _, o := range objects {
		byKey[key(o.Kind(), o.Name())] = o
	}

	included := map[string]bool{}
	var queue []snapshot.Object

	visit := func(o snapshot.Object) {
		k := key(o.Kind(), o.Name())
		if included[k] {
			return
		}
		included[k] = true
		queue = append(queue, o)
	}

	for _, o := range objects {
		if o.Kind() == "Cluster" && o.Name() == clusterName {
			visit(o)
		}
	}
	// Labelled objects are the bulk of it: CAPI puts the cluster name on
	// everything the topology creates.
	for _, o := range objects {
		if o.Labels()[ClusterNameLabel] == clusterName {
			visit(o)
		}
	}

	for i := 0; i < len(queue); i++ {
		current := queue[i]
		for _, path := range refPaths {
			ref, ok := current.ContractRef(path...)
			if !ok {
				continue
			}
			if child, ok := byKey[key(ref.Kind, ref.Name)]; ok {
				visit(child)
			}
		}
		// Owner references point the other way, so this pass picks up children
		// that carry no cluster label.
		for _, candidate := range objects {
			for _, owner := range candidate.OwnerRefs() {
				if owner.Kind == current.Kind() && owner.Name == current.Name() {
					visit(candidate)
				}
			}
		}
	}

	out := make([]snapshot.Object, 0, len(queue))
	out = append(out, queue...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind() != out[j].Kind() {
			return out[i].Kind() < out[j].Kind()
		}
		return out[i].Name() < out[j].Name()
	})
	return out
}

// refPaths are the contract references CAPI objects use to point at each other.
// Every one is read from the pinned types (docs/api-snapshot.md).
var refPaths = [][]string{
	{"spec", "infrastructureRef"},
	{"spec", "controlPlaneRef"},
	{"spec", "bootstrapRef"},
	{"spec", "template", "spec", "infrastructureRef"},
	{"spec", "template", "spec", "bootstrap", "configRef"},
}

func key(kind, name string) string { return kind + "/" + name }
