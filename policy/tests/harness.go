// Package tests compiles the ValidatingAdmissionPolicy YAML in policy/vap and
// evaluates it with cel-go, so every denied field has a test that runs in
// milliseconds and needs no cluster.
//
// The policy files are the source of truth: this harness reads them, it does not
// restate them. A rule that exists only here would prove nothing.
package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"k8s.io/apiserver/pkg/cel/library"
	"sigs.k8s.io/yaml"
)

// Policy is one ValidatingAdmissionPolicy, compiled.
type Policy struct {
	Name             string
	matchConditions  []named
	variables        []named
	validations      []validation
	auditAnnotations []named
	env              *cel.Env
}

type named struct {
	name    string
	program cel.Program
	source  string
}

type validation struct {
	expression cel.Program
	message    cel.Program
	reason     string
	source     string
}

// Request is one admission request, in the shape the policy sees it.
type Request struct {
	Object    map[string]any
	OldObject map[string]any
	Username  string
	Groups    []string
	Operation string
	Resource  string
	Kind      string
}

// Decision is what the API server would do.
type Decision struct {
	Allowed bool
	Message string
	Reason  string
	// Audit is the annotations the binding would record, empty values dropped
	// exactly as the API server drops them.
	Audit map[string]string
	// Matched is false when the policy's matchConditions excluded the request,
	// which is how a controller is exempted.
	Matched bool
}

