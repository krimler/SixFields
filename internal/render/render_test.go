package render_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"sixfields/internal/eta"
	"sixfields/internal/fixture"
	"sixfields/internal/fold"
	"sixfields/internal/golden"
	"sixfields/internal/render"
	"sixfields/internal/snapshot"
	"sixfields/internal/why"
)

const fixturesDir = "../../testdata/fixtures"
const goldenDir = "../../testdata/golden/render"

func load(t *testing.T, scenario string) []snapshot.Envelope {
	t.Helper()
	envelopes, err := fixture.Load(filepath.Join(fixturesDir, scenario))
	require.NoError(t, err)
	require.NotEmpty(t, envelopes)
	return envelopes
}

func viewAt(env snapshot.Envelope, estimates map[fold.PhaseName]eta.Estimate) render.View {
	now := fixture.NowFor(env)
	res := fold.Fold(env, fold.Options{Now: now})
	v := render.View{Result: res, Estimates: estimates, Elapsed: res.Elapsed}
	if stall, ok := why.Rank(env, res, why.Options{Now: now}); ok && res.Phases != nil {
		if _, stalled := res.Stalled(); stalled {
			v.Stall = &stall
		}
	}
	return v
}

func sampleEstimates() map[fold.PhaseName]eta.Estimate {
	return map[fold.PhaseName]eta.Estimate{
		fold.Infrastructure: eta.From([]time.Duration{80 * time.Second, 90 * time.Second, 100 * time.Second}),
		fold.ControlPlane:   eta.From([]time.Duration{70 * time.Second, 75 * time.Second, 120 * time.Second}),
		fold.Workers:        eta.From([]time.Duration{60 * time.Second, 85 * time.Second, 95 * time.Second}),
	}
}

// One transcript golden per scenario, in the plain renderer, which is what CI
// reads and what the e2e assertions grep.
func TestUX_PlainTranscriptGoldens(t *testing.T) {
	for _, scenario := range []string{"std-docker-happy", "inmem-happy", "inmem-stall-vm", "stall-bad-version", "hosted-docker-happy", "scale-up"} {
		t.Run(scenario, func(t *testing.T) {
			var buf bytes.Buffer
			plain := &render.Plain{}
			for _, env := range load(t, scenario) {
				out, changed := plain.Render(viewAt(env, sampleEstimates()), 80)
				if changed {
					buf.WriteString(out)
				}
			}
			golden.Text(t, filepath.Join(goldenDir, "plain", scenario+".txt"), buf.String())
		})
	}
}

// The TTY frame at both widths. A frame is a pure function of the view and the
// width, so these goldens pin the layout without a terminal.
func TestUX_TTYFrameGoldensAt80And120(t *testing.T) {
	for _, scenario := range []string{"std-docker-happy", "inmem-stall-vm"} {
		envelopes := load(t, scenario)
		last := envelopes[len(envelopes)-1]
		for _, width := range []int{80, 120} {
			t.Run(scenario+"-"+strconv.Itoa(width), func(t *testing.T) {
				tty := &render.TTY{}
				frame := tty.Frame(viewAt(last, sampleEstimates()), width)
				for _, line := range strings.Split(strings.TrimRight(frame, "\n"), "\n") {
					if strings.HasPrefix(line, "raw: ") {
						// The escape hatch must stay copy-pasteable, so it is the
						// one line allowed to run past the terminal width.
						continue
					}
					require.LessOrEqual(t, len([]rune(render.StripANSI(line))), width, "line over width: %q", line)
				}
				golden.Text(t, filepath.Join(goldenDir, "tty", scenario+"-"+strconv.Itoa(width)+".txt"), frame)
			})
		}
	}
}

// No meaning by colour alone: strip the ANSI from the coloured frame and it must
// be identical to the frame rendered with colour off.
func TestUX_ColorCarriesNoMeaning(t *testing.T) {
	envelopes := load(t, "inmem-stall-vm")
	v := viewAt(envelopes[len(envelopes)-1], sampleEstimates())

	colored := (&render.TTY{Theme: render.Theme{Color: true}}).Frame(v, 100)
	plain := (&render.TTY{Theme: render.Theme{Color: false}}).Frame(v, 100)
	require.NotEqual(t, colored, plain, "the colour theme must actually emit colour")
	require.Equal(t, plain, render.StripANSI(colored))
}

