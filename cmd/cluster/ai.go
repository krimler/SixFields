package main

import (
	"context"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"sixfields/internal/explain"
	"sixfields/internal/msg"
	"sixfields/internal/render"
	"sixfields/internal/snapshot"
	"sixfields/internal/why"
)

// DefaultLocalURL is where the local runtime is expected when CLUSTER_AI_URL is
// unset. It has to match CLUSTER_AI_URL in versions.env, which is what
// `make doctor-ai` writes and what the scripts export;
// TestUX_DefaultLocalURLMatchesVersionsEnv holds the two together.
const DefaultLocalURL = "http://127.0.0.1:11434/v1"

// aiOptions are the cost and privacy knobs from docs/ai.md.
type aiOptions struct {
	enabled   bool
	anonymize string // "", "on", "off" — empty means the backend's default
	redactIPs bool
}

// backend picks an explainer from the environment. `local` is the default and
// with it nothing leaves the machine; `anthropic` is opt-in and turns
// anonymisation on. `noop` and `cassette` exist so tests never call a model.
func backend() (explain.Explainer, string, error) {
	mode, err := explain.ParseMode(os.Getenv("CLUSTER_AI"))
	if err != nil {
		return nil, "", &msg.Error{Code: msg.NoRuntime, Summary: err.Error(),
			Action: "set CLUSTER_AI=off, explain or all"}
	}
	if mode == explain.Off {
		return nil, "off", nil
	}
	switch strings.ToLower(os.Getenv("CLUSTER_AI_BACKEND")) {
	case "noop":
		return explain.Noop{}, "noop", nil
	case "cassette":
		return explain.Cassette{Dir: cassetteDir()}, "cassette", nil
	case "anthropic":
		// A separate variable on purpose. CLUSTER_AI_MODEL names the local model
		// (versions.env pins it), and sending that id to Anthropic asks for a model
		// that does not exist there.
		return explain.Anthropic{Model: os.Getenv("ANTHROPIC_MODEL")}, "anthropic", nil
	default:
		url := os.Getenv("CLUSTER_AI_URL")
		if url == "" {
			url = DefaultLocalURL
		}
		return explain.Local{URL: url, Model: os.Getenv("CLUSTER_AI_MODEL")}, "local", nil
	}
}

func cassetteDir() string {
	if dir := os.Getenv("CLUSTER_AI_CASSETTES"); dir != "" {
		return dir
	}
	return "testdata/cassettes"
}

// anonymizeByDefault is on for anything that leaves the machine and off for
// anything that does not. docs/ai.md says which is active.
func anonymizeByDefault(name string) bool { return name == "anthropic" }

// explainStall prints the runbook first and the model's three lines under it. If
// the model is slow, wrong or absent, the runbook stands alone — the ladder never
// depends on the top rung.
func explainStall(ctx context.Context, cmd *cobra.Command, stall why.Stall, view render.View, env snapshot.Envelope, opt aiOptions) {
	if !opt.enabled {
		// The runbook is its own rung: `cluster docs <code>` prints it. Printing it
		// unasked would bury the one line `why` exists to show.
		return
	}
	// --explain prints the runbook first and streams the model's lines under it, so
	// a slow or absent model costs nothing but itself (D4.1, the latency contract).
	runbook, hasRunbook := msg.Runbook(stall.Code)
	if hasRunbook {
		outln(cmd)
		out(cmd, runbook)
	}

	explainer, name, err := backend()
	if err != nil || explainer == nil {
		if err != nil {
			cmd.PrintErrln(err.Error())
		}
		return
	}

	req := explain.Request{
		Code: stall.Code, Stall: stall, Phases: view.Result.Phases,
		Runbook: runbook, Names: objectNames(env),
	}

	hide := anonymizeByDefault(name)
	switch opt.anonymize {
	case "on":
		hide = true
	case "off":
		hide = false
	}
	anon := explain.NewAnonymizer()
	anon.RedactIPs = opt.redactIPs
	sent := req
	if hide || opt.redactIPs {
		anon.Learn(req.Names...)
		sent = anon.HideRequest(req)
	}

	answer, err := explainer.Explain(ctx, sent)
	if err != nil {
		cmd.PrintErrf("--explain: %v (the runbook above still applies)\n", err)
		return
	}
	if hide {
		answer = anon.RevealExplanation(answer)
	}
	if err := answer.Validate(stall.Code); err != nil {
		cmd.PrintErrf("--explain: discarded, %v\n", err)
		return
	}
	// Grounding is not advisory: an explanation that names something the analyzer
	// never saw is worse than no explanation.
	if err := answer.Grounded(req); err != nil {
		cmd.PrintErrf("--explain: discarded, %v\n", err)
		return
	}

	outln(cmd)
	for _, line := range answer.Lines {
		outln(cmd, line)
	}
	outln(cmd, "next: "+answer.NextCommand)
}

func objectNames(env snapshot.Envelope) []string {
	out := make([]string, 0, len(env.Objects)*2)
	for _, o := range env.Objects {
		out = append(out, o.Kind()+"/"+o.Name(), o.Name())
	}
	return out
}
