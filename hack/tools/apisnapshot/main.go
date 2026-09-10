// Command apisnapshot writes docs/api-snapshot.{md,json} from the API packages of
// the pinned releases in versions.env. Nothing in this repo may name a CAPI
// condition, reason or phase that is not in the JSON it produces.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type source struct {
	Module   string
	Version  string // key in versions.env
	Packages []pkg
	// Providers this module ships, for the contract table. MetadataPath is
	// relative to the module root, empty when the module ships no metadata.yaml.
	Providers    []string
	MetadataPath string
}

type pkg struct {
	// Dir is relative to the module root.
	Dir string
	// Group is the API group the kinds in this package belong to.
	Group string
	// Kinds maps a source file basename to the kind its constants describe.
	Kinds map[string]string
}

// The packages we read. Adding a provider means adding an entry here and
// re-running `make api-snapshot`; there is no other list.
var sources = []source{{
	Module:       "sigs.k8s.io/cluster-api/api",
	Version:      "CAPI_VERSION",
	Providers:    []string{"cluster-api (core)", "kubeadm bootstrap", "kubeadm control-plane"},
	MetadataPath: "",
	Packages: []pkg{{
		Dir:   "core/v1beta2",
		Group: "cluster.x-k8s.io",
		Kinds: map[string]string{
			"cluster_types.go":            "Cluster",
			"clusterclass_types.go":       "ClusterClass",
			"machine_types.go":            "Machine",
			"machineset_types.go":         "MachineSet",
			"machinedeployment_types.go":  "MachineDeployment",
			"machinepool_types.go":        "MachinePool",
			"machinehealthcheck_types.go": "MachineHealthCheck",
			"cluster_phase_types.go":      "Cluster",
			"machine_phase_types.go":      "Machine",
			"condition_consts.go":         "*",
			"common_types.go":             "*",
		},
	}, {
		Dir:   "controlplane/kubeadm/v1beta2",
		Group: "controlplane.cluster.x-k8s.io",
		Kinds: map[string]string{"": "KubeadmControlPlane"},
	}, {
		Dir:   "bootstrap/kubeadm/v1beta2",
		Group: "bootstrap.cluster.x-k8s.io",
		Kinds: map[string]string{"": "KubeadmConfig"},
	}, {
		Dir:   "addons/v1beta2",
		Group: "addons.cluster.x-k8s.io",
		Kinds: map[string]string{"": "ClusterResourceSet"},
	}},
}, {
	Module:       "sigs.k8s.io/cluster-api/test",
	Version:      "CAPD_VERSION",
	Providers:    []string{"docker (CAPD)", "in-memory"},
	MetadataPath: "", // released from the cluster-api repo at the same tag as core

	Packages: []pkg{{
		Dir:   "infrastructure/docker/api/v1beta2",
		Group: "infrastructure.cluster.x-k8s.io",
		Kinds: map[string]string{
			"devcluster_types.go":     "DevCluster",
			"devmachine_types.go":     "DevMachine",
			"devmachinepool_types.go": "DevMachinePool",
			"dockercluster_types.go":  "DockerCluster (deprecated)",
			"dockermachine_types.go":  "DockerMachine (deprecated)",
		},
	}},
}, {
	Module:       "github.com/k0sproject/k0smotron",
	Version:      "K0SMOTRON_VERSION",
	Providers:    []string{"k0smotron (control-plane, bootstrap, infrastructure)"},
	MetadataPath: "metadata.yaml",
	Packages: []pkg{{
		Dir:   "api/controlplane/v1beta1",
		Group: "controlplane.cluster.x-k8s.io",
		Kinds: map[string]string{"": "K0sControlPlane / K0smotronControlPlane"},
	}, {
		Dir:   "api/bootstrap/v1beta1",
		Group: "bootstrap.cluster.x-k8s.io",
		Kinds: map[string]string{"": "K0sWorkerConfig"},
	}},
}}

