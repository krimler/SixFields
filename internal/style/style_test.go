package style_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// notOurs are files this project did not write. The build spec and its two copies
// are the specification handed over at the start; the cassettes are recorded model
// output.
var notOurs = map[string]bool{
	"docs/build-spec.md": true,
	"PLAN.md":            true,
	"CLAUDE.md":          true,
	"LICENSE":            true,
	// This file has to contain what it forbids.
	"internal/style/style_test.go": true,
}

func ours(t *testing.T) []string {
	t.Helper()
	// From the repo root, so the paths are the ones notOurs and userFacing expect.
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	require.NoError(t, err, "the style lint needs a git checkout")

	var files []string
	for _, path := range strings.Fields(string(out)) {
		if notOurs[path] || strings.HasPrefix(path, "testdata/cassettes") {
			continue
		}
		if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".md") ||
			strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".sh") {
			files = append(files, path)
		}
	}
	require.NotEmpty(t, files)
	return files
}

const repoRoot = "../.."

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, path))
	require.NoError(t, err)
	return string(b)
}

// An em dash is the punctuation of a sentence that was written to sound
// considered. Use a comma, a colon, or two sentences.
func TestStyle_NoEmDashes(t *testing.T) {
	for _, path := range ours(t) {
		for i, line := range strings.Split(read(t, path), "\n") {
			require.NotContains(t, line, "—", "%s:%d", path, i+1)
			require.NotContains(t, line, "–", "%s:%d", path, i+1)
		}
	}
}

// padding is a contrast that carries nothing. The sentence has one thing to say,
// so it should say it. These are banned everywhere.
var padding = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bnot (just|only|merely|simply)\b`),
	regexp.MustCompile(`(?i)\bbut rather\b`),
	regexp.MustCompile(`(?i)\bas opposed to\b`),
	regexp.MustCompile(`(?i)\bmore than just\b`),
	regexp.MustCompile(`(?i)\bis(n't| not) about .*,? it'?s about\b`),
	regexp.MustCompile(`(?i)\bthink of it less as\b`),
}

func TestStyle_NoPaddedContrasts(t *testing.T) {
	for _, path := range ours(t) {
		for i, line := range strings.Split(read(t, path), "\n") {
			for _, pattern := range padding {
				require.NotRegexp(t, pattern, line, "%s:%d: say the thing plainly", path, i+1)
			}
		}
	}
}

// comparison names an alternative. In a comment or a DECISIONS.md entry that is
// the substance: "we hand it to kubectl <comparison> reimplementing apply" records
// the choice that was made. In something a user reads it is padding, because the
// user never considered the alternative. So it is banned in prose a user meets
// and allowed where a maintainer is explaining a decision.
var comparison = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\brather than\b`),
	regexp.MustCompile(`(?i)\binstead of\b`),
}

// userFacing is everything a person reads without opening the source.
func userFacing(path string) bool {
	switch {
	case path == "README.md":
		return true
	case strings.HasPrefix(path, "docs/") && strings.HasSuffix(path, ".md"):
		return path != "docs/api-snapshot.md"
	case strings.HasPrefix(path, "internal/msg/longform/"):
		return true
	case strings.HasPrefix(path, "internal/msg/runbooks/"):
		return true
	case strings.HasSuffix(path, "SKILL.md"):
		return true
	}
	return false
}

func TestStyle_NoComparisonsInProse(t *testing.T) {
	checked := 0
	for _, path := range ours(t) {
		if !userFacing(path) {
			continue
		}
		checked++
		for i, line := range strings.Split(read(t, path), "\n") {
			for _, pattern := range comparison {
				require.NotRegexp(t, pattern, line,
					"%s:%d: a reader has not considered the alternative, so naming it is padding", path, i+1)
			}
		}
	}
	require.NotZero(t, checked)
}

// The strings a command prints are prose a user meets, so they follow the same
// rule as the documents.
func TestStyle_NoComparisonsInWhatCommandsPrint(t *testing.T) {
	literal := regexp.MustCompile(`"([^"\\]|\\.)*"`)
	for _, path := range ours(t) {
		if !strings.HasPrefix(path, "cmd/") && !strings.HasPrefix(path, "internal/msg/") {
			continue
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		for i, line := range strings.Split(read(t, path), "\n") {
			for _, quoted := range literal.FindAllString(line, -1) {
				for _, pattern := range comparison {
					require.NotRegexp(t, pattern, quoted, "%s:%d", path, i+1)
				}
			}
		}
	}
}

// Words that promise something the sentence then fails to deliver.
var hedges = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bit is recommended to consider\b`),
	regexp.MustCompile(`(?i)\bmay or may not\b`),
	regexp.MustCompile(`(?i)\bplease note that\b`),
	regexp.MustCompile(`(?i)\bin order to\b`),
	regexp.MustCompile(`(?i)\bleverage([sd])? the\b`),
	regexp.MustCompile(`(?i)\bseamless(ly)?\b`),
	regexp.MustCompile(`(?i)\bpowerful and flexible\b`),
	regexp.MustCompile(`(?i)\bbest.in.class\b`),
}

func TestStyle_NoHedgingOrMarketing(t *testing.T) {
	for _, path := range ours(t) {
		for i, line := range strings.Split(read(t, path), "\n") {
			for _, pattern := range hedges {
				require.NotRegexp(t, pattern, line, "%s:%d", path, i+1)
			}
		}
	}
}
