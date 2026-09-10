// Package fold turns a snapshot of every CAPI object behind a cluster into four
// phases a person can read. It is the answer to "a long opaque wait".
//
// Pure: it takes an envelope and returns values. No Kubernetes client, no clock —
// the caller passes Now, so a replay at 50x and a live run fold identically.
package fold

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"capi-distro/internal/snapshot"
)

type PhaseName string

const (
	Infrastructure PhaseName = "infrastructure"
	ControlPlane   PhaseName = "control plane"
	Workers        PhaseName = "workers"
	Addons         PhaseName = "addons"
)

// Order is the order phases are shown and the order they normally complete in.
var Order = []PhaseName{Infrastructure, ControlPlane, Workers, Addons}

type State string

const (
	Pending State = "pending"
	Running State = "running"
	Done    State = "done"
	Stalled State = "stalled"
)

type Phase struct {
	Name  PhaseName `json:"name"`
	State State     `json:"state"`
	// Detail is the one thing worth showing next to the phase: "2/3 nodes",
	// "hosted", "no workers".
	Detail string `json:"detail"`
	// Elapsed is how long this phase has been running, or how long it took.
	Elapsed time.Duration `json:"elapsed_ns"`
	// LastTransition is the most recent condition change on any contributing
	// object. Stall detection is entirely about this field.
	LastTransition time.Time      `json:"last_transition,omitzero"`
	StartedAt      time.Time      `json:"started_at,omitzero"`
	Contributors   []snapshot.Ref `json:"contributors"`
}

type Result struct {
	Cluster   snapshot.Ref  `json:"cluster"`
	Class     string        `json:"class,omitempty"`
	Version   string        `json:"version,omitempty"`
	Placement string        `json:"placement,omitempty"`
	Phases    []Phase       `json:"phases"`
	Ready     bool          `json:"ready"`
	At        time.Time     `json:"at"`
	Elapsed   time.Duration `json:"elapsed_ns"`
}

type Options struct {
	// Now is the instant the snapshot is being read at. Fixtures set it from
	// _meta.t_plus_s so a recorded timeline replays deterministically.
	Now time.Time
	// StallAfter is how long a running phase may go without any contributing
	// object changing a condition before it is called stalled.
	StallAfter time.Duration
}

const DefaultStallAfter = 3 * time.Minute

func (o Options) stallAfter() time.Duration {
	if o.StallAfter <= 0 {
		return DefaultStallAfter
	}
	return o.StallAfter
}

// Phase returns the named phase.
func (r Result) Phase(name PhaseName) (Phase, bool) {
	for _, p := range r.Phases {
		if p.Name == name {
			return p, true
		}
	}
	return Phase{}, false
}

// Stalled returns the first stalled phase, which is the only one worth naming.
func (r Result) Stalled() (Phase, bool) {
	for _, p := range r.Phases {
		if p.State == Stalled {
			return p, true
		}
	}
	return Phase{}, false
}

func Fold(env snapshot.Envelope, opt Options) Result {
	cluster, ok := env.Cluster()
	if !ok {
		return Result{At: opt.Now}
	}
	res := Result{
		Cluster: cluster.Ref(),
		// v1beta2 spells this spec.topology.classRef.{name,namespace}; the
		// v1beta1 spelling (spec.topology.class) is gone
		// (api@v1.14.2 core/v1beta2/cluster_types.go, Topology.ClassRef).
		Class:     cluster.String("spec", "topology", "classRef", "name"),
		Version:   cluster.String("spec", "topology", "version"),
		Placement: placement(cluster),
		At:        opt.Now,
	}
	start := cluster.CreationTimestamp()
	if !start.IsZero() && !opt.Now.IsZero() {
		res.Elapsed = opt.Now.Sub(start)
	}

	phases := []Phase{
		foldInfrastructure(env, cluster),
		foldControlPlane(env, cluster),
		foldWorkers(env, cluster),
		foldAddons(env, cluster),
	}

	// A phase starts when the one before it finishes; the first starts with the
	// Cluster. That is the only way to get per-phase durations out of a single
	// snapshot, and it is what internal/eta stores.
	prevEnd := start
	for i := range phases {
		p := &phases[i]
		p.StartedAt = prevEnd
		end := opt.Now
		if p.State == Done && !p.LastTransition.IsZero() {
			end = p.LastTransition
			prevEnd = end
		}
		if !p.StartedAt.IsZero() && !end.IsZero() && end.After(p.StartedAt) {
			p.Elapsed = end.Sub(p.StartedAt)
		}
		// Only the frontier phase can stall. Workers sitting at 0/2 while the
		// control plane is stuck are waiting, not stuck, and naming them would
		// point the user at the wrong object.
		if p.State == Running && i == frontier(phases) && stalled(*p, opt) {
			p.State = Stalled
		}
	}
	res.Phases = phases
	res.Ready = allDone(phases)
	return res
}