// The stall block: the line, then exactly one raw: line immediately after it, and
// an action to take. This is the contract in D2.2 and D2.4.
func TestUX_StallBlockShape(t *testing.T) {
	for _, scenario := range []string{"inmem-stall-vm", "stall-bad-version", "hosted-stall-pod", "stall-cp-killed"} {
		t.Run(scenario, func(t *testing.T) {
			envelopes := load(t, scenario)
			v := viewAt(envelopes[len(envelopes)-1], sampleEstimates())
			require.NotNil(t, v.Stall, "scenario must stall")

			frame := (&render.TTY{}).Frame(v, 100)
			lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")

			stallIndex := -1
			rawCount := 0
			for i, line := range lines {
				if strings.Contains(line, v.Stall.Object.String()) && strings.Contains(line, "(") {
					stallIndex = i
				}
				if strings.HasPrefix(line, "raw: ") {
					rawCount++
				}
			}
			require.NotEqual(t, -1, stallIndex, "no stall line in:\n%s", frame)
			require.Equal(t, 1, rawCount, "exactly one raw: line")
			require.True(t, strings.HasPrefix(lines[stallIndex+1], "raw: kubectl get "),
				"the stall line is followed by the raw line, got %q", lines[stallIndex+1])
			require.LessOrEqual(t, len(lines[stallIndex]), 120)

			last := lines[len(lines)-1]
			require.True(t, strings.HasPrefix(last, "next: "), "the block ends with an action, got %q", last)
		})
	}
}

// An estimate next to a stall would be a claim about progress that is not being
// made. A stalled phase shows what the phase usually takes instead.
func TestUX_NoETAOnAStalledPhase(t *testing.T) {
	envelopes := load(t, "inmem-stall-vm")
	v := viewAt(envelopes[len(envelopes)-1], sampleEstimates())
	frame := (&render.TTY{}).Frame(v, 100)
	require.NotContains(t, frame, "left")
	require.Contains(t, frame, "typical: p50 ")
}

// With too little history the tool says so rather than inventing a number.
func TestUX_NoHistorySaysSo(t *testing.T) {
	envelopes := load(t, "std-docker-happy")
	v := viewAt(envelopes[1], nil)
	frame := (&render.TTY{}).Frame(v, 100)
	require.Contains(t, frame, "no history yet")
}

// The stream says something within a second of starting, measured on a replay
// with a fake clock (D2.1).
func TestUX_FirstFeedbackUnder1s(t *testing.T) {
	envelopes := load(t, "std-docker-happy")
	clock := fixture.NowFor(envelopes[0])
	var buf bytes.Buffer
	stream := &render.Stream{
		Out: &buf, Renderer: &render.Plain{}, Now: func() time.Time { return clock },
		Width: func() int { return 80 },
	}
	for _, env := range envelopes {
		clock = fixture.NowFor(env)
		require.NoError(t, stream.Update(viewAt(env, sampleEstimates())))
	}
	require.Less(t, stream.TimeToFirstOutput(), time.Second)
	require.NotEmpty(t, buf.String())
}

// While a cluster is still coming up the tool never goes quiet for long: a silent
// command looks like a hung one, which is the problem this project exists to fix.
func TestUX_NoSilentGaps(t *testing.T) {
	envelopes := load(t, "std-docker-happy")
	start := fixture.NowFor(envelopes[0])
	clock := start

	var buf bytes.Buffer
	stream := &render.Stream{
		Out: &buf, Renderer: &render.Plain{}, Now: func() time.Time { return clock },
		Width: func() int { return 80 },
	}

	// Replay the recorded timeline a second at a time, so a phase that stays the
	// same for minutes is exercised rather than skipped over.
	last := envelopes[len(envelopes)-1]
	for offset := 0; offset <= last.Meta.TPlusS; offset++ {
		env := envelopes[0]
		for _, candidate := range envelopes {
			if candidate.Meta.TPlusS <= offset {
				env = candidate
			}
		}
		clock = start.Add(time.Duration(offset) * time.Second)
		view := viewAt(env, sampleEstimates())
		view.Elapsed = clock.Sub(start)
		require.NoError(t, stream.Update(view))
	}

	require.LessOrEqual(t, stream.LongestSilence(), render.DefaultHeartbeat,
		"the stream went %s without saying anything", stream.LongestSilence())
}