// notes are findings that a constant table cannot express. Each one cites the
// file and line in the pinned release that it was read from; if a note stops
// being true after a version bump, the citation is where to check.
var notes = []string{
	"MachinePool publishes **no v1beta2 condition constants**: the whole block is commented " +
		"out with \"not yet implemented\" (`core/v1beta2/machinepool_types.go:31`). " +
		"`status.conditions` is still `[]metav1.Condition`, and the replica counters " +
		"(`replicas`, `readyReplicas`, `availableReplicas`, `upToDateReplicas`) are populated. " +
		"Anything folding a MachinePool must read counters, not condition types.",
	"The v1beta2 contract is compatible with v1beta1 only temporarily, until v1beta1 is EOL " +
		"(`sigs.k8s.io/cluster-api/internal/contract/version.go:49`). k0smotron is a v1beta1-contract " +
		"provider, so Phases 4-5 carry that risk.",
	"Deprecated v1beta1 conditions live under `status.deprecated.v1beta1.conditions` and are " +
		"marked `deprecated: true` in api-snapshot.json. Fold reads them only as a fallback.",
	"The in-memory backend of the `Dev*` kinds reports four separate provisioning conditions on " +
		"DevMachine (`VMProvisioned`, `EtcdProvisioned`, `APIServerProvisioned`, `NodeProvisioned`), " +
		"each with its own configurable startup duration. That is what makes a stall inducible " +
		"without Docker.",
	"`DockerCluster`/`DockerMachine` still exist in the pinned CAPD but are deprecated in favour " +
		"of the `Dev*` kinds with `spec.backend.docker`; they appear here only so the policy can " +
		"deny them by name.",
}

// Constants that are neither conditions nor reasons but that this repo depends on
// by name: the fold and policy layers key off them.
var extraNames = map[string]bool{
	"ClusterTopologyOwnedLabel":                 true,
	"ClusterNameLabel":                          true,
	"ClusterTopologyMachineDeploymentNameLabel": true,
	"ClusterTopologyMachinePoolNameLabel":       true,
	"ProviderNameLabel":                         true,
	"PausedAnnotation":                          true,
}

type entry struct {
	Kind       string `json:"kind"`
	Group      string `json:"group"`
	Class      string `json:"class"` // condition | reason | phase | label
	Const      string `json:"const"`
	Value      string `json:"value"`
	Doc        string `json:"doc,omitempty"`
	Source     string `json:"source"` // module path + version
	File       string `json:"file"`   // path relative to module root
	Line       int    `json:"line"`
	Package    string `json:"package"`
	Deprecated bool   `json:"deprecated,omitempty"`
}

type contract struct {
	Module    string   `json:"module"`
	Version   string   `json:"version"`
	Contract  string   `json:"contract"`
	Providers []string `json:"providers"`
	Source    string   `json:"source"`
}

type snapshot struct {
	Generated string            `json:"generated_by"`
	Versions  map[string]string `json:"versions"`
	Notes     []string          `json:"notes"`
	Contracts []contract        `json:"contracts"`
	Entries   []entry           `json:"entries"`
}

func main() {
	root := flag.String("root", ".", "repo root")
	flag.Parse()

	versions, err := readEnv(filepath.Join(*root, "versions.env"))
	check(err)

	snap := snapshot{
		Generated: "make api-snapshot (hack/tools/apisnapshot)",
		Versions:  map[string]string{},
		Notes:     notes,
	}
	// Two passes over every package: CAPI aliases most constants across packages
	// (MachinePoolAvailableCondition = clusterv1.AvailableCondition), so no single
	// package can be resolved on its own.
	var raws []raw
	for _, s := range sources {
		version := versions[s.Version]
		if version == "" {
			check(fmt.Errorf("versions.env has no %s", s.Version))
		}
		dir, err := moduleDir(s.Module, version)
		check(err)
		snap.Versions[s.Module] = version
		c, err := readContract(dir, s, version, versions["CAPI_CONTRACT"])
		check(err)
		snap.Contracts = append(snap.Contracts, c)
		for _, p := range s.Packages {
			rs, err := collect(filepath.Join(dir, p.Dir), s.Module+"@"+version, p)
			check(err)
			raws = append(raws, rs...)
		}
	}
	values := map[string]string{}
	for _, r := range raws {
		if v, ok := literal(r.value); ok {
			values[r.name] = v
		}
	}
	snap.Entries = resolve(raws, values)
	sort.Slice(snap.Entries, func(i, j int) bool {
		a, b := snap.Entries[i], snap.Entries[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Class != b.Class {
			return a.Class < b.Class
		}
		return a.Const < b.Const
	})

	out, err := json.MarshalIndent(snap, "", "  ")
	check(err)
	check(os.WriteFile(filepath.Join(*root, "docs", "api-snapshot.json"), append(out, '\n'), 0o644))
	check(os.WriteFile(filepath.Join(*root, "docs", "api-snapshot.md"), []byte(markdown(snap)), 0o644))
	fmt.Printf("api-snapshot: %d constants from %d modules\n", len(snap.Entries), len(snap.Versions))
}

type raw struct {
	name   string
	value  ast.Expr
	typ    string
	doc    string
	kind   string
	group  string
	source string
	file   string
	pkg    string
	line   int
}

func collect(dir, source string, p pkg) ([]raw, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var out []raw
	for _, astPkg := range pkgs {
		for _, name := range sortedFiles(astPkg) {
			f := astPkg.Files[name]
			base := filepath.Base(name)
			kind := p.Kinds[base]
			if kind == "" {
				kind = p.Kinds[""]
			}
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok || len(vs.Values) != 1 || len(vs.Names) != 1 {
						continue
					}
					doc := firstLine(vs.Doc)
					if doc == "" {
						doc = firstLine(gd.Doc)
					}
					out = append(out, raw{
						name: vs.Names[0].Name, value: vs.Values[0], typ: typeName(vs.Type),
						doc: doc, kind: kind, group: p.Group, source: source,
						file: filepath.Join(p.Dir, base), pkg: astPkg.Name,
						line: fset.Position(vs.Names[0].Pos()).Line,
					})
				}
			}
		}
	}
	return out, nil
}

