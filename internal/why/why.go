// Package why answers one question: of everything that is wrong, which single
// object should the user look at? It is the deterministic analyzer that every AI
// path is built on — the model only ever explains what this package decided.
//
// Pure: it takes an envelope and a fold result and returns values.
package why

import (
	"sort"
	"strings"
	"time"

	"sixfields/internal/fold"
	"sixfields/internal/msg"
	"sixfields/internal/snapshot"
)

// negativePolarity conditions report an activity, not a fault: they are True while
// the object is doing the thing and False the rest of the time
// (api@v1.14.2 core/v1beta2/cluster_types.go:532, ConditionPolarity). Treating
// their False as a failure is how a first live run ranked "Paused=False" — "this
// object is not paused" — above the node that had genuinely not come up.
//
// They are excluded from the candidate set entirely: True means work in progress,
// which the phase detail already shows, and False means nothing at all.
var negativePolarity = map[string]bool{
	"Paused":      true,
	"Deleting":    true,
	"Remediating": true,
	"RollingOut":  true,
	"ScalingUp":   true,
	"ScalingDown": true,
	"Updating":    true,
}

// waitsFor reports whether this candidate is waiting on another candidate that is
// also failing. Providers say so in the reason: the in-memory backend reports
// APIServerProvisioned=False with reason WaitingForVMProvisioned while
// VMProvisioned is itself False. The one being waited on is the cause and the
// other three are its shadow, so the cause is what gets named.
func waitsFor(c Candidate, all []Candidate) bool {
	const prefix = "WaitingFor"
	if !strings.HasPrefix(c.Condition.Reason, prefix) {
		return false
	}
	named := strings.TrimPrefix(c.Condition.Reason, prefix)
	for _, other := range all {
		if other.Object == c.Object && other.Condition.Type == named {
			return true
		}
	}
	return false
}

// summaryConditions aggregate other conditions on the same object. They are true
// less often and say less: "Ready=False reason=NotReady" restates what a specific
// condition already reported. They still rank, but below anything specific on the
// same object, so the line a user reads names the actual fault.
var summaryConditions = map[string]bool{
	"Ready":            true,
	"Available":        true,
	"MachinesReady":    true,
	"MachinesUpToDate": true,
}

// Condition types the ranker classifies by. Every name appears in
// docs/api-snapshot.json; TestWhy_OnlyUsesSnapshotConditions fails if one does not.
var (
	// etcd is its own stall class because it is the one component whose failure
	// looks like "nothing is happening" from every other object.
	etcdConditions = map[string]bool{
		"EtcdClusterHealthy": true, // KubeadmControlPlane
		"EtcdMemberHealthy":  true, // KubeadmControlPlane, per machine
		"EtcdProvisioned":    true, // DevMachine, in-memory backend
	}
	// A node that never registers is a different problem from a machine that
	// never provisioned, and it has a different runbook.
	nodeConditions = map[string]bool{
		"NodeHealthy":     true, // Machine
		"NodeReady":       true, // Machine
		"NodeProvisioned": true, // DevMachine, in-memory backend
	}
)

// MessageBudget is how much of an upstream condition message fits on one line.
// The rest is available under --verbose.
const MessageBudget = 72

// Candidate is one False condition on one object, with the reasons it did or did
// not win. Losers are kept so `cluster why --explain-ranking` can show the
// ranking without anyone reading this file.
type Candidate struct {
	Object      snapshot.Ref       `json:"object"`
	Condition   snapshot.Condition `json:"condition"`
	Specificity int                `json:"specificity"`
	Since       time.Duration      `json:"since_ns"`
	// LostTo is empty on the winner and otherwise says which rule decided it.
	LostTo string `json:"lost_to,omitempty"`
}

// Stall is the one line the user reads, plus everything needed to go a rung
// deeper: the full message, the raw kubectl command, and the ranked candidates.
type Stall struct {
	Object      snapshot.Ref   `json:"object"`
	Code        msg.Code       `json:"code"`
	Phase       fold.PhaseName `json:"phase"`
	Reason      string         `json:"reason"`
	Message     string         `json:"message"`
	FullMessage string         `json:"full_message,omitempty"`
	Since       time.Duration  `json:"since_ns"`
	Raw         string         `json:"raw"`
	Candidates  []Candidate    `json:"candidates,omitempty"`
	// ConditionType is the raw CAPI condition type. It is jargon, so it belongs on
	// the raw line and under --verbose only (D2.5).
	ConditionType string `json:"condition_type"`
}

