// Package fixture builds snapshot envelopes for scenarios that cannot be recorded
// from a live cluster yet.
//
// CLAUDE.md rule 4 says fixtures are recorded, not written. These are the
// exception, and they declare it: every envelope this package produces carries
// _meta.synthetic and a note saying what it stands in for, and
// TestFixtures_SyntheticAreDeclared fails on one that does not. When a container
// runtime is available, `make record-fixture` replaces them scenario by scenario
// and the assertions do not change — that is the whole point of folding from an
// envelope rather than from a client.
package fixture

import (
	"fmt"
	"time"

	"capi-distro/internal/snapshot"
)

const (
	capiVersion = "v1.14.2"
	namespace   = "default"

	coreGroup      = "cluster.x-k8s.io"
	coreAPI        = "cluster.x-k8s.io/v1beta2"
	cpAPI          = "controlplane.cluster.x-k8s.io/v1beta2"
	bootstrapAPI   = "bootstrap.cluster.x-k8s.io/v1beta2"
	infraAPI       = "infrastructure.cluster.x-k8s.io/v1beta2"
	addonsAPI      = "addons.cluster.x-k8s.io/v1beta2"
	clusterNameKey = "cluster.x-k8s.io/cluster-name"
	ownedKey       = "topology.cluster.x-k8s.io/owned"
)

// T0 is the instant every synthetic timeline starts at. Fixed so goldens are
// stable and a replay is reproducible.
var T0 = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

type object struct {
	o snapshot.Object
}

func newObject(apiVersion, kind, name string) *object {
	return &object{o: snapshot.Object{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name":              name,
			"namespace":         namespace,
			"uid":               fmt.Sprintf("uid-%s-%s", kind, name),
			"creationTimestamp": T0.Format(time.RFC3339),
			"labels":            map[string]any{},
		},
		"spec":   map[string]any{},
		"status": map[string]any{},
	}}
}

func (b *object) label(k, v string) *object {
	b.o["metadata"].(map[string]any)["labels"].(map[string]any)[k] = v
	return b
}

func (b *object) owned(cluster string) *object {
	return b.label(clusterNameKey, cluster).label(ownedKey, "")
}

func (b *object) ownedBy(kind, name string) *object {
	meta := b.o["metadata"].(map[string]any)
	refs, _ := meta["ownerReferences"].([]any)
	meta["ownerReferences"] = append(refs, map[string]any{
		"apiVersion": ownerAPIVersion(kind),
		"kind":       kind,
		"name":       name,
		"uid":        fmt.Sprintf("uid-%s-%s", kind, name),
	})
	return b
}

func ownerAPIVersion(kind string) string {
	switch kind {
	case "KubeadmControlPlane", "K0sControlPlane", "K0smotronControlPlane":
		return cpAPI
	case "DevCluster", "DevMachine":
		return infraAPI
	default:
		return coreAPI
	}
}

func (b *object) spec(path string, value any) *object {
	b.o["spec"].(map[string]any)[path] = value
	return b
}

func (b *object) status(path string, value any) *object {
	b.o["status"].(map[string]any)[path] = value
	return b
}

// condition adds or replaces a condition. at is when it last changed, which is the
// only input stall detection has.
func (b *object) condition(condType, status, reason, message string, at time.Time) *object {
	st := b.o["status"].(map[string]any)
	conds, _ := st["conditions"].([]any)
	entry := map[string]any{
		"type":               condType,
		"status":             status,
		"reason":             reason,
		"lastTransitionTime": at.Format(time.RFC3339),
	}
	if message != "" {
		entry["message"] = message
	}
	for i, c := range conds {
		if snapshot.Object(c.(map[string]any)).String("type") == condType {
			conds[i] = entry
			st["conditions"] = conds
			return b
		}
	}
	st["conditions"] = append(conds, entry)
	return b
}

func (b *object) ref(field, kind, name, group string) *object {
	return b.spec(field, map[string]any{"kind": kind, "name": name, "apiGroup": group})
}

// scenario collects the objects of one cluster and stamps a timeline out of them.
type scenario struct {
	name    string
	note    string
	cluster string
	objects []*object
}

func (s *scenario) add(o *object) *object {
	s.objects = append(s.objects, o)
	return o
}

func (s *scenario) envelope(at time.Time) snapshot.Envelope {
	objs := make([]snapshot.Object, 0, len(s.objects))
	for _, o := range s.objects {
		objs = append(objs, deepCopy(o.o))
	}
	return snapshot.Envelope{
		Meta: snapshot.Meta{
			Scenario:  s.name,
			TPlusS:    int(at.Sub(T0).Seconds()),
			CAPI:      capiVersion,
			Note:      s.note,
			Synthetic: true,
		},
		Objects: objs,
	}
}

func deepCopy(o snapshot.Object) snapshot.Object {
	out := make(snapshot.Object, len(o))
	for k, v := range o {
		out[k] = deepCopyValue(v)
	}
	return out
}

func deepCopyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = deepCopyValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = deepCopyValue(vv)
		}
		return out
	default:
		return v
	}
}