// The render budget: replaying a timeline at fifty times speed must not produce
// more than two frames a second, and no frame may repeat the one before it (D2.7).
func TestUX_RenderBudgetAndNoFlicker(t *testing.T) {
	envelopes := load(t, "inmem-happy")
	clock := fixture.NowFor(envelopes[0])
	var buf bytes.Buffer
	tty := &render.TTY{}
	stream := &render.Stream{
		Out: &buf, Renderer: tty, Now: func() time.Time { return clock },
		Width: func() int { return 80 },
	}

	// Fifty updates per simulated second, the speed a --replay --speed 50 reaches.
	frames := []string{}
	for _, env := range envelopes {
		base := fixture.NowFor(env)
		for i := 0; i < 50; i++ {
			clock = base.Add(time.Duration(i) * 20 * time.Millisecond)
			require.NoError(t, stream.Update(viewAt(env, sampleEstimates())))
			if out := stream.Last(); out != "" && (len(frames) == 0 || frames[len(frames)-1] != out) {
				frames = append(frames, out)
			}
		}
	}
	span := clock.Sub(fixture.NowFor(envelopes[0])).Seconds()
	require.LessOrEqual(t, float64(stream.Renders()), 2*span+1,
		"%d renders over %.0fs simulated is more than 2/s", stream.Renders(), span)
	for i := 1; i < len(frames); i++ {
		require.NotEqual(t, frames[i-1], frames[i], "two consecutive identical frames")
	}
}

// --json is a stability contract. The document must validate against the checked-in
// schema, and the schema version must match the one the code emits (D2.9).
func TestUX_JSONMatchesSchema(t *testing.T) {
	schema := readSchema(t)
	require.Equal(t, render.SchemaVersion, schema.Properties.Version.Const)

	for _, scenario := range []string{"std-docker-happy", "inmem-stall-vm"} {
		t.Run(scenario, func(t *testing.T) {
			envelopes := load(t, scenario)
			out, ok := (&render.JSON{}).Render(viewAt(envelopes[len(envelopes)-1], sampleEstimates()), 80)
			require.True(t, ok)

			var doc map[string]any
			require.NoError(t, json.Unmarshal([]byte(out), &doc))
			for _, field := range schema.Required {
				require.Contains(t, doc, field, "the schema requires %q", field)
			}
			for field := range doc {
				require.Contains(t, schema.Properties.Names, field,
					"%q is emitted but not described in docs/schema/status.v1.json", field)
			}
		})
	}
}

// --json --verbose carries the objects the fold was computed from, so the escape
// hatch is one command away rather than a different tool.
func TestUX_VerboseJSONCarriesTheEnvelope(t *testing.T) {
	envelopes := load(t, "std-docker-happy")
	env := envelopes[len(envelopes)-1]
	v := viewAt(env, sampleEstimates())
	v.Verbose, v.Envelope = true, &env

	out, ok := (&render.JSON{}).Render(v, 80)
	require.True(t, ok)
	var doc render.Status
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	require.NotNil(t, doc.Envelope)
	require.Equal(t, len(env.Objects), len(doc.Envelope.Objects))
}

type schemaDoc struct {
	Required   []string
	Properties struct {
		Names   map[string]any
		Version struct{ Const string }
	}
}

func readSchema(t *testing.T) schemaDoc {
	t.Helper()
	b, err := os.ReadFile("../../docs/schema/status.v1.json")
	require.NoError(t, err)
	var raw struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(b, &raw))
	out := schemaDoc{Required: raw.Required}
	out.Properties.Names = raw.Properties
	if version, ok := raw.Properties["version"].(map[string]any); ok {
		out.Properties.Version.Const, _ = version["const"].(string)
	}
	return out
}
