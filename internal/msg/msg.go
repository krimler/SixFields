// Package msg holds every string this tool shows a user, as a typed entry in one
// registry. That is what makes the UX testable: the lints in msg_test.go walk the
// registry instead of grepping the codebase, and `cluster explain <code>` reads
// the same entries.
//
// Pure: text/template and embed only.
package msg

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"text/template"
)

// Code is stable across releases. It is what a user searches for and what
// `cluster explain` takes.
type Code string

// Stall classes. One per way a cluster can stop making progress; `internal/why`
// maps a ranked candidate to exactly one of these.
const (
	InfraNotReady       Code = "CAPI-INFRA-001"
	ControlPlaneNotInit Code = "CAPI-CP-001"
	ControlPlaneMachine Code = "CAPI-CP-002"
	EtcdNotHealthy      Code = "CAPI-CP-003"
	WorkersNotReady     Code = "CAPI-WRK-001"
	NodeNotJoining      Code = "CAPI-WRK-002"
	TopologyFailed      Code = "CAPI-TOPO-001"
	AddonsNotApplied    Code = "CAPI-ADDON-001"
	VersionUnavailable  Code = "CAPI-VERSION-001"
)

// Admission denials. The policy in policy/vap/ and `cluster plan` render the same
// text from these entries; TestUX_PlanMatchesAdmission asserts they are identical.
const (
	FieldManaged      Code = "CAPI-ADM-001"
	KindManaged       Code = "CAPI-ADM-002"
	VariableUnknown   Code = "CAPI-ADM-003"
	BreakGlassPartial Code = "CAPI-ADM-004"
	VersionMalformed  Code = "CAPI-ADM-005"
)

// Environment problems, the ones `cluster doctor` reports.
const (
	NoRuntime       Code = "CAPI-ENV-001"
	ClusterNotFound Code = "CAPI-ENV-002"
)

// Class groups codes for the exit-code contract and for the runbook lint.
type Class string

const (
	Stall       Class = "stall"
	Denial      Class = "denial"
	Environment Class = "environment"
)

// Entry is one message. Summary is a one-line template over Vars; when it is empty
// the class default is used, which is why the nine stall entries do not repeat one
// string nine times. NextAction is required and is what makes every message end
// with something the user can do.
type Entry struct {
	Code    Code
	Class   Class
	Title   string
	Summary string
	// NextAction is a command, a doc path, or a field to change. Never empty:
	// TestUX_EveryEntryHasNextAction fails on an empty one, and New() below is the
	// only way to build an Error, so the compiler carries the same rule into code.
	NextAction string
}

// Vars is everything a Summary template may reference. One struct for every
// message keeps the templates checkable at test time.
type Vars struct {
	// Title is filled in by Render from the entry; callers leave it empty.
	Title     string
	Object    string // Kind/name
	Field     string // spec.topology.controlPlane.replicas
	Kind      string // KubeadmControlPlane
	Class     string // std
	Variable  string // an unknown ClusterClass variable name
	Reason    string // condition reason
	Message   string // condition message, already truncated
	Since     string // 3m12s
	Version   string
	Namespace string
	Ready     int
	Desired   int
}

var registry = map[Code]Entry{
	InfraNotReady: {
		Code: InfraNotReady, Class: Stall,
		Title:      "infrastructure is not ready",
		NextAction: "cluster docs CAPI-INFRA-001",
	},
	ControlPlaneNotInit: {
		Code: ControlPlaneNotInit, Class: Stall,
		Title:      "control plane is not initializing",
		NextAction: "cluster docs CAPI-CP-001",
	},
	ControlPlaneMachine: {
		Code: ControlPlaneMachine, Class: Stall,
		Title:      "a control-plane machine is stuck provisioning",
		NextAction: "cluster docs CAPI-CP-002",
	},
	EtcdNotHealthy: {
		Code: EtcdNotHealthy, Class: Stall,
		Title:      "etcd is not coming up",
		NextAction: "cluster docs CAPI-CP-003",
	},
	WorkersNotReady: {
		Code: WorkersNotReady, Class: Stall,
		Title:      "worker machines are not becoming ready",
		NextAction: "cluster docs CAPI-WRK-001",
	},
	NodeNotJoining: {
		Code: NodeNotJoining, Class: Stall,
		Title:      "a node is not joining the cluster",
		NextAction: "cluster docs CAPI-WRK-002",
	},
	TopologyFailed: {
		Code: TopologyFailed, Class: Stall,
		Title:      "the topology could not be reconciled",
		NextAction: "cluster docs CAPI-TOPO-001",
	},
	AddonsNotApplied: {
		Code: AddonsNotApplied, Class: Stall,
		Title:      "add-ons were not applied",
		NextAction: "cluster docs CAPI-ADDON-001",
	},
	VersionUnavailable: {
		Code: VersionUnavailable, Class: Stall,
		Title:      "the requested Kubernetes version is not available",
		NextAction: "set spec.topology.version to a version the provider publishes",
	},

	FieldManaged: {
		Code: FieldManaged, Class: Denial,
		Title:      "the field is managed by the class",
		NextAction: "docs/eject.md",
	},
	KindManaged: {
		Code: KindManaged, Class: Denial,
		Title: "the kind is managed by the class",
		// No class name: a managed object carries the cluster's name, never its
		// class, so the policy cannot know which of the assembly's classes made it.
		// Naming one told a std-inmemory user their object belonged to std.
		Summary:    "{{.Kind}} is managed by the ClusterClass that created it. Set it via the Cluster or use break-glass (docs/eject.md).",
		NextAction: "docs/eject.md",
	},
	VariableUnknown: {
		Code: VariableUnknown, Class: Denial,
		Title:      "the variable is not one the class exposes",
		Summary:    "spec.topology.variables[{{.Variable}}] is managed by ClusterClass '{{.Class}}'. Set it via the class or use break-glass (docs/eject.md).",
		NextAction: "docs/eject.md",
	},
	BreakGlassPartial: {
		Code: BreakGlassPartial, Class: Denial,
		Title:      "break-glass needs both the label and the group",
		Summary:    "{{.Field}} needs both the sixfields.io/break-glass label and membership of group sixfields:break-glass. Use break-glass (docs/eject.md).",
		NextAction: "docs/eject.md",
	},

	VersionMalformed: {
		Code: VersionMalformed, Class: Denial,
		Title:      "the version is not a Kubernetes version",
		Summary:    "spec.topology.version is '{{.Version}}', which is not a Kubernetes version. Set it to something like v1.34.11, or use break-glass (docs/eject.md).",
		NextAction: "set spec.topology.version to a version the provider publishes, e.g. v1.34.11",
	},

	NoRuntime: {
		Code: NoRuntime, Class: Environment,
		Title:      "no container runtime is running",
		Summary:    "no container runtime is answering.",
		NextAction: "start Docker Desktop, OrbStack or Colima, then run: make doctor",
	},
	ClusterNotFound: {
		Code: ClusterNotFound, Class: Environment,
		Title:      "no such cluster",
		Summary:    "no Cluster {{.Object}} in namespace {{.Namespace}}.",
		NextAction: "kubectl get clusters -A",
	},
}

