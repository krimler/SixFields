// Package gen turns the six fields a user may write into a Cluster. It is the one
// place that knows the user-facing surface, so `cluster new`, `cluster plan` and
// the JSON schema in docs/schema/cluster-spec.v1.json all agree by construction.
//
// Pure: no Kubernetes client. Validation here is a local copy of the admission
// policy's rules, and TestUX_PlanMatchesAdmission asserts the messages are the
// same text, so an error a user hits at admission is one they could have hit
// before submitting.
package gen

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"sixfields/internal/msg"
)

// APIVersion is the Cluster API version this generator writes. It matches the
// pinned release in versions.env; docs/api-snapshot.md records the contract.
const APIVersion = "cluster.x-k8s.io/v1beta2"

// DefaultClass is the only class an ordinary user names.
const DefaultClass = "std"

// ManagedKinds are the kinds the ClusterClass owns. A user writes exactly one
// kind, Cluster; everything here is derived from it and is refused before submit
// as well as at admission. The list is the same one the policy carries, and
// TestPolicy_ManagedKindListMatchesTheGenerator fails if the two drift.
//
// Every kind whose name ends in Template is managed too; that rule is in
// IsManagedKind rather than here, because it covers templates from providers
// this list has never heard of.
var ManagedKinds = []string{
	"KubeadmControlPlane", "K0sControlPlane", "K0smotronControlPlane",
	"MachineDeployment", "MachineSet", "Machine", "MachinePool",
	"DockerCluster", "DevCluster",
	"DockerMachine", "DevMachine",
	"DockerMachinePool", "DevMachinePool",
	"KubeadmConfig", "K0sWorkerConfig", "K0sControllerConfig",
	"KubeadmConfigTemplate",
}

// IsManagedKind reports whether a kind belongs to the class rather than to the
// user.
func IsManagedKind(kind string) bool {
	if strings.HasSuffix(kind, "Template") {
		return true
	}
	for _, m := range ManagedKinds {
		if m == kind {
			return true
		}
	}
	return false
}

// HostedClassSuffix is how placement resolves to a class. A ClusterClass has
// exactly one controlPlane (api@v1.14.2 core/v1beta2/clusterclass_types.go), so a
// hosted control plane cannot be a patch on the same class; it is a second class,
// and this is the only place that knows it.
const HostedClassSuffix = "-hosted"

// ClassFor returns the class a placement resolves to. The user writes
// placement: hosted and never sees this.
func ClassFor(class, placement string) string {
	if placement == "hosted" && !strings.HasSuffix(class, HostedClassSuffix) {
		return class + HostedClassSuffix
	}
	return class
}

// Backend is which worker kind the class puts behind a pool. Users never set it:
// the overlay does, because it depends on whether the provider has a native
// scaling group.
type Backend string

const (
	// MachineDeployments is the default: GA, and it works on bare metal, vSphere
	// and Docker, none of which have a scaling group.
	MachineDeployments Backend = "machineDeployments"
	// MachinePools is used only where the provider has an ASG/VMSS/MIG.
	MachinePools Backend = "machinePools"
)

// Pool is the user's only worker noun.
type Pool struct {
	Name     string `json:"name"`
	Class    string `json:"class"`
	Replicas int64  `json:"replicas"`
}

// Spec is the whole user-facing surface. Anything not here belongs to the class.
type Spec struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Class       string            `json:"class,omitempty"`
	Version     string            `json:"version"`
	Size        string            `json:"size,omitempty"`
	Placement   string            `json:"placement,omitempty"`
	Pools       []Pool            `json:"pools,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	// Backend is set by the overlay, not by a user.
	Backend Backend `json:"-"`
}

// ControlPlaneReplicas is what a size means. One value, used by the generator, by
// the admission policy's consistency check, and by the class docs.
func ControlPlaneReplicas(size string) int64 {
	if size == "ha" {
		return 3
	}
	return 1
}

// AllowedVariables is the allow-list the admission policy enforces. It lives here
// so the CLI and the policy cannot drift apart silently.
var AllowedVariables = []string{"size", "placement"}

// kubernetesVersion is deliberately loose: it rejects an empty value, an
// unsubstituted ${PLACEHOLDER} and a version without its v, and leaves judging
// whether the version exists to the provider, which is the only thing that knows.
// kubernetesVersion is applied to the version the API server will store, not to
// the one the user typed. Cluster API's mutating webhook prepends a missing `v`
// before any validation runs, with the comment "Tolerate version strings without
// a v prefix" (cluster-api@v1.14.2 core/webhooks/admission/cluster.go:92), so
// refusing `1.34.11` here would refuse a manifest the cluster accepts and
// corrects. Prefixing before matching is how the client stays the same answer as
// admission, which is the only claim `cluster plan` makes.
var kubernetesVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+`)

// asStored is the version after the webhook has had it.
func asStored(version string) string {
	if version != "" && !strings.HasPrefix(version, "v") {
		return "v" + version
	}
	return version
}

