// Package explain is the only place a model is ever called. Everything it is
// given comes from the deterministic analyzer in internal/why, and everything it
// returns is checked against that input before a user sees it: the model explains
// the analyzer's answer, it never produces one.
//
// Four backends behind one interface — noop, cassette, local and anthropic — so
// every AI code path is testable without a model, and `make test` never calls one.
package explain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"capi-distro/internal/fold"
	"capi-distro/internal/msg"
	"capi-distro/internal/why"
)

// SchemaVersion is docs/schema/explain.v1.json. Output that does not validate is
// discarded and the runbook stands alone.
const SchemaVersion = "v1"

// Timeout is the hard ceiling on a model call. The runbook is printed first and
// the explanation streams under it, so a slow model costs nothing but itself.
const Timeout = 60 * time.Second

// Request is everything a backend is given. There is no free-text field: a
// backend cannot be handed anything the analyzer did not produce.
type Request struct {
	Code    msg.Code
	Stall   why.Stall
	Phases  []fold.Phase
	Runbook string
	// Names are the object names that appear in the envelope. The grounding check
	// uses them, and the anonymiser replaces them.
	Names []string
}

// Explanation is the shape docs/schema/explain.v1.json describes.
type Explanation struct {
	Code        string   `json:"code"`
	Lines       []string `json:"lines"`
	NextCommand string   `json:"next_command"`
}

type Explainer interface {
	Explain(ctx context.Context, req Request) (Explanation, error)
}

// ErrNoBackend is returned when AI is switched off. Callers print the runbook and
// carry on: an explanation is a rung, never the answer.
var ErrNoBackend = errors.New("no explainer backend configured")

// Mode is the CLUSTER_AI setting. `explain` is the default: the one AI rung that
// earns its place, and nothing else calls a model.
type Mode string

const (
	Off        Mode = "off"
	ExplainOne Mode = "explain"
	All        Mode = "all"
)

func ParseMode(value string) (Mode, error) {
	switch Mode(strings.ToLower(strings.TrimSpace(value))) {
	case "", ExplainOne:
		return ExplainOne, nil
	case Off:
		return Off, nil
	case All:
		return All, nil
	}
	return "", fmt.Errorf("CLUSTER_AI must be off, explain or all, not %q", value)
}

// Validate applies the schema's rules in code, so an invalid response is caught
// whether it came from a model, a cassette or a test.
func (e Explanation) Validate(want msg.Code) error {
	if e.Code != string(want) {
		return fmt.Errorf("explanation is for %s, the analyzer emitted %s", e.Code, want)
	}
	if len(e.Lines) != 3 {
		return fmt.Errorf("explanation has %d lines, the schema requires exactly 3", len(e.Lines))
	}
	for i, line := range e.Lines {
		if line == "" || len(line) > 160 || strings.Contains(line, "\n") {
			return fmt.Errorf("line %d does not fit the schema: %q", i+1, line)
		}
	}
	if e.NextCommand == "" || len(e.NextCommand) > 300 || strings.Contains(e.NextCommand, "\n") {
		return fmt.Errorf("next_command does not fit the schema: %q", e.NextCommand)
	}
	verb, _, _ := strings.Cut(e.NextCommand, " ")
	switch verb {
	case "kubectl", "cluster", "clusterctl", "docker", "make":
	default:
		return fmt.Errorf("next_command must start with a real command, not %q", verb)
	}
	return nil
}

// Grounded checks that every object name and every number in the explanation
// appears in what the model was given. This is the deterministic half of D4: a
// model that invents a machine name fails the build rather than the user.
func (e Explanation) Grounded(req Request) error {
	haystack := strings.ToLower(strings.Join([]string{
		req.Stall.Object.String(), req.Stall.Reason, req.Stall.Message, req.Stall.FullMessage,
		req.Stall.Raw, string(req.Code), strings.Join(req.Names, " "), phaseText(req.Phases),
	}, " "))

	for _, line := range e.Lines {
		for _, token := range groundableTokens(line) {
			if !strings.Contains(haystack, strings.ToLower(token)) {
				return fmt.Errorf("%q is not in what the model was given", token)
			}
		}
	}
	return nil
}

func phaseText(phases []fold.Phase) string {
	var b strings.Builder
	for _, p := range phases {
		fmt.Fprintf(&b, " %s %s %s", p.Name, p.State, p.Detail)
	}
	return b.String()
}

// groundableTokens are the parts of a sentence that can be wrong in a way that
// matters: an object reference (Kind/name) and a number. Ordinary words are the
// model's job and are not checked.
func groundableTokens(line string) []string {
	var out []string
	for _, field := range strings.Fields(line) {
		trimmed := strings.Trim(field, ".,;:()[]\"'`")
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "/") && !strings.HasPrefix(trimmed, "/") {
			out = append(out, trimmed)
			continue
		}
		if isNumeric(trimmed) {
			out = append(out, trimmed)
		}
	}
	return out
}

func isNumeric(s string) bool {
	digits := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case r == '.' || r == '%' || r == 'm' || r == 's' || r == 'h':
		default:
			return false
		}
	}
	return digits
}
