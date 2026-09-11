package msg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fullVars fills every field, so a template that references one still renders.
func fullVars() Vars {
	return Vars{
		Object: "KubeadmControlPlane/dev-1", Field: "spec.topology.controlPlane.replicas",
		Kind: "KubeadmControlPlane", Class: "std", Variable: "gpu", Reason: "WaitingForEtcd",
		Message: "etcd member is not healthy", Since: "3m12s", Version: "v1.34.11",
		Namespace: "default", Ready: 1, Desired: 3,
	}
}

func TestUX_EveryEntryHasNextAction(t *testing.T) {
	for _, code := range Codes() {
		e, _ := Get(code)
		require.NotEmpty(t, e.NextAction, "%s: every message ends with something the user can do", code)
		require.NotEmpty(t, e.Title, "%s: needs a title", code)
	}
}

func TestUX_EveryCodeRenders(t *testing.T) {
	for _, code := range Codes() {
		got := Render(code, fullVars())
		require.NotEmpty(t, got, "%s rendered empty", code)
		require.NotContains(t, got, "<no value>", "%s left a placeholder unfilled", code)
		require.NotContains(t, got, "{{", "%s left a template action unexecuted", code)
	}
}

// The stall line contract from D2: one line, under budget, naming the object and an
// elapsed time. The renderer adds the raw: line; this covers the message itself.
func TestUX_StallLineContract(t *testing.T) {
	for _, code := range Codes() {
		e, _ := Get(code)
		if e.Class != Stall {
			continue
		}
		got := Render(code, fullVars())
		require.LessOrEqual(t, len(got), 120, "%s: stall line is %d chars: %q", code, len(got), got)
		require.NotContains(t, got, "\n", "%s: a stall line is one line", code)
		require.Contains(t, got, "KubeadmControlPlane/dev-1", "%s: a stall line names kind/name", code)
		require.Contains(t, got, "3m12s", "%s: a stall line carries an elapsed time", code)
	}
}

// Every deny message names the offending path, the class, and the break-glass, in
// at most two sentences (D2.3).
func TestUX_AdmissionMessageContract(t *testing.T) {
	for _, code := range Codes() {
		e, _ := Get(code)
		if e.Class != Denial {
			continue
		}
		got := Render(code, fullVars())
		require.Contains(t, strings.ToLower(got), "break-glass", "%s", code)
		require.LessOrEqual(t, strings.Count(got, ". "), 1, "%s: at most two sentences: %q", code, got)
		hasPath := strings.Contains(got, "spec.topology") || strings.Contains(got, "KubeadmControlPlane")
		require.True(t, hasPath, "%s: names the offending field or kind: %q", code, got)
	}
}

// Raw CAPI condition type names are jargon. They belong on raw: lines and under
// --verbose, never in a message. The deny-list is generated from the pinned API,
// so it updates itself when CAPI does (D2.5).
func TestUX_NoJargonInMessages(t *testing.T) {
	deny := conditionTypeNames(t)
	require.NotEmpty(t, deny, "the jargon deny-list must come from docs/api-snapshot.json")
	for _, code := range Codes() {
		e, _ := Get(code)
		for _, text := range []string{e.Title, Render(code, Vars{})} {
			for _, jargon := range deny {
				require.NotContains(t, text, jargon, "%s: %q is a raw condition type name", code, jargon)
			}
		}
	}
}

func TestUX_EveryCodeHasALongform(t *testing.T) {
	for _, code := range Codes() {
		text, ok := Longform(code)
		require.True(t, ok, "%s has no long form in internal/msg/longform/", code)
		lower := strings.ToLower(text)
		// The long form says what happened, why, and what to do next (D5.3).
		require.Contains(t, lower, "what happened:", "%s", code)
		require.Contains(t, lower, "why:", "%s", code)
		require.Contains(t, lower, "next:", "%s", code)
	}
}

func TestUX_NoOrphanLongforms(t *testing.T) {
	known := map[Code]bool{}
	for _, c := range Codes() {
		known[c] = true
	}
	for _, c := range LongformCodes() {
		require.True(t, known[c], "longform/%s.md has no entry in the registry", c)
	}
}

func TestTruncate_CutsAtAWordBoundary(t *testing.T) {
	const long = "waiting for the bootstrap provider to produce the data secret for this machine"
	got := Truncate(long, 40)
	require.LessOrEqual(t, len(got), 40)
	require.True(t, strings.HasSuffix(got, "…"), "got %q", got)
	require.False(t, strings.Contains(got, "  "))
	require.Equal(t, "short", Truncate("short", 40))
	require.Equal(t, "collapsed spaces", Truncate("collapsed   spaces", 40))
}

