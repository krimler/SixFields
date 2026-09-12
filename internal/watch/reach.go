// Package watch assembles the same envelope the fixtures hold, from a live
// management cluster. Everything above it, fold, why, eta, render, cannot tell
// the difference, which is why a replayed fixture is a real test of the CLI.
package watch

import (
	"sort"

	"sixfields/internal/snapshot"
)

// ClusterNameLabel is how CAPI marks every object it creates for a cluster. The
// name is from the pinned release (docs/api-snapshot.md, "(labels)").
const ClusterNameLabel = "cluster.x-k8s.io/cluster-name"

// Reachable returns the objects that belong to one cluster: the Cluster itself,
// everything it references, everything labelled with its name, and everything
// owned transitively by any of those. Anything else in the namespace, another
// cluster's machines, a stray template, is excluded.
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
	// A ClusterResourceSetBinding carries no cluster label and is owned by the
	// ClusterResourceSet rather than by the Cluster; spec.clusterName is its only
	// link. Without this the add-ons phase reported "none" on a cluster that had
	// just had a CNI applied to it.
	for _, o := range objects {
		if o.String("spec", "clusterName") == clusterName {
			visit(o)
		}
	}

	for i := 0; i < len(queue); i++ {
		current := queue[i]
		// A Cluster points at its ClusterClass by name, not by a contract
		// reference, and a ClusterClass points at its templates the same way. They
		// are followed here so that `cluster render` produces a file that stands on
		// its own: the rendered Cluster keeps spec.topology, so without the class
		// and its templates the file depends on the thing it is meant to let you
		// leave.
		if current.Kind() == "Cluster" {
			if name := current.String("spec", "topology", "classRef", "name"); name != "" {
				if class, ok := byKey[key("ClusterClass", name)]; ok {
					visit(class)
				}
			}
		}
		if current.Kind() == "ClusterClass" {
			for _, t := range classTemplates(current) {
				if tmpl, ok := byKey[key(t.Kind, t.Name)]; ok {
					visit(tmpl)
				}
			}
		}
		// A binding records what has been applied; the set defines it. The set
		// carries no cluster label and does not own the binding, so this name is
		// the only link, and without it a rendered file has a binding pointing at
		// nothing.
		if current.Kind() == "ClusterResourceSetBinding" {
			bindings, _ := current.Slice("spec", "bindings")
			for _, item := range bindings {
				entry, ok := item.(map[string]any)
				if !ok {
					continue
				}
				name := snapshot.Object(entry).String("clusterResourceSetName")
				if set, ok := byKey[key("ClusterResourceSet", name)]; ok && name != "" {
					visit(set)
				}
			}
		}
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
		// that carry no cluster label. Not from a shared definition: those are
		// reached because one cluster uses them, and their children belong to
		// every cluster that does.
		if sharedDefinitions[current.Kind()] {
			continue
		}
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

// sharedDefinitions are the objects a cluster refers to rather than owns. One
// belongs to many clusters, so its owner references lead away from the cluster
// being rendered and must not be followed.
//
// Both were found by rendering a live cluster. CAPI adds an owner reference to
// every template a ClusterClass has ever named and never removes it, so
// std-control-plane-machine was owned by both `std` and `std-inmemory` and the
// docker class's templates appeared in an in-memory cluster's file. A
// ClusterResourceSet owns the binding of every cluster it applies to, and
// following those took a 24-object render to 37.
var sharedDefinitions = map[string]bool{
	"ClusterClass":       true,
	"ClusterResourceSet": true,
}

func key(kind, name string) string { return kind + "/" + name }

// classTemplates are the templates a ClusterClass names. Every one is a
// templateRef, which carries kind and name and no namespace, because a class and
// its templates live together (api@v1.14.2 core/v1beta2/clusterclass_types.go).
func classTemplates(class snapshot.Object) []snapshot.Ref {
	var out []snapshot.Ref
	add := func(m map[string]any) {
		o := snapshot.Object(m)
		if ref, ok := o.ContractRef("templateRef"); ok {
			out = append(out, ref)
		}
	}
	spec, ok := class.Map("spec")
	if !ok {
		return nil
	}
	for _, field := range []string{"infrastructure", "controlPlane"} {
		if m, ok := spec[field].(map[string]any); ok {
			add(m)
			if mi, ok := m["machineInfrastructure"].(map[string]any); ok {
				add(mi)
			}
		}
	}
	workers, _ := spec["workers"].(map[string]any)
	for _, backend := range []string{"machineDeployments", "machinePools"} {
		items, _ := workers[backend].([]any)
		for _, item := range items {
			pool, ok := item.(map[string]any)
			if !ok {
				continue
			}
			for _, field := range []string{"bootstrap", "infrastructure"} {
				if m, ok := pool[field].(map[string]any); ok {
					add(m)
				}
			}
		}
	}
	return out
}