var (
	allowedSizes      = []string{"dev", "ha"}
	allowedPlacements = []string{"self", "hosted"}
)

// Defaults fills in what a user left out. Every default is one PLAN.md names.
func (s Spec) Defaults() Spec {
	if s.Namespace == "" {
		s.Namespace = "default"
	}
	if s.Class == "" {
		s.Class = DefaultClass
	}
	if s.Size == "" {
		s.Size = "dev"
	}
	if s.Placement == "" {
		s.Placement = "self"
	}
	if s.Backend == "" {
		s.Backend = MachineDeployments
	}
	for i := range s.Pools {
		if s.Pools[i].Class == "" {
			s.Pools[i].Class = "default"
		}
	}
	return s
}

// Validate returns every problem it finds: a user fixing a cluster file
// should see all of it in one pass.
func (s Spec) Validate() []*msg.Error {
	s = s.Defaults()
	var errs []*msg.Error
	add := func(code msg.Code, v msg.Vars) {
		v.Class = s.Class
		errs = append(errs, msg.New(code, v))
	}

	if s.Name == "" {
		add(msg.FieldManaged, msg.Vars{Field: "metadata.name"})
	}
	if !kubernetesVersion.MatchString(asStored(s.Version)) {
		add(msg.VersionMalformed, msg.Vars{Field: "spec.topology.version", Version: s.Version})
	}
	if !contains(allowedSizes, s.Size) {
		add(msg.VariableUnknown, msg.Vars{Variable: "size"})
	}
	if !contains(allowedPlacements, s.Placement) {
		add(msg.VariableUnknown, msg.Vars{Variable: "placement"})
	}
	names := map[string]bool{}
	for _, p := range s.Pools {
		switch {
		case p.Name == "":
			add(msg.FieldManaged, msg.Vars{Field: "spec.topology.workers." + string(s.Backend) + "[].name"})
		case names[p.Name]:
			add(msg.FieldManaged, msg.Vars{Field: "spec.topology.workers." + string(s.Backend) + "[" + p.Name + "].name"})
		}
		names[p.Name] = true
		if p.Replicas < 0 {
			add(msg.FieldManaged, msg.Vars{Field: "spec.topology.workers." + string(s.Backend) + "[" + p.Name + "].replicas"})
		}
	}
	return errs
}

// Cluster returns the object a user would have written by hand. The map is
// ordered by the YAML marshaller, so the output is stable.
func (s Spec) Cluster() map[string]any {
	s = s.Defaults()

	metadata := map[string]any{"name": s.Name, "namespace": s.Namespace}
	if len(s.Labels) > 0 {
		metadata["labels"] = toAny(s.Labels)
	}
	if len(s.Annotations) > 0 {
		metadata["annotations"] = toAny(s.Annotations)
	}

	topology := map[string]any{
		// v1beta2 spells the class reference spec.topology.classRef.{name,namespace}
		// (api@v1.14.2 core/v1beta2/cluster_types.go, Topology.ClassRef).
		"classRef": map[string]any{"name": ClassFor(s.Class, s.Placement)},
		"version":  s.Version,
		"variables": []any{
			map[string]any{"name": "size", "value": s.Size},
			map[string]any{"name": "placement", "value": s.Placement},
		},
		// size is expanded here rather than by a class patch: a
		// KubeadmControlPlaneTemplate has no replicas field, and the topology patch
		// engine preserves spec.replicas on the control-plane object
		// (core/reconcilers/topology/cluster/patches/engine.go, PreserveFields), so a
		// patch would be dropped without an error. The user still never writes it.
		"controlPlane": map[string]any{"replicas": ControlPlaneReplicas(s.Size)},
	}
	if len(s.Pools) > 0 {
		pools := make([]any, 0, len(s.Pools))
		for _, p := range s.Pools {
			pools = append(pools, map[string]any{
				"name": p.Name, "class": p.Class, "replicas": p.Replicas,
			})
		}
		topology["workers"] = map[string]any{string(s.Backend): pools}
	}

	return map[string]any{
		"apiVersion": APIVersion,
		"kind":       "Cluster",
		"metadata":   metadata,
		"spec":       map[string]any{"topology": topology},
	}
}

// YAML is what `cluster new` prints and what a user commits.
func (s Spec) YAML() ([]byte, error) {
	b, err := yaml.Marshal(s.Cluster())
	if err != nil {
		return nil, fmt.Errorf("render cluster: %w", err)
	}
	return b, nil
}

// FromYAML reads a Cluster back into the user-facing spec, rejecting anything the
// user is not allowed to write. This is what `cluster plan -f cluster.yaml` runs,
// and it is where an admission denial is moved to before-submit.
func FromYAML(b []byte) (Spec, []*msg.Error) {
	var obj map[string]any
	if err := yaml.Unmarshal(b, &obj); err != nil {
		return Spec{}, []*msg.Error{msg.Wrap(msg.FieldManaged, msg.Vars{Field: "the file", Class: DefaultClass}, err)}
	}
	return FromObject(obj)
}