// Line is the stall line: kind/name, what is wrong in plain language, and how long
// it has been that way. Under 120 characters by construction.
func (s Stall) Line() string {
	return msg.Render(s.Code, msg.Vars{
		Object: s.Object.String(),
		Since:  short(s.Since),
	})
}

// specificity ranks kinds from the leaf outwards. The most specific object that is
// failing is the one worth naming: "DevMachine/dev-1-cp-abcde" tells a user where
// to look, "Cluster/dev-1" does not.
func specificity(kind string) int {
	switch kind {
	case "DevMachine", "DockerMachine", "KubeadmConfig", "K0sWorkerConfig":
		return 45
	case "Machine":
		return 40
	case "MachineSet":
		return 30
	case "MachineDeployment", "MachinePool", "DevMachinePool":
		return 25
	case "KubeadmControlPlane", "K0sControlPlane", "K0smotronControlPlane":
		return 20
	case "DevCluster", "DockerCluster", "ClusterResourceSetBinding":
		return 15
	case "Cluster":
		return 10
	default:
		return 5
	}
}

type Options struct {
	Now time.Time
}

// Rank returns the object to name, or false when nothing is failing.
func Rank(env snapshot.Envelope, res fold.Result, opt Options) (Stall, bool) {
	phase, ok := res.Stalled()
	if !ok {
		// Not stalled, but `cluster why` still answers on a running cluster: the
		// frontier phase is what it is waiting for.
		phase, ok = firstNotDone(res)
		if !ok {
			return Stall{}, false
		}
	}

	candidates := collect(env, phase, opt.Now)
	if len(candidates) == 0 {
		return Stall{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Specificity != b.Specificity {
			return a.Specificity > b.Specificity
		}
		if summaryConditions[a.Condition.Type] != summaryConditions[b.Condition.Type] {
			return !summaryConditions[a.Condition.Type]
		}
		if aWaits, bWaits := waitsFor(a, candidates), waitsFor(b, candidates); aWaits != bWaits {
			return !aWaits
		}
		if !a.Condition.LastTransitionTime.Equal(b.Condition.LastTransitionTime) {
			return a.Condition.LastTransitionTime.After(b.Condition.LastTransitionTime)
		}
		if severityRank(a.Condition.Severity) != severityRank(b.Condition.Severity) {
			return severityRank(a.Condition.Severity) > severityRank(b.Condition.Severity)
		}
		return a.Object.Name < b.Object.Name
	})
	explainLosses(candidates)

	win := candidates[0]
	full := strings.Join(strings.Fields(win.Condition.Message), " ")
	stall := Stall{
		Object:        win.Object,
		Code:          classify(res, phase, win),
		Phase:         phase.Name,
		Reason:        win.Condition.Reason,
		Message:       msg.Truncate(full, MessageBudget),
		Since:         win.Since,
		Raw:           win.Object.Kubectl(),
		Candidates:    candidates,
		ConditionType: win.Condition.Type,
	}
	if stall.Message != full {
		stall.FullMessage = full
	}
	return stall, true
}

func firstNotDone(res fold.Result) (fold.Phase, bool) {
	for _, p := range res.Phases {
		if p.State != fold.Done {
			return p, true
		}
	}
	return fold.Phase{}, false
}

