package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"capi-distro/internal/golden"
	"capi-distro/internal/msg"
)

// runCLI executes the command tree in-process and returns stdout, stderr and the
// exit code the binary would have used.
func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)

	err := root.Execute()
	code := 0
	if err != nil {
		if q, ok := err.(quiet); ok {
			code = q.code
		} else {
			code = msg.ExitCodeOf(err)
		}
	}
	return out.String(), errOut.String(), code
}

// Every command's help has an Examples block. A command a reader cannot copy from
// is a command they will not use (D2.10).
func TestUX_EveryCommandHasExamples(t *testing.T) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Name() != "completion" && cmd.Name() != "help" && cmd.Runnable() {
			require.NotEmpty(t, cmd.Example, "%q has no Examples: block", cmd.CommandPath())
			runnable := false
			for _, line := range strings.Split(cmd.Example, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "cluster ") {
					runnable = true
				}
			}
			require.True(t, runnable, "%q has an Examples block with no cluster command in it", cmd.CommandPath())
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(newRootCmd())
}

// The exit-code contract, one case per code (D2.11).
func TestUX_ExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"ready", []string{"why", "--replay", "../../testdata/fixtures/std-docker-happy"}, msg.ExitReady},
		{"stalled", []string{"why", "--replay", "../../testdata/fixtures/inmem-stall-etcd", "--stall-after", "1m"}, msg.ExitStalled},
		{"rejected", []string{"plan", "-f", "testdata/denied.yaml"}, msg.ExitRejected},
		{"environment", []string{"explain", "CAPI-NOPE-001"}, msg.ExitEnv},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, code := runCLI(t, tc.args...)
			require.Equal(t, tc.want, code)
		})
	}
}

// The examples in examples/ are applied by people, so they must pass the same
// checks admission applies and must not drift from the pinned version.
func TestExamples_PassPlanAndMatchPinnedVersions(t *testing.T) {
	want := pinned(t, "WORKLOAD_K8S_VERSION")
	require.NotEmpty(t, want)

	matches, err := filepath.Glob("../../examples/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			b, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Contains(t, string(b), "version: "+want,
				"the example pins a different version from versions.env")

			out, errOut, code := runCLI(t, "plan", "-f", path)
			require.Equal(t, msg.ExitReady, code, "stderr:\n%s", errOut)
			require.Contains(t, out, "no field is rejected")
		})
	}
}

// A denied file produces the same text `cluster plan` and admission produce, and
// every line of it ends with something to do.
func TestUX_PlanRejectionsHaveNextActions(t *testing.T) {
	_, errOut, code := runCLI(t, "plan", "-f", "testdata/denied.yaml")
	require.Equal(t, msg.ExitRejected, code)
	lines := strings.Split(strings.TrimRight(errOut, "\n"), "\n")
	require.NotEmpty(t, lines)
	for i := 0; i < len(lines); i += 2 {
		require.Contains(t, strings.ToLower(lines[i]), "break-glass")
		require.True(t, strings.HasPrefix(lines[i+1], "  next: "), "no next action after %q", lines[i])
	}
}

// The transcript a person sees when they replay a stall, pinned.
func TestUX_ReplayTranscriptGoldens(t *testing.T) {
	for _, scenario := range []string{"inmem-stall-etcd", "std-docker-happy"} {
		t.Run(scenario, func(t *testing.T) {
			out, _, _ := runCLI(t, "status", "--replay", "../../testdata/fixtures/"+scenario,
				"--speed", "0", "--no-tty", "--stall-after", "1m")
			golden.Text(t, filepath.Join("../../testdata/golden/cli", scenario+".txt"), out)
		})
	}
}

// CAPD writes a kubeconfig pointing at the load balancer's address inside the
// container network, which the host cannot reach on Docker Desktop. This is the
// paper cut the project exists to remove, so it has a test rather than a footnote.
func TestKubeconfig_RewritesForDockerDesktop(t *testing.T) {
	const in = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: QUJD
    server: https://dev-1-lb:6443
  name: dev-1
contexts:
- context:
    cluster: dev-1
    user: dev-1-admin
  name: dev-1-admin@dev-1
current-context: dev-1-admin@dev-1
`
	got := RewriteServer(in, "56789")
	require.Contains(t, got, "server: https://127.0.0.1:56789")
	require.NotContains(t, got, "dev-1-lb:6443")
	require.Contains(t, got, "certificate-authority-data: QUJD", "everything else is untouched")
	require.Contains(t, got, "current-context: dev-1-admin@dev-1")
}

// The documented first run is at most four commands (D2.10). The README is the
// contract, so the count comes from the README rather than from this file.
func TestUX_FirstRunCommandCount(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	require.NoError(t, err)

	inBlock := false
	var commands []string
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "```"):
			// The first fenced block after the "First run" heading is the path.
			if inBlock {
				require.LessOrEqual(t, len(commands), 4,
					"the documented first run is %d commands: %v", len(commands), commands)
				return
			}
		case inBlock && line != "" && !strings.HasPrefix(line, "#"):
			commands = append(commands, line)
		}
		if strings.HasPrefix(line, "## First run") {
			inBlock = false
		}
		if strings.Contains(line, "```sh") || strings.Contains(line, "```console") {
			inBlock = true
		}
	}
	require.NotEmpty(t, commands, "README.md has no first-run block")
}

// pinned reads one value out of versions.env, so a test never hardcodes a version.
func pinned(t *testing.T, key string) string {
	t.Helper()
	b, err := os.ReadFile("../../versions.env")
	require.NoError(t, err)
	for _, line := range strings.Split(string(b), "\n") {
		if name, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok && name == key {
			return value
		}
	}
	return ""
}