// FromObject is FromYAML for an already-decoded object.
func FromObject(obj map[string]any) (Spec, []*msg.Error) {
	var errs []*msg.Error
	spec := Spec{}

	metadata, _ := obj["metadata"].(map[string]any)
	spec.Name, _ = metadata["name"].(string)
	spec.Namespace, _ = metadata["namespace"].(string)
	spec.Labels = fromAny(metadata["labels"])
	spec.Annotations = fromAny(metadata["annotations"])

	// A managed kind is not a Cluster with strange fields in it. Reading one
	// field by field produced denials naming fields that are legitimate on that
	// kind, and a class the object does not carry; the server-side policy denies
	// the whole kind and names none. Same answer, same words.
	if kind, _ := obj["kind"].(string); kind != "" && kind != "Cluster" {
		if IsManagedKind(kind) {
			return Spec{}, []*msg.Error{msg.New(msg.KindManaged, msg.Vars{Kind: kind})}
		}
	}

	root, _ := obj["spec"].(map[string]any)
	topology, _ := root["topology"].(map[string]any)
	// The class is read before any denial is written, because every denial names
	// it. Reading it afterwards made each field beside topology say 'std'
	// whatever the file said, while the policy interpolated the real name.
	if classRef, ok := topology["classRef"].(map[string]any); ok {
		spec.Class, _ = classRef["name"].(string)
		// placement is expanded into the class name on the way out, so it is
		// folded back on the way in and the spec round-trips.
		spec.Class = strings.TrimSuffix(spec.Class, HostedClassSuffix)
	}
	class := DefaultClass
	if spec.Class != "" {
		class = spec.Class
	}

	for field := range root {
		if field != "topology" {
			errs = append(errs, msg.New(msg.FieldManaged, msg.Vars{Field: "spec." + field, Class: class}))
		}
	}
	if classRef, ok := topology["classRef"].(map[string]any); ok {
		for field := range classRef {
			if field != "name" && field != "namespace" {
				errs = append(errs, msg.New(msg.FieldManaged, msg.Vars{Field: "spec.topology.classRef." + field, Class: class}))
			}
		}
	}
	spec.Version, _ = topology["version"].(string)

	for field := range topology {
		switch field {
		case "classRef", "version", "variables", "workers", "controlPlane":
		default:
			errs = append(errs, msg.New(msg.FieldManaged, msg.Vars{Field: "spec.topology." + field, Class: class}))
		}
	}

	for _, item := range sliceOf(topology["variables"]) {
		v, _ := item.(map[string]any)
		name, _ := v["name"].(string)
		value, _ := v["value"].(string)
		switch name {
		case "size":
			spec.Size = value
		case "placement":
			spec.Placement = value
		default:
			errs = append(errs, msg.New(msg.VariableUnknown, msg.Vars{Variable: name, Class: class}))
		}
	}

	if cp, ok := topology["controlPlane"].(map[string]any); ok {
		for field := range cp {
			if field != "replicas" {
				errs = append(errs, msg.New(msg.FieldManaged, msg.Vars{Field: "spec.topology.controlPlane." + field, Class: class}))
			}
		}
		if replicas, present := cp["replicas"]; present && intOf(replicas) != ControlPlaneReplicas(spec.Size) {
			errs = append(errs, msg.New(msg.FieldManaged, msg.Vars{Field: "spec.topology.controlPlane.replicas", Class: class}))
		}
	}

	workers, _ := topology["workers"].(map[string]any)
	for field := range workers {
		if field != string(MachineDeployments) && field != string(MachinePools) {
			errs = append(errs, msg.New(msg.FieldManaged, msg.Vars{Field: "spec.topology.workers." + field, Class: class}))
		}
	}
	for _, backend := range []Backend{MachineDeployments, MachinePools} {
		items := sliceOf(workers[string(backend)])
		if len(items) == 0 {
			continue
		}
		spec.Backend = backend
		for _, item := range items {
			p, _ := item.(map[string]any)
			pool := Pool{}
			pool.Name, _ = p["name"].(string)
			pool.Class, _ = p["class"].(string)
			pool.Replicas = intOf(p["replicas"])
			for field := range p {
				switch field {
				case "name", "class", "replicas":
				default:
					errs = append(errs, msg.New(msg.FieldManaged, msg.Vars{
						Field: fmt.Sprintf("spec.topology.workers.%s[%s].%s", backend, pool.Name, field), Class: class,
					}))
				}
			}
			spec.Pools = append(spec.Pools, pool)
		}
	}

	errs = append(errs, spec.Validate()...)
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Summary < errs[j].Summary })
	return spec.Defaults(), errs
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func sliceOf(v any) []any {
	s, _ := v.([]any)
	return s
}

func intOf(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}

func toAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func fromAny(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, vv := range m {
		if s, ok := vv.(string); ok {
			out[k] = s
		}
	}
	return out
}