// Load compiles every policy in a file.
func Load(path string) ([]*Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []*Policy
	for _, doc := range splitYAML(string(b)) {
		var head struct {
			Kind     string            `json:"kind"`
			Metadata map[string]string `json:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(doc), &head); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if head.Kind != "ValidatingAdmissionPolicy" {
			continue
		}
		policy, err := compile([]byte(doc))
		if err != nil {
			return nil, fmt.Errorf("%s (%s): %w", path, head.Metadata["name"], err)
		}
		out = append(out, policy)
	}
	return out, nil
}

// LoadDir compiles every policy in a directory, which is what `make test` runs.
func LoadDir(dir string) ([]*Policy, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []*Policy
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		policies, err := Load(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, policies...)
	}
	return out, nil
}

type policyDoc struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		MatchConditions []struct {
			Name       string `json:"name"`
			Expression string `json:"expression"`
		} `json:"matchConditions"`
		Variables []struct {
			Name       string `json:"name"`
			Expression string `json:"expression"`
		} `json:"variables"`
		Validations []struct {
			Expression        string `json:"expression"`
			Message           string `json:"message"`
			MessageExpression string `json:"messageExpression"`
			Reason            string `json:"reason"`
		} `json:"validations"`
		AuditAnnotations []struct {
			Key             string `json:"key"`
			ValueExpression string `json:"valueExpression"`
		} `json:"auditAnnotations"`
	} `json:"spec"`
}

func compile(doc []byte) (*Policy, error) {
	var parsed policyDoc
	if err := yaml.Unmarshal(doc, &parsed); err != nil {
		return nil, err
	}

	env, err := newEnv()
	if err != nil {
		return nil, err
	}
	p := &Policy{Name: parsed.Metadata.Name, env: env}

	for _, m := range parsed.Spec.MatchConditions {
		program, err := program(env, m.Expression)
		if err != nil {
			return nil, fmt.Errorf("matchCondition %s: %w", m.Name, err)
		}
		p.matchConditions = append(p.matchConditions, named{name: m.Name, program: program, source: m.Expression})
	}
	for _, v := range parsed.Spec.Variables {
		program, err := program(env, v.Expression)
		if err != nil {
			return nil, fmt.Errorf("variable %s: %w", v.Name, err)
		}
		p.variables = append(p.variables, named{name: v.Name, program: program, source: v.Expression})
	}
	for i, v := range parsed.Spec.Validations {
		expr, err := program(env, v.Expression)
		if err != nil {
			return nil, fmt.Errorf("validation %d: %w", i, err)
		}
		val := validation{expression: expr, reason: v.Reason, source: v.Expression}
		if v.MessageExpression != "" {
			message, err := program(env, v.MessageExpression)
			if err != nil {
				return nil, fmt.Errorf("validation %d messageExpression: %w", i, err)
			}
			val.message = message
		}
		p.validations = append(p.validations, val)
	}
	for _, a := range parsed.Spec.AuditAnnotations {
		program, err := program(env, a.ValueExpression)
		if err != nil {
			return nil, fmt.Errorf("auditAnnotation %s: %w", a.Key, err)
		}
		p.auditAnnotations = append(p.auditAnnotations, named{name: a.Key, program: program, source: a.ValueExpression})
	}
	return p, nil
}

// newEnv declares what a ValidatingAdmissionPolicy expression may reference, with
// the same CEL extension libraries the API server registers.
func newEnv() (*cel.Env, error) {
	opts := []cel.EnvOption{
		cel.Variable("object", cel.DynType),
		cel.Variable("oldObject", cel.DynType),
		cel.Variable("request", cel.DynType),
		cel.Variable("params", cel.DynType),
		cel.Variable("namespaceObject", cel.DynType),
		cel.Variable("variables", cel.MapType(cel.StringType, cel.DynType)),
		cel.OptionalTypes(),
	}
	for _, lib := range library.KnownLibraries() {
		opts = append(opts, cel.Lib(lib))
	}
	return cel.NewEnv(opts...)
}

func program(env *cel.Env, expression string) (cel.Program, error) {
	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}
	return env.Program(ast)
}

// Evaluate runs the policy the way the API server does: match conditions, then
// variables in declaration order, then each validation until one fails.
func (p *Policy) Evaluate(req Request) (Decision, error) {
	activation := map[string]any{
		"object":    req.Object,
		"oldObject": req.OldObject,
		"request":   requestObject(req),
		"params":    nil,
		"variables": map[string]any{},
	}

	for _, m := range p.matchConditions {
		out, _, err := m.program.Eval(activation)
		if err != nil {
			return Decision{}, fmt.Errorf("%s: matchCondition %s: %w", p.Name, m.name, err)
		}
		matched, ok := out.Value().(bool)
		if !ok {
			return Decision{}, fmt.Errorf("%s: matchCondition %s did not return a bool", p.Name, m.name)
		}
		if !matched {
			return Decision{Allowed: true, Matched: false}, nil
		}
	}

	// VAP variables are lazy and may only reference earlier ones, so evaluating
	// in declaration order is equivalent.
	vars := map[string]any{}
	activation["variables"] = vars
	for _, v := range p.variables {
		out, _, err := v.program.Eval(activation)
		if err != nil {
			return Decision{}, fmt.Errorf("%s: variable %s: %w", p.Name, v.name, err)
		}
		vars[v.name] = out
	}

	decision := Decision{Allowed: true, Matched: true, Audit: map[string]string{}}
	for _, a := range p.auditAnnotations {
		out, _, err := a.program.Eval(activation)
		if err != nil {
			return Decision{}, fmt.Errorf("%s: auditAnnotation %s: %w", p.Name, a.name, err)
		}
		// The API server drops empty audit annotation values.
		if value, ok := out.Value().(string); ok && value != "" {
			decision.Audit[a.name] = value
		}
	}

	for _, v := range p.validations {
		out, _, err := v.expression.Eval(activation)
		if err != nil {
			return Decision{}, fmt.Errorf("%s: validation %q: %w", p.Name, short(v.source), err)
		}
		allowed, ok := out.Value().(bool)
		if !ok {
			return Decision{}, fmt.Errorf("%s: validation %q did not return a bool", p.Name, short(v.source))
		}
		if allowed {
			continue
		}
		decision.Allowed = false
		decision.Reason = v.reason
		if v.message != nil {
			message, _, err := v.message.Eval(activation)
			if err != nil {
				return Decision{}, fmt.Errorf("%s: messageExpression: %w", p.Name, err)
			}
			decision.Message = asString(message)
		}
		return decision, nil
	}
	return decision, nil
}

func requestObject(req Request) map[string]any {
	operation := req.Operation
	if operation == "" {
		operation = "CREATE"
	}
	groups := req.Groups
	if groups == nil {
		groups = []string{"system:authenticated"}
	}
	return map[string]any{
		"operation": operation,
		"userInfo":  map[string]any{"username": req.Username, "groups": groups},
		"resource": map[string]any{
			"group": groupOf(req.Object), "version": "v1beta2", "resource": req.Resource,
		},
		"kind": map[string]any{
			"group": groupOf(req.Object), "version": "v1beta2", "kind": req.Kind,
		},
		"name":      nameOf(req.Object),
		"namespace": "default",
	}
}

func groupOf(object map[string]any) string {
	apiVersion, _ := object["apiVersion"].(string)
	group, _, _ := strings.Cut(apiVersion, "/")
	if !strings.Contains(apiVersion, "/") {
		return ""
	}
	return group
}

func nameOf(object map[string]any) string {
	metadata, _ := object["metadata"].(map[string]any)
	name, _ := metadata["name"].(string)
	return name
}

func asString(v ref.Val) string {
	if s, ok := v.Value().(string); ok {
		return s
	}
	if v.Type() == types.StringType {
		return fmt.Sprint(v.Value())
	}
	return fmt.Sprint(v.Value())
}

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}

func splitYAML(text string) []string {
	var out []string
	for _, doc := range strings.Split(text, "\n---\n") {
		if strings.TrimSpace(doc) != "" {
			out = append(out, doc)
		}
	}
	return out
}
