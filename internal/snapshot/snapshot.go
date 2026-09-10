// Package snapshot is the data the whole tool is built on: one JSON envelope
// holding every CAPI object behind a cluster at one instant. `internal/watch`
// assembles it from a live cluster and `cluster fixture record` writes it to
// disk, so fold, why and eta see the same bytes in tests and in production.
//
// Pure: encoding/json and time only. No Kubernetes client reaches this package.
package snapshot

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Envelope is one snapshot. The JSON form is the fixture format in CLAUDE.md.
type Envelope struct {
	Meta    Meta     `json:"_meta"`
	Objects []Object `json:"objects"`
}

type Meta struct {
	Scenario string `json:"scenario"`
	TPlusS   int    `json:"t_plus_s"`
	CAPI     string `json:"capi"`
	Note     string `json:"note"`
	// Synthetic marks an envelope that was generated rather than recorded from a
	// real run. CLAUDE.md rule 4 wants recorded fixtures; a synthetic one must say
	// so and say what it stands in for, and TestFixtures_SyntheticAreDeclared fails
	// if it does not.
	Synthetic  bool   `json:"synthetic,omitempty"`
	RecordedAt string `json:"recorded_at,omitempty"`
}

// Object is one Kubernetes object as JSON. Untyped on purpose: the tool reads a
// dozen kinds from three providers and owns none of their schemas.
type Object map[string]any

// Ref identifies an object the way a user sees it in a stall line.
type Ref struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Group     string `json:"group,omitempty"`
}

func (r Ref) String() string { return r.Kind + "/" + r.Name }

// Kubectl is the copy-pasteable command behind every stall line. The escape
// hatch is a feature: this is how a user leaves the abstraction.
func (r Ref) Kubectl() string {
	resource := strings.ToLower(r.Kind)
	if r.Group != "" {
		resource += "." + r.Group
	}
	cmd := fmt.Sprintf("kubectl get %s %s", resource, r.Name)
	if r.Namespace != "" {
		cmd += " -n " + r.Namespace
	}
	return cmd + " -o yaml"
}

// Condition is a condition from either API generation, normalised. Deprecated is
// true when it was read from status.deprecated.v1beta1.conditions, which is where
// CAPI v1.11+ keeps the old ones (docs/api-snapshot.md).
type Condition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	Severity           string    `json:"severity,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime,omitzero"`
	Deprecated         bool      `json:"deprecated,omitempty"`
}

func (c Condition) IsTrue() bool  { return c.Status == "True" }
func (c Condition) IsFalse() bool { return c.Status == "False" }

func (o Object) APIVersion() string { return o.String("apiVersion") }
func (o Object) Kind() string       { return o.String("kind") }
func (o Object) Name() string       { return o.String("metadata", "name") }
func (o Object) Namespace() string  { return o.String("metadata", "namespace") }
func (o Object) UID() string        { return o.String("metadata", "uid") }

// Group is the API group without the version: cluster.x-k8s.io, not
// cluster.x-k8s.io/v1beta2.
func (o Object) Group() string {
	group, _, _ := strings.Cut(o.APIVersion(), "/")
	if !strings.Contains(o.APIVersion(), "/") {
		return ""
	}
	return group
}

func (o Object) Ref() Ref {
	return Ref{Kind: o.Kind(), Name: o.Name(), Namespace: o.Namespace(), Group: o.Group()}
}

func (o Object) CreationTimestamp() time.Time { return o.Time("metadata", "creationTimestamp") }

// Conditions returns status.conditions, or the deprecated v1beta1 block when the
// object has no v1beta2 conditions. Callers get one shape either way; whether a
// provider is on the old contract stays inside this function.
func (o Object) Conditions() []Condition {
	if cs := o.conditionsAt("status", "conditions"); len(cs) > 0 {
		return cs
	}
	cs := o.conditionsAt("status", "deprecated", "v1beta1", "conditions")
	for i := range cs {
		cs[i].Deprecated = true
	}
	return cs
}

func (o Object) conditionsAt(path ...string) []Condition {
	raw, ok := o.Slice(path...)
	if !ok {
		return nil
	}
	out := make([]Condition, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		c := Object(m)
		out = append(out, Condition{
			Type:               c.String("type"),
			Status:             c.String("status"),
			Reason:             c.String("reason"),
			Message:            c.String("message"),
			Severity:           c.String("severity"),
			LastTransitionTime: c.Time("lastTransitionTime"),
		})
	}
	return out
}

