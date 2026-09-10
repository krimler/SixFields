package explain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Noop echoes the analyzer's own output back. Every AI code path — the flag, the
// timeout, the grounding check, the renderer — is exercised by it, with no model
// and no network.
type Noop struct{}

func (Noop) Explain(_ context.Context, req Request) (Explanation, error) {
	return Explanation{
		Code: string(req.Code),
		Lines: []string{
			fmt.Sprintf("%s is what the cluster is waiting on.", req.Stall.Object),
			firstNonEmpty(req.Stall.Message, "It has not reported a reason."),
			fmt.Sprintf("The %s phase finishes when it does.", req.Stall.Phase),
		},
		NextCommand: req.Stall.Raw,
	}, nil
}

// Cassette replays a recorded response. CI is deterministic and free, and the
// cassettes in this repo are recorded from the pinned local model, not from a
// paid API.
type Cassette struct {
	Dir string
	// Record writes a cassette when one is missing, using Inner.
	Record bool
	Inner  Explainer
}

func (c Cassette) Explain(ctx context.Context, req Request) (Explanation, error) {
	path := filepath.Join(c.Dir, Key(req)+".json")
	b, err := os.ReadFile(path)
	if err == nil {
		var out Explanation
		if err := json.Unmarshal(b, &out); err != nil {
			return Explanation{}, fmt.Errorf("%s: %w", path, err)
		}
		return out, nil
	}
	if !c.Record || c.Inner == nil {
		return Explanation{}, fmt.Errorf("no cassette at %s: run `make test-llm` with CLUSTER_AI_RECORD=1 to record one", path)
	}
	out, err := c.Inner.Explain(ctx, req)
	if err != nil {
		return Explanation{}, err
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return Explanation{}, err
	}
	recorded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return Explanation{}, err
	}
	return out, os.WriteFile(path, append(recorded, '\n'), 0o644)
}

// Key is the cassette name and the cache key: a hash of everything the model is
// given, so an identical stall is answered once.
func Key(req Request) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		string(req.Code), req.Stall.Object.String(), req.Stall.Reason,
		req.Stall.FullMessage, req.Stall.Message, phaseText(req.Phases),
	}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

// Prompt is what every model backend sends. It is built from the analyzer's
// output only, and it says plainly that inventing a name is the one unacceptable
// failure — the grounding check enforces it either way.
func Prompt(req Request) string {
	var b strings.Builder
	b.WriteString("A Cluster API cluster has stopped making progress. A deterministic analyzer has already\n")
	b.WriteString("decided which object is to blame. Explain its finding to the person waiting.\n\n")
	fmt.Fprintf(&b, "Stall class: %s\n", req.Code)
	fmt.Fprintf(&b, "Blocking object: %s\n", req.Stall.Object)
	fmt.Fprintf(&b, "Reason reported by the provider: %s\n", req.Stall.Reason)
	fmt.Fprintf(&b, "Message: %s\n", firstNonEmpty(req.Stall.FullMessage, req.Stall.Message))
	fmt.Fprintf(&b, "Phase: %s\n", req.Stall.Phase)
	b.WriteString("\nPhases:\n")
	for _, p := range req.Phases {
		fmt.Fprintf(&b, "  %-15s %-8s %s\n", p.Name, p.State, p.Detail)
	}
	if req.Runbook != "" {
		b.WriteString("\nRunbook for this stall class:\n")
		b.WriteString(req.Runbook)
	}
	b.WriteString("\nReturn JSON only, matching this shape exactly:\n")
	b.WriteString(`{"code":"<the stall class above>","lines":["<what is blocked>","<why>","<what changes when it is fixed>"],"next_command":"<one command to run>"}`)
	b.WriteString("\n\nRules: exactly three lines, each one sentence and under 160 characters. Use no\n")
	b.WriteString("Kubernetes condition type names. Every object name and every number you write must\n")
	b.WriteString("appear above — if you are not sure of a name, leave it out. next_command must start\n")
	b.WriteString("with kubectl, cluster, clusterctl, docker or make.\n")
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// parse pulls the JSON object out of a model response that may have wrapped it in
// prose or a fence. Anything else is a failure, and a failure means the runbook
// stands alone rather than a half-parsed answer reaching the user.
func parse(text string) (Explanation, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return Explanation{}, fmt.Errorf("no JSON object in the model response")
	}
	var out Explanation
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return Explanation{}, fmt.Errorf("model response is not the expected JSON: %w", err)
	}
	return out, nil
}