// conditionTypeNames reads the condition type values out of the generated API
// snapshot. Nothing here is typed by hand.
func conditionTypeNames(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("../../docs/api-snapshot.json")
	require.NoError(t, err)
	var snap struct {
		Entries []struct {
			Class string `json:"class"`
			Value string `json:"value"`
		} `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(b, &snap))
	seen := map[string]bool{}
	var out []string
	for _, e := range snap.Entries {
		// Single-word values like "Ready" or "Available" are ordinary English and
		// would make the lint useless; the jargon is the compound CamelCase names.
		if e.Class != "condition" || seen[e.Value] || !isCompoundCamel(e.Value) {
			continue
		}
		seen[e.Value] = true
		out = append(out, e.Value)
	}
	return out
}

func isCompoundCamel(s string) bool {
	upper := 0
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			upper++
		}
	}
	return upper >= 2
}

// The runbooks are written in docs/runbooks and embedded in the binary so
// `cluster docs` works offline. Two copies, one source: this fails if they drift.
func TestUX_EmbeddedRunbooksMatchDocs(t *testing.T) {
	docs, err := os.ReadDir("../../docs/runbooks")
	require.NoError(t, err)
	require.NotEmpty(t, docs)

	for _, entry := range docs {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		want, err := os.ReadFile("../../docs/runbooks/" + entry.Name())
		require.NoError(t, err)
		got, ok := Runbook(Code(strings.TrimSuffix(entry.Name(), ".md")))
		require.True(t, ok, "%s is not embedded; run: make sync-embeds", entry.Name())
		require.Equal(t, string(want), got, "%s has drifted; run: make sync-embeds", entry.Name())
	}
}

// Every stall class has a runbook, and no runbook exists for a code that cannot
// be emitted.
func TestUX_RunbookPerStallClass(t *testing.T) {
	for _, code := range Codes() {
		entry, _ := Get(code)
		_, ok := Runbook(code)
		if entry.Class == Stall {
			require.True(t, ok, "%s is a stall class with no runbook", code)
			continue
		}
		require.False(t, ok, "%s is not a stall class but has a runbook", code)
	}
}

// A runbook opens by showing what the user just saw. If the renderer's stall
// block changes shape, that opening becomes a lie, so the prefixes are checked
// against the ones internal/render actually emits.
func TestUX_RunbookExamplesMatchTheRealStallBlock(t *testing.T) {
	for _, code := range Codes() {
		entry, _ := Get(code)
		if entry.Class != Stall {
			continue
		}
		text, ok := Runbook(code)
		require.True(t, ok, "%s", code)

		require.Contains(t, text, "## What you are seeing", "%s", code)
		require.Contains(t, text, "\nraw: kubectl get ", "%s: the raw line is unindented and copy-pasteable", code)
		require.Contains(t, text, "\nnext: "+entry.NextAction,
			"%s: the next line must be the registry's action, which is what the renderer prints", code)
		require.NotContains(t, text, "\n  raw: ", "%s: the raw line must not be indented", code)
		require.NotContains(t, text, "\n  Next: ", "%s: the renderer writes `next:`, not `Next:`", code)
	}
}

// The jargon lint runs over the docs as well as the code. A user-facing page that
// names a CAPI condition type has leaked an implementation detail into the one
// place a reader is meant to be able to trust; the runbooks are exempt, because
// naming conditions is what a runbook is for.
func TestUX_NoJargonInUserFacingDocs(t *testing.T) {
	deny := conditionTypeNames(t)
	require.NotEmpty(t, deny)

	for _, path := range []string{
		"../../README.md",
		"../../docs/user-guide.md",
		"../../docs/eject.md",
		"../../docs/ai.md",
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			b, err := os.ReadFile(path)
			require.NoError(t, err)

			for i, line := range prose(string(b)) {
				for _, jargon := range deny {
					require.NotContains(t, line, jargon,
						"%s:%d names the condition type %q outside a code block", path, i+1, jargon)
				}
			}
		})
	}
}

// prose returns the lines a reader reads as English: fenced code blocks and
// backticked spans are where a command or a raw condition name belongs.
func prose(text string) []string {
	var out []string
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			out = append(out, "")
			continue
		}
		if fenced || strings.HasPrefix(line, "    ") {
			out = append(out, "")
			continue
		}
		out = append(out, stripInlineCode(line))
	}
	return out
}

func stripInlineCode(line string) string {
	var b strings.Builder
	inCode := false
	for _, r := range line {
		if r == '`' {
			inCode = !inCode
			continue
		}
		if !inCode {
			b.WriteRune(r)
		}
	}
	return b.String()
}