// literal unwraps a string literal or a one-argument conversion of one
// (ClusterPhase("Pending")); anything else is resolved by name in a second pass.
func literal(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.CallExpr:
		if len(v.Args) == 1 {
			return literal(v.Args[0])
		}
	}
	return "", false
}

func resolve(raws []raw, values map[string]string) []entry {
	var out []entry
	for _, r := range raws {
		value, ok := literal(r.value)
		if !ok {
			switch v := r.value.(type) {
			case *ast.Ident:
				value, ok = values[v.Name]
			case *ast.SelectorExpr:
				value, ok = values[v.Sel.Name]
			}
		}
		if !ok || value == "" {
			continue
		}
		class := classify(r.name, r.typ)
		if class == "" {
			// Phase enums are written as conversions: ClusterPhasePending = ClusterPhase("Pending").
			if call, ok := r.value.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && strings.HasSuffix(id.Name, "Phase") {
					class = "phase"
				}
			}
		}
		if class == "" {
			continue
		}
		kind := r.kind
		if kind == "" || kind == "*" {
			kind = inferKind(r.name, class)
		}
		out = append(out, entry{
			Kind: kind, Group: r.group, Class: class, Const: r.name, Value: value,
			Doc: r.doc, Source: r.source, File: r.file, Line: r.line, Package: r.pkg,
			Deprecated: strings.Contains(r.name, "V1Beta1") || strings.Contains(r.file, "v1beta1_condition_consts.go"),
		})
	}
	return out
}

// knownKinds is ordered longest-first so MachineDeployment wins over Machine.
var knownKinds = []string{
	"KubeadmControlPlane", "MachineHealthCheck", "MachineDeployment", "ClusterResourceSet",
	"ClusterClass", "MachinePool", "MachineSet", "DevMachinePool", "DevMachine", "DevCluster",
	"Machine", "Cluster",
}

func inferKind(name, class string) string {
	for _, k := range knownKinds {
		if strings.HasPrefix(name, k) {
			return k
		}
	}
	return sharedKind(class)
}

func classify(name, typ string) string {
	switch {
	case strings.HasSuffix(name, "Condition"):
		return "condition"
	case strings.HasSuffix(name, "Reason"):
		return "reason"
	case strings.HasSuffix(typ, "Phase"):
		return "phase"
	case extraNames[name]:
		return "label"
	}
	return ""
}

func sharedKind(class string) string {
	if class == "label" {
		return "(labels)"
	}
	return "(shared)"
}

func typeName(e ast.Expr) string {
	id, ok := e.(*ast.Ident)
	if !ok {
		return ""
	}
	return id.Name
}

func firstLine(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	for _, c := range g.List {
		t := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"))
		if t == "" || strings.HasPrefix(t, "+") {
			continue
		}
		if i := strings.Index(t, ". "); i > 0 {
			t = t[:i+1]
		}
		return t
	}
	return ""
}

func sortedFiles(p *ast.Package) []string {
	names := make([]string, 0, len(p.Files))
	for n := range p.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func moduleDir(module, version string) (string, error) {
	cmd := exec.Command("go", "mod", "download", "-json", module+"@"+version)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go mod download %s@%s: %w", module, version, err)
	}
	var info struct{ Dir string }
	if err := json.Unmarshal(out, &info); err != nil {
		return "", err
	}
	if info.Dir == "" {
		return "", fmt.Errorf("%s@%s: no Dir in go mod download output", module, version)
	}
	return info.Dir, nil
}