// defaultSummary is the shape of a message when an entry does not override it.
//
// The stall line is deliberately free of raw condition type names: it names the
// object, says in plain language what is wrong, and gives an elapsed time. The
// upstream condition type, reason and message are one rung down, under --verbose
// and on the raw: line (D2.5, the jargon lint).
var defaultSummary = map[Class]string{
	Stall:       "{{.Object}}: {{.Title}} ({{.Since}})",
	Denial:      "{{.Field}} is managed by ClusterClass '{{.Class}}'. Set it via the class or use break-glass (docs/eject.md).",
	Environment: "{{.Title}}.",
}

//go:embed longform/*.md
var longform embed.FS

// Get returns the entry for a code. Unknown codes are a programming error: the
// registry is the only source of codes.
func Get(code Code) (Entry, bool) {
	e, ok := registry[code]
	return e, ok
}

// Codes returns every code, sorted, for the lints and for `cluster explain` with
// no argument.
func Codes() []Code {
	out := make([]Code, 0, len(registry))
	for c := range registry {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Render fills an entry's summary. A template that cannot execute is a bug in this
// package, so it panics rather than returning a half-rendered string to a user.
func Render(code Code, v Vars) string {
	e, ok := registry[code]
	if !ok {
		panic("msg: unknown code " + string(code))
	}
	text := e.Summary
	if text == "" {
		text = defaultSummary[e.Class]
	}
	v.Title = e.Title
	t, err := template.New(string(code)).Option("missingkey=error").Parse(text)
	if err != nil {
		panic(fmt.Sprintf("msg: %s: %v", code, err))
	}
	var b strings.Builder
	if err := t.Execute(&b, v); err != nil {
		panic(fmt.Sprintf("msg: %s: %v", code, err))
	}
	return b.String()
}

// Longform is the text `cluster explain <code>` prints: what happened, why, and
// what to do. Written at build time with the local model, human-reviewed, shipped
// as static text, so no key is needed at runtime (D5.3).
func Longform(code Code) (string, bool) {
	b, err := longform.ReadFile("longform/" + string(code) + ".md")
	if err != nil {
		return "", false
	}
	return string(b), true
}

// Truncate shortens an upstream message to a budget at a word boundary. Long
// provider messages are the main reason stall lines blow their length contract.
func Truncate(s string, budget int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= budget || budget < 2 {
		return s
	}
	cut := s[:budget-1]
	if i := strings.LastIndex(cut, " "); i > budget/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "\u2026"
}

// LongformCodes lists the codes that have a long form, for the lint that requires
// one per code.
func LongformCodes() []Code {
	entries, err := fs.ReadDir(longform, "longform")
	if err != nil {
		return nil
	}
	out := make([]Code, 0, len(entries))
	for _, e := range entries {
		out = append(out, Code(strings.TrimSuffix(e.Name(), ".md")))
	}
	return out
}

//go:embed runbooks/*.md
var runbooks embed.FS

// Runbook is the operator's playbook for a stall class: what to check, in order.
// It is embedded so `cluster docs` works with no network and no repo checkout.
func Runbook(code Code) (string, bool) {
	b, err := runbooks.ReadFile("runbooks/" + string(code) + ".md")
	if err != nil {
		return "", false
	}
	return string(b), true
}
