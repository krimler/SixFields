package render

import (
	"fmt"
	"strings"

	"capi-distro/internal/fold"
)

// TTY draws four rows and redraws them in place. It is what a person watching a
// cluster come up sees.
type TTY struct {
	Theme Theme
	last  string
}

// nameColumn is wide enough for the longest phase name plus a space.
const nameColumn = 15

// barWidth is fixed so the four rows line up whatever the terminal width; the
// detail column takes the slack.
const barWidth = 12

func (t *TTY) Render(v View, width int) (string, bool) {
	frame := t.Frame(v, width)
	if frame == t.last {
		return "", false
	}
	t.last = frame
	return frame, true
}

// Frame is the whole screen. It is a pure function of the view and the width,
// which is what makes the goldens at 80 and 120 columns meaningful.
func (t *TTY) Frame(v View, width int) string {
	if width <= 0 {
		width = DefaultWidth
	}
	var b strings.Builder

	header := fmt.Sprintf("%s  class %s · %s · %s",
		v.Result.Cluster.String(), v.Result.Class, v.Result.Version, placementLabel(v.Result.Placement))
	b.WriteString(t.Theme.paint(ansiBold, truncate(header, width)))
	b.WriteString("\n")

	for _, p := range v.Result.Phases {
		b.WriteString(t.row(v, p, width))
		b.WriteString("\n")
	}

	if lines := stallBlock(v, t.Theme); len(lines) > 0 {
		b.WriteString("\n")
		for _, line := range lines {
			// The raw line is the escape hatch. A truncated kubectl command is not
			// copy-pasteable, so it is the one thing that may run past the width.
			if strings.HasPrefix(line, "raw: ") {
				b.WriteString(line)
			} else {
				b.WriteString(truncateVisible(line, width))
			}
			b.WriteString("\n")
		}
	} else if v.Result.Ready {
		b.WriteString("\n")
		b.WriteString(t.Theme.paint(ansiGreen, "ready in "+Short(v.Result.Elapsed)))
		b.WriteString("\n")
	} else {
		b.WriteString("\n")
		b.WriteString(t.Theme.paint(ansiDim, "safe to Ctrl-C; `cluster status "+v.Result.Cluster.Name+"` resumes"))
		b.WriteString("\n")
	}
	return b.String()
}

func (t *TTY) row(v View, p fold.Phase, width int) string {
	name := pad(string(p.Name), nameColumn)
	bar := t.Theme.forState(p.State, progressBar(p.State))
	state := pad(string(p.State), 8)

	right := etaText(v, p)
	if p.State == fold.Done {
		right = Short(p.Elapsed)
		if p.Detail == "none" && len(p.Contributors) == 0 {
			// Nothing ran, so an elapsed time here would be the cluster's age
			// wearing the addons phase's name.
			right = ""
		}
	}

	left := fmt.Sprintf("%s %s %s %s", name, bar, t.Theme.forState(p.State, state), p.Detail)
	if right == "" {
		return truncateVisible(left, width)
	}
	// Right-align the detail column; fall back to one space when the terminal is
	// too narrow to align anything.
	gap := width - visibleLen(left) - len(right)
	if gap < 1 {
		gap = 1
	}
	return truncateVisible(left+strings.Repeat(" ", gap)+t.Theme.paint(ansiDim, right), width)
}

// progressBar is blocks, not a spinner: a frame must be a pure function of state
// so two renders of the same state are identical and the flicker test can hold.
func progressBar(state fold.State) string {
	switch state {
	case fold.Done:
		return strings.Repeat("█", barWidth)
	case fold.Running:
		return strings.Repeat("█", barWidth/2) + strings.Repeat("░", barWidth-barWidth/2)
	case fold.Stalled:
		return strings.Repeat("█", barWidth/2) + strings.Repeat("!", barWidth-barWidth/2)
	default:
		return strings.Repeat("░", barWidth)
	}
}

func placementLabel(placement string) string {
	if placement == "" {
		return "self"
	}
	return placement
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func truncate(s string, width int) string {
	if width <= 1 || len(s) <= width {
		return s
	}
	return s[:width-1] + "…"
}

// truncateVisible measures the printable length so colour codes do not count
// against the terminal width.
func truncateVisible(s string, width int) string {
	if visibleLen(s) <= width {
		return s
	}
	plain := StripANSI(s)
	if len(plain) <= width {
		return s
	}
	return truncate(plain, width)
}

func visibleLen(s string) int { return len([]rune(StripANSI(s))) }
