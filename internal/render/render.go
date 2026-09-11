// Package render turns a folded result into what a person sees. Three renderers
// share one view: a redrawn TTY frame, a machine-greppable plain stream for CI,
// and JSON for anything that wants to build on this.
//
// Pure: it writes strings. The clock and the terminal width are inputs, so a
// replay at 50x renders exactly what a live run would.
package render

import (
	"fmt"
	"strings"
	"time"

	"sixfields/internal/eta"
	"sixfields/internal/fold"
	"sixfields/internal/msg"
	"sixfields/internal/snapshot"
	"sixfields/internal/why"
)

// View is everything a renderer may show. Assembling it is the caller's job, so
// no renderer reaches back into a cluster.
type View struct {
	Result    fold.Result
	Stall     *why.Stall
	Estimates map[fold.PhaseName]eta.Estimate
	Verbose   bool
	// Elapsed is how long the command has been watching, which is what the plain
	// renderer stamps each line with.
	Elapsed time.Duration
	// Envelope is the objects the fold came from. --json --verbose emits it, so a
	// user or an agent can go from the summary to the raw truth in one step.
	Envelope *snapshot.Envelope
}

// Renderer produces output for a view. The second result is false when there is
// nothing new to say, which is what keeps the render budget in D2.7 honest.
type Renderer interface {
	Render(v View, width int) (string, bool)
}

// Heartbeater says something when nothing has changed for a while. A renderer
// that cannot do this will go silent during a long phase, and a silent tool looks
// like a hung one, which is the whole problem this project is about.
type Heartbeater interface {
	Heartbeat(v View, width int) (string, bool)
}

// DefaultWidth is used when the terminal width is unknown, e.g. in a pipe.
const DefaultWidth = 80

// Theme carries the one decision that changes every line: colour or not. Nothing
// in this package encodes meaning in colour alone, TestUX_ColorCarriesNoMeaning
// strips the ANSI and asserts the tokens are identical.
type Theme struct{ Color bool }

const (
	ansiReset  = "\x1b[0m"
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiBold   = "\x1b[1m"
)

func (t Theme) paint(code, s string) string {
	if !t.Color || s == "" {
		return s
	}
	return code + s + ansiReset
}

func (t Theme) forState(state fold.State, s string) string {
	switch state {
	case fold.Done:
		return t.paint(ansiGreen, s)
	case fold.Running:
		return t.paint(ansiYellow, s)
	case fold.Stalled:
		return t.paint(ansiRed, s)
	default:
		return t.paint(ansiDim, s)
	}
}

// StripANSI removes escape sequences, for the colour-independence test and for
// anything that captures output.
func StripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// Short formats a duration the way a person says it out loud.
func Short(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

// etaText is the right-hand column of a running phase. A stalled phase never gets
// one: an estimate next to a stall is a lie about progress. Nor does a phase
// behind a stalled one, it is waiting, and its own history says nothing about
// how long the thing in front of it will take.
func etaText(v View, p fold.Phase) string {
	if p.State != fold.Running || !isFrontier(v.Result, p.Name) {
		return ""
	}
	est, ok := v.Estimates[p.Name]
	if !ok || !est.Known {
		return "no history yet"
	}
	remaining, ok := est.Remaining(p.Elapsed)
	if !ok {
		return "longer than usual"
	}
	return "~" + Short(remaining) + " left"
}

// isFrontier reports whether this is the earliest phase that is not done: the one
// thing the cluster is actually waiting on.
func isFrontier(res fold.Result, name fold.PhaseName) bool {
	for _, p := range res.Phases {
		if p.State != fold.Done {
			return p.Name == name
		}
	}
	return false
}

// typicalText is what a stall shows instead of an estimate: what this phase
// usually costs, so the user can judge how far off this run is.
func typicalText(v View, name fold.PhaseName) string {
	est, ok := v.Estimates[name]
	if !ok || !est.Known {
		return ""
	}
	return fmt.Sprintf("typical: p50 %s · p95 %s", Short(est.P50), Short(est.P95))
}

// stallBlock is the same in every renderer, because the stall line contract is
// about the text, not the medium: the line, then exactly one raw: line, then what
// this phase usually takes, then what to do next.
func stallBlock(v View, theme Theme) []string {
	if v.Stall == nil {
		return nil
	}
	s := *v.Stall
	lines := []string{
		theme.paint(ansiRed, s.Line()),
		"raw: " + s.Raw,
	}
	if typical := typicalText(v, s.Phase); typical != "" {
		lines = append(lines, theme.paint(ansiDim, typical))
	}
	lines = append(lines, "next: "+nextAction(s))
	if v.Verbose {
		lines = append(lines, "", "why "+s.Object.String()+" was chosen:",
			fmt.Sprintf("  %s=False reason=%s", s.ConditionType, s.Reason))
		if s.FullMessage != "" {
			lines = append(lines, "  "+s.FullMessage)
		} else if s.Message != "" {
			lines = append(lines, "  "+s.Message)
		}
		for _, c := range s.Candidates[1:] {
			lines = append(lines, fmt.Sprintf("  ranked below: %s %s=False (%s)", c.Object, c.Condition.Type, c.LostTo))
		}
	}
	return lines
}

// nextAction comes from the message registry, so the action a user is given is
// the same one `cluster explain` and the runbook give. Most stall classes send
// them to the runbook; one sends them to the field to change.
func nextAction(s why.Stall) string {
	if entry, ok := msg.Get(s.Code); ok {
		return entry.NextAction
	}
	return "cluster docs " + string(s.Code)
}

// StallLines exposes the stall block for `cluster why`, which prints that and
// nothing else. One function, so the block is identical in both commands.
func StallLines(v View, theme Theme) []string { return stallBlock(v, theme) }