// collect gathers every False condition on every object contributing to the phase,
// plus the objects those contributors point at — the truth about a Machine is
// usually on its DevMachine.
func collect(env snapshot.Envelope, phase fold.Phase, now time.Time) []Candidate {
	seen := map[string]bool{}
	var out []Candidate

	add := func(o snapshot.Object) {
		ref := o.Ref()
		key := ref.Kind + "/" + ref.Name
		if seen[key] {
			return
		}
		seen[key] = true
		for _, c := range o.Conditions() {
			if !c.IsFalse() || negativePolarity[c.Type] {
				continue
			}
			since := time.Duration(0)
			if !c.LastTransitionTime.IsZero() && !now.IsZero() {
				since = now.Sub(c.LastTransitionTime)
			}
			out = append(out, Candidate{
				Object: ref, Condition: c, Specificity: specificity(ref.Kind), Since: since,
			})
		}
	}

	for _, ref := range phase.Contributors {
		obj, ok := env.FindRef(ref)
		if !ok {
			continue
		}
		add(obj)
		for _, field := range [][]string{{"spec", "infrastructureRef"}, {"spec", "bootstrapRef"}} {
			if r, ok := obj.ContractRef(field...); ok {
				if child, ok := env.FindRef(r); ok {
					add(child)
				}
			}
		}
	}
	// The Cluster is a contributor to every phase but is never listed as one, so
	// that a fold contributor list stays about the phase. Its own conditions still
	// matter when nothing more specific is failing.
	if cluster, ok := env.Cluster(); ok {
		add(cluster)
	}
	return out
}

func explainLosses(candidates []Candidate) {
	if len(candidates) == 0 {
		return
	}
	win := candidates[0]
	for i := range candidates[1:] {
		c := &candidates[i+1]
		switch {
		case c.Specificity < win.Specificity:
			c.LostTo = "less specific object"
		case summaryConditions[c.Condition.Type] && !summaryConditions[win.Condition.Type]:
			c.LostTo = "summary condition"
		case waitsFor(*c, candidates) && !waitsFor(win, candidates):
			c.LostTo = "waiting on " + strings.TrimPrefix(c.Condition.Reason, "WaitingFor")
		case c.Condition.LastTransitionTime.Before(win.Condition.LastTransitionTime):
			c.LostTo = "older transition"
		case severityRank(c.Condition.Severity) < severityRank(win.Condition.Severity):
			c.LostTo = "lower severity"
		default:
			c.LostTo = "later in name order"
		}
	}
}

func severityRank(s string) int {
	switch s {
	case "Error":
		return 3
	case "Warning":
		return 2
	case "Info":
		return 1
	default:
		return 0
	}
}

// classify maps the winning candidate to one stall class. Every branch here has a
// runbook in docs/runbooks and a long form in internal/msg/longform.
func classify(res fold.Result, phase fold.Phase, win Candidate) msg.Code {
	cond := win.Condition
	if win.Object.Kind == "Cluster" && cond.Type == "TopologyReconciled" {
		return msg.TopologyFailed
	}
	if isVersionFailure(res, cond) {
		return msg.VersionUnavailable
	}
	// A node that never became ready has one runbook whether it is a control-plane
	// node or a worker, so this is decided before the phase.
	if nodeConditions[cond.Type] {
		return msg.NodeNotJoining
	}
	switch phase.Name {
	case fold.Infrastructure:
		return msg.InfraNotReady
	case fold.ControlPlane:
		switch {
		case etcdConditions[cond.Type]:
			return msg.EtcdNotHealthy
		case isMachineKind(win.Object.Kind):
			return msg.ControlPlaneMachine
		default:
			return msg.ControlPlaneNotInit
		}
	case fold.Workers:
		return msg.WorkersNotReady
	case fold.Addons:
		return msg.AddonsNotApplied
	}
	return msg.InfraNotReady
}

func isMachineKind(kind string) bool {
	switch kind {
	case "Machine", "DevMachine", "DockerMachine", "KubeadmConfig", "K0sWorkerConfig":
		return true
	}
	return false
}

// isVersionFailure is deliberately narrow: an image failure that names the version
// the user asked for is a version problem, and an image failure that does not is a
// registry problem. Guessing either way would send the user to the wrong runbook.
func isVersionFailure(res fold.Result, c snapshot.Condition) bool {
	if res.Version == "" || !strings.Contains(c.Message, res.Version) {
		return false
	}
	return strings.Contains(strings.ToLower(c.Reason), "image") ||
		strings.Contains(strings.ToLower(c.Message), "image")
}

// short formats a duration the way a person says it: 3m12s, 45s, 1h4m.
func short(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return d.String()
	case d < time.Hour:
		return d.Truncate(time.Second).String()
	default:
		return d.Truncate(time.Minute).String()
	}
}