// frontier is the index of the earliest phase that is not done: the one thing the
// cluster is actually waiting on.
func frontier(phases []Phase) int {
	for i, p := range phases {
		if p.State != Done {
			return i
		}
	}
	return len(phases)
}

func stalled(p Phase, opt Options) bool {
	if p.LastTransition.IsZero() || opt.Now.IsZero() {
		return false
	}
	return opt.Now.Sub(p.LastTransition) > opt.stallAfter()
}

func allDone(phases []Phase) bool {
	for _, p := range phases {
		if p.State != Done {
			return false
		}
	}
	return true
}

func placement(cluster snapshot.Object) string {
	vars, ok := cluster.Slice("spec", "topology", "variables")
	if !ok {
		return ""
	}
	for _, item := range vars {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		v := snapshot.Object(m)
		if v.String("name") != "placement" {
			continue
		}
		if s, ok := v.Map("value"); ok {
			_ = s
		}
		if s := v.String("value"); s != "" {
			return s
		}
	}
	return ""
}

func foldInfrastructure(env snapshot.Envelope, cluster snapshot.Object) Phase {
	p := Phase{Name: Infrastructure, State: Pending, Detail: "waiting"}
	contributors := []snapshot.Object{cluster}
	var seen clock
	seen.only(cluster, clusterInfrastructureReady, clusterTopologyReconciled)
	if ref, ok := cluster.ContractRef("spec", "infrastructureRef"); ok {
		p.Contributors = append(p.Contributors, ref)
		if infra, ok := env.FindRef(ref); ok {
			contributors = append(contributors, infra)
			seen.all(infra)
			p.Detail = ref.Kind
		}
	}
	p.LastTransition = seen.latest

	if provisioned, ok := cluster.Bool("status", "initialization", "infrastructureProvisioned"); ok && provisioned {
		p.State, p.Detail = Done, "ready"
		return p
	}
	c, ok := cluster.Condition(clusterInfrastructureReady)
	if !ok {
		return p
	}
	switch {
	case c.IsTrue():
		p.State, p.Detail = Done, "ready"
	default:
		p.State = Running
		p.Detail = detailFrom(contributors[len(contributors)-1], c)
	}
	return p
}

func foldControlPlane(env snapshot.Envelope, cluster snapshot.Object) Phase {
	p := Phase{Name: ControlPlane, State: Pending, Detail: "waiting"}
	contributors := []snapshot.Object{cluster}

	var seen clock
	seen.only(cluster, clusterControlPlaneInit, clusterControlPlaneAvailable)

	cpRef, hasRef := cluster.ContractRef("spec", "controlPlaneRef")
	var cp snapshot.Object
	if hasRef {
		p.Contributors = append(p.Contributors, cpRef)
		if obj, ok := env.FindRef(cpRef); ok {
			cp = obj
			contributors = append(contributors, obj)
			seen.all(obj)
		}
	}
	for _, m := range controlPlaneMachines(env, cluster, cpRef) {
		p.Contributors = append(p.Contributors, m.Ref())
		contributors = append(contributors, m)
		seen.all(m)
		if infra, ok := m.ContractRef("spec", "infrastructureRef"); ok {
			if obj, ok := env.FindRef(infra); ok {
				seen.all(obj)
			}
		}
	}
	p.LastTransition = seen.latest

	// A hosted control plane has no machines, so there is nothing to count: its
	// own Available condition is the whole answer (PLAN.md Phase 4).
	hosted := hostedControlPlaneKinds[cpRef.Kind]
	var ready, desired int64
	if !hosted {
		ready, desired = replicaCounts(cluster, cp, "controlPlane")
	}

	avail, hasAvail := cluster.Condition(clusterControlPlaneAvailable)
	switch {
	case hasAvail && avail.IsTrue() && (desired == 0 || ready >= desired):
		p.State = Done
		p.Detail = controlPlaneDetail(hosted, ready, desired, "ready")
	case hasAvail:
		p.State = Running
		p.Detail = controlPlaneDetail(hosted, ready, desired, detailFrom(cp, avail))
	case hasRef:
		p.State = Running
		p.Detail = controlPlaneDetail(hosted, ready, desired, "starting")
	}
	if p.State == Running {
		if init, ok := cluster.Condition(clusterControlPlaneInit); ok && !init.IsTrue() && ready == 0 {
			p.Detail = controlPlaneDetail(hosted, ready, desired, detailFrom(cp, init))
		}
	}
	return p
}