// readContract reads the provider's metadata.yaml and returns the CAPI contract
// version its pinned release series declares. CAPI's own contract comes from the
// repo root, which the api submodule does not ship, so it falls back to the
// contract recorded in versions.env.
func readContract(dir string, s source, version, capiContract string) (contract, error) {
	c := contract{Module: s.Module, Version: version, Providers: s.Providers}
	if s.MetadataPath == "" {
		c.Contract = capiContract
		c.Source = "released from the cluster-api repo at this tag; versions.env CAPI_CONTRACT"
		return c, nil
	}
	path := filepath.Join(dir, s.MetadataPath)
	f, err := os.Open(path)
	if err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	defer f.Close()
	major, minor, ok := majorMinor(version)
	if !ok {
		return c, fmt.Errorf("cannot parse version %q", version)
	}
	var curMajor, curMinor string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "- major:"):
			curMajor = strings.TrimSpace(strings.TrimPrefix(line, "- major:"))
		case strings.HasPrefix(line, "minor:"):
			curMinor = strings.TrimSpace(strings.TrimPrefix(line, "minor:"))
		case strings.HasPrefix(line, "contract:"):
			if curMajor == major && curMinor == minor {
				c.Contract = strings.TrimSpace(strings.TrimPrefix(line, "contract:"))
				c.Source = s.MetadataPath
				return c, sc.Err()
			}
		}
	}
	if err := sc.Err(); err != nil {
		return c, err
	}
	return c, fmt.Errorf("%s declares no contract for %s.%s", path, major, minor)
}

func majorMinor(version string) (string, string, bool) {
	v := strings.TrimPrefix(version, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func readEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	env := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		env[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return env, sc.Err()
}

func markdown(s snapshot) string {
	var b strings.Builder
	b.WriteString("# API snapshot\n\n")
	b.WriteString("Generated by `make api-snapshot`. Every condition type, reason and phase this\n")
	b.WriteString("repo reads is below, with the file and line it came from in the pinned release.\n")
	b.WriteString("Do not name a CAPI condition anywhere in this repo unless it appears here.\n\n")
	b.WriteString("Modules read:\n\n")
	mods := make([]string, 0, len(s.Versions))
	for m := range s.Versions {
		mods = append(mods, m)
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&b, "- `%s@%s`\n", m, s.Versions[m])
	}
	b.WriteString("\n## Notes\n\n")
	for _, n := range notes {
		fmt.Fprintf(&b, "- %s\n", n)
	}
	b.WriteString("\n## Contract check\n\n")
	b.WriteString("The CAPI contract version each pinned provider implements, read from its\n")
	b.WriteString("`metadata.yaml`. A provider on `v1beta1` still works on CAPI v1.14 (v1beta2 is\n")
	b.WriteString("temporarily compatible with v1beta1, `internal/contract/version.go:49`) but is a\n")
	b.WriteString("risk once v1beta1 is removed.\n\n")
	b.WriteString("| provider | module | version | contract | read from |\n|---|---|---|---|---|\n")
	for _, c := range s.Contracts {
		fmt.Fprintf(&b, "| %s | `%s` | %s | `%s` | %s |\n", strings.Join(c.Providers, ", "), c.Module, c.Version, c.Contract, c.Source)
	}
	b.WriteString("\n")

	byKind := map[string][]entry{}
	var kinds []string
	for _, e := range s.Entries {
		if _, seen := byKind[e.Kind]; !seen {
			kinds = append(kinds, e.Kind)
		}
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		fmt.Fprintf(&b, "## %s\n\n", k)
		for _, class := range []string{"condition", "phase", "reason", "label"} {
			var rows []entry
			for _, e := range byKind[k] {
				if e.Class == class {
					rows = append(rows, e)
				}
			}
			if len(rows) == 0 {
				continue
			}
			fmt.Fprintf(&b, "### %ss\n\n", class)
			b.WriteString("| value | constant | source |\n|---|---|---|\n")
			for _, e := range rows {
				fmt.Fprintf(&b, "| `%s` | `%s` | %s `%s:%d` |\n", e.Value, e.Const, shortMod(e.Source), e.File, e.Line)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func shortMod(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "apisnapshot:", err)
		os.Exit(1)
	}
}
