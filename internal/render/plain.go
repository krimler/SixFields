package render

import (
	"fmt"
	"strings"

	"sixfields/internal/fold"
)

// Plain writes one line per state change: greppable, no cursor movement, safe in
// a log. This is what CI reads and what the e2e goldens are written against.
type Plain struct {
	Theme Theme
	seen  map[fold.PhaseName]string
	done  bool
}

// Heartbeat restates the phase the cluster is waiting on, with the time it has
// been waiting. It is what stops a long phase looking like a hung command.
func (p *Plain) Heartbeat(v View, _ int) (string, bool) {
	if v.Result.Ready {
		return "", false
	}
	for _, phase := range v.Result.Phases {
		if phase.State == fold.Done {
			continue
		}
		return fmt.Sprintf("%-7s %-15s %-8s %s\n",
			"t+"+Short(v.Elapsed), phase.Name, "waiting", phase.Detail), true
	}
	return "", false
}

func (p *Plain) Render(v View, _ int) (string, bool) {
	if p.seen == nil {
		p.seen = map[fold.PhaseName]string{}
	}
	var lines []string
	for _, phase := range v.Result.Phases {
		key := string(phase.State) + " " + phase.Detail
		if p.seen[phase.Name] == key {
			continue
		}
		p.seen[phase.Name] = key
		lines = append(lines, fmt.Sprintf("%-7s %-15s %-8s %s",
			"t+"+Short(v.Elapsed), phase.Name, phase.State, phase.Detail))
	}

	if v.Stall != nil {
		stallKey := "stall " + v.Stall.Object.String() + " " + string(v.Stall.Code)
		if p.seen["__stall"] != stallKey {
			p.seen["__stall"] = stallKey
			lines = append(lines, stallBlock(v, p.Theme)...)
		}
	} else {
		delete(p.seen, "__stall")
	}

	if v.Result.Ready && !p.done {
		p.done = true
		lines = append(lines, "ready in "+Short(v.Result.Elapsed))
	}

	if len(lines) == 0 {
		return "", false
	}
	return strings.Join(lines, "\n") + "\n", true
}