func controlPlaneDetail(hosted bool, ready, desired int64, fallback string) string {
	if hosted {
		return "hosted · " + fallback
	}
	if desired > 0 {
		return fmt.Sprintf("%d/%d nodes", ready, desired)
	}
	return fallback
}

// controlPlaneMachines are the Machines the control plane owns. CAPI labels them
// with the cluster name and an owner reference to the control-plane object.
func controlPlaneMachines(env snapshot.Envelope, cluster snapshot.Object, cpRef snapshot.Ref) []snapshot.Object {
	var out []snapshot.Object
	for _, m := range env.ByKind("Machine") {
		if m.Labels()["cluster.x-k8s.io/cluster-name"] != cluster.Name() {
			continue
		}
		for _, owner := range m.OwnerRefs() {
			if owner.Kind == cpRef.Kind && owner.Name == cpRef.Name {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// workerMachines are the Machines that are not owned by the control plane: pools
// own them through a MachineSet, so ownership is one hop away and the cluster
// label is the reliable filter.
func workerMachines(env snapshot.Envelope, cluster snapshot.Object) []snapshot.Object {
	cpRef, _ := cluster.ContractRef("spec", "controlPlaneRef")
	var out []snapshot.Object
	for _, m := range env.ByKind("Machine") {
		if m.Labels()["cluster.x-k8s.io/cluster-name"] != cluster.Name() {
			continue
		}
		owned := false
		for _, owner := range m.OwnerRefs() {
			if owner.Kind == cpRef.Kind && owner.Name == cpRef.Name {
				owned = true
			}
		}
		if !owned {
			out = append(out, m)
		}
	}
	return out
}

func foldWorkers(env snapshot.Envelope, cluster snapshot.Object) Phase {
	p := Phase{Name: Workers, State: Pending, Detail: "waiting"}
	contributors := []snapshot.Object{cluster}

	var seen clock
	seen.only(cluster, clusterWorkersAvailable)

	// The user's noun is a pool; the class decides whether that is a
	// MachineDeployment or a MachinePool, and this is the only place that shows.
	pools := env.ByKind("MachineDeployment", "MachinePool")
	var owned []snapshot.Object
	for _, pool := range pools {
		if pool.Labels()["cluster.x-k8s.io/cluster-name"] != cluster.Name() {
			continue
		}
		owned = append(owned, pool)
		p.Contributors = append(p.Contributors, pool.Ref())
		contributors = append(contributors, pool)
		seen.all(pool)
	}
	pools = owned
	for _, m := range workerMachines(env, cluster) {
		seen.all(m)
		if infra, ok := m.ContractRef("spec", "infrastructureRef"); ok {
			if obj, ok := env.FindRef(infra); ok {
				seen.all(obj)
			}
		}
	}
	p.LastTransition = seen.latest

	ready, desired := replicaCounts(cluster, snapshot.Object{}, "workers")
	if desired == 0 && len(p.Contributors) == 0 {
		// Nothing to wait for. Saying "no workers" beats an empty row.
		p.State, p.Detail = Done, "no workers"
		return p
	}
	if desired == 0 {
		for _, pool := range pools {
			r, d := poolCounts(pool)
			ready += r
			desired += d
		}
	}

	avail, hasAvail := cluster.Condition(clusterWorkersAvailable)
	switch {
	case desired > 0 && ready >= desired:
		p.State, p.Detail = Done, fmt.Sprintf("%d/%d nodes", ready, desired)
	case hasAvail && avail.IsTrue():
		p.State, p.Detail = Done, fmt.Sprintf("%d/%d nodes", ready, desired)
	case len(p.Contributors) > 0:
		p.State, p.Detail = Running, fmt.Sprintf("%d/%d nodes", ready, desired)
	}
	return p
}

// poolCounts reads a pool's own replica counters. MachinePool publishes no
// v1beta2 conditions in the pinned release (docs/api-snapshot.md, Notes), so
// counters are the only thing both pool kinds agree on.
func poolCounts(pool snapshot.Object) (ready, desired int64) {
	ready, _ = pool.Int("status", "readyReplicas")
	desired, ok := pool.Int("spec", "replicas")
	if !ok {
		desired, _ = pool.Int("status", "replicas")
	}
	return ready, desired
}

func foldAddons(env snapshot.Envelope, cluster snapshot.Object) Phase {
	p := Phase{Name: Addons, State: Done, Detail: "none"}
	var contributors []snapshot.Object
	for _, b := range env.ByKind("ClusterResourceSetBinding") {
		// The binding is named after the cluster and also names it in
		// spec.clusterName; it carries no label at all.
		if b.Name() != cluster.Name() &&
			b.String("spec", "clusterName") != cluster.Name() &&
			b.Labels()["cluster.x-k8s.io/cluster-name"] != cluster.Name() {
			continue
		}
		contributors = append(contributors, b)
		p.Contributors = append(p.Contributors, b.Ref())
	}
	if len(contributors) == 0 {
		return p
	}
	var seen clock
	seen.all(contributors...)
	p.LastTransition = seen.latest

	applied, total := 0, 0
	for _, b := range contributors {
		a, t := bindingCounts(b)
		applied += a
		total += t
	}
	switch {
	case total == 0:
		p.State, p.Detail = Running, "waiting"
	case applied >= total:
		p.State, p.Detail = Done, fmt.Sprintf("%d/%d applied", applied, total)
	default:
		p.State, p.Detail = Running, fmt.Sprintf("%d/%d applied", applied, total)
	}
	return p
}

func bindingCounts(b snapshot.Object) (applied, total int) {
	bindings, ok := b.Slice("spec", "bindings")
	if !ok {
		return 0, 0
	}
	for _, item := range bindings {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		resources, ok := snapshot.Object(m).Slice("resources")
		if !ok {
			continue
		}
		for _, r := range resources {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			total++
			if ok, _ := snapshot.Object(rm).Bool("applied"); ok {
				applied++
			}
		}
	}
	return applied, total
}

// replicaCounts prefers the Cluster's own rollup (status.controlPlane /
// status.workers), which CAPI computes across every provider, and falls back to
// the control-plane object's counters.
func replicaCounts(cluster, obj snapshot.Object, section string) (ready, desired int64) {
	ready, _ = cluster.Int("status", section, "readyReplicas")
	desired, _ = cluster.Int("status", section, "desiredReplicas")
	if desired > 0 {
		return ready, desired
	}
	if len(obj) == 0 {
		return ready, desired
	}
	r, _ := obj.Int("status", "readyReplicas")
	d, ok := obj.Int("spec", "replicas")
	if !ok {
		d, _ = obj.Int("status", "replicas")
	}
	if r > ready {
		ready = r
	}
	if d > desired {
		desired = d
	}
	return ready, desired
}

// detailFrom turns a False condition into a short human phrase. The raw condition
// type never reaches here: the reason is provider text, which is informative, and
// the type is jargon the renderer keeps for --verbose (D2.5).
func detailFrom(obj snapshot.Object, c snapshot.Condition) string {
	if c.Reason != "" {
		return humanise(c.Reason)
	}
	if len(obj) > 0 {
		if first, ok := firstFalse(obj); ok && first.Reason != "" {
			return humanise(first.Reason)
		}
	}
	return "waiting"
}

func firstFalse(obj snapshot.Object) (snapshot.Condition, bool) {
	cs := obj.Conditions()
	sort.SliceStable(cs, func(i, j int) bool {
		return cs[i].LastTransitionTime.After(cs[j].LastTransitionTime)
	})
	for _, c := range cs {
		if c.IsFalse() {
			return c, true
		}
	}
	return snapshot.Condition{}, false
}

// clock accumulates the transition times that belong to one phase. The Cluster
// carries conditions for all four phases at once, so taking its whole
// LastTransition would make every phase look like it changed whenever any of them
// did — and stall detection would never fire.
type clock struct{ latest time.Time }

// only adds the named conditions of an object.
func (c *clock) only(o snapshot.Object, types ...string) {
	for _, name := range types {
		if cond, ok := o.Condition(name); ok {
			c.at(cond.LastTransitionTime)
		}
	}
}

// all adds every condition of an object, for objects that exist solely for one
// phase.
func (c *clock) all(objs ...snapshot.Object) {
	for _, o := range objs {
		c.at(o.LastTransition())
	}
}

func (c *clock) at(t time.Time) {
	if t.After(c.latest) {
		c.latest = t
	}
}

// humanise turns a CamelCase provider reason into something readable:
// "WaitingForBootstrapData" becomes "waiting for bootstrap data". Provider reasons
// are the one piece of upstream vocabulary worth showing, because they say what is
// actually happening; the CamelCase is not.
func humanise(reason string) string {
	var words []string
	start := 0
	runes := []rune(reason)
	for i := 1; i < len(runes); i++ {
		if runes[i] >= 'A' && runes[i] <= 'Z' && (runes[i-1] < 'A' || runes[i-1] > 'Z') {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	words = append(words, string(runes[start:]))
	for i, w := range words {
		if i == 0 {
			words[i] = strings.ToLower(w)
			continue
		}
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, " ")
}