// Condition returns the named condition. The second result distinguishes "absent"
// from "present and Unknown", which is the difference between a phase that has not
// started and one that is running.
func (o Object) Condition(name string) (Condition, bool) {
	for _, c := range o.Conditions() {
		if c.Type == name {
			return c, true
		}
	}
	return Condition{}, false
}

// LastTransition is the most recent condition transition on this object. Stall
// detection is "nothing transitioned for longer than stallAfter".
func (o Object) LastTransition() time.Time {
	var latest time.Time
	for _, c := range o.Conditions() {
		if c.LastTransitionTime.After(latest) {
			latest = c.LastTransitionTime
		}
	}
	return latest
}

func (o Object) String(path ...string) string {
	v, ok := o.get(path...)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Int reads a number that may have arrived as a JSON float, an int64 from a
// decoder configured for numbers, or a string.
func (o Object) Int(path ...string) (int64, bool) {
	v, ok := o.get(path...)
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

func (o Object) Bool(path ...string) (bool, bool) {
	v, ok := o.get(path...)
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func (o Object) Time(path ...string) time.Time {
	s := o.String(path...)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (o Object) Slice(path ...string) ([]any, bool) {
	v, ok := o.get(path...)
	if !ok {
		return nil, false
	}
	s, ok := v.([]any)
	return s, ok
}

func (o Object) Map(path ...string) (Object, bool) {
	v, ok := o.get(path...)
	if !ok {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return Object(m), ok
}

func (o Object) Labels() map[string]string      { return o.stringMap("metadata", "labels") }
func (o Object) Annotations() map[string]string { return o.stringMap("metadata", "annotations") }

func (o Object) stringMap(path ...string) map[string]string {
	m, ok := o.Map(path...)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// ContractRef reads a reference the CAPI contract defines, e.g. spec.controlPlaneRef.
// v1beta2 refs carry apiGroup and kind; v1beta1 refs carried apiVersion.
func (o Object) ContractRef(path ...string) (Ref, bool) {
	m, ok := o.Map(path...)
	if !ok {
		return Ref{}, false
	}
	kind, name := m.String("kind"), m.String("name")
	if kind == "" || name == "" {
		return Ref{}, false
	}
	group := m.String("apiGroup")
	if group == "" {
		group, _, _ = strings.Cut(m.String("apiVersion"), "/")
	}
	ns := m.String("namespace")
	if ns == "" {
		ns = o.Namespace()
	}
	return Ref{Kind: kind, Name: name, Namespace: ns, Group: group}, true
}

func (o Object) OwnerRefs() []Ref {
	raw, ok := o.Slice("metadata", "ownerReferences")
	if !ok {
		return nil
	}
	out := make([]Ref, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		owner := Object(m)
		group, _, _ := strings.Cut(owner.String("apiVersion"), "/")
		out = append(out, Ref{Kind: owner.String("kind"), Name: owner.String("name"), Namespace: o.Namespace(), Group: group})
	}
	return out
}

func (o Object) get(path ...string) (any, bool) {
	var cur any = map[string]any(o)
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[key]
		if !ok || cur == nil {
			return nil, false
		}
	}
	return cur, true
}

// Find returns the object with this kind and name, or false. Kind is enough to
// disambiguate inside one cluster's object set.
func (e Envelope) Find(kind, name string) (Object, bool) {
	for _, o := range e.Objects {
		if o.Kind() == kind && o.Name() == name {
			return o, true
		}
	}
	return Object{}, false
}

func (e Envelope) FindRef(r Ref) (Object, bool) {
	for _, o := range e.Objects {
		if o.Kind() == r.Kind && o.Name() == r.Name && (r.Namespace == "" || o.Namespace() == r.Namespace) {
			return o, true
		}
	}
	return Object{}, false
}

// ByKind returns every object of a kind, ordered by name so folding is
// deterministic whatever order the informers delivered them in.
func (e Envelope) ByKind(kinds ...string) []Object {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	var out []Object
	for _, o := range e.Objects {
		if want[o.Kind()] {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Cluster returns the one Cluster the envelope is about.
func (e Envelope) Cluster() (Object, bool) {
	for _, o := range e.Objects {
		if o.Kind() == "Cluster" && o.Group() == "cluster.x-k8s.io" {
			return o, true
		}
	}
	return Object{}, false
}

// OwnedBy returns objects whose ownerReferences include ref.
func (e Envelope) OwnedBy(ref Ref) []Object {
	var out []Object
	for _, o := range e.Objects {
		for _, owner := range o.OwnerRefs() {
			if owner.Kind == ref.Kind && owner.Name == ref.Name {
				out = append(out, o)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
