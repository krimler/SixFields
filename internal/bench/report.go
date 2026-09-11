//go:build bench

package bench

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

type backendRun struct {
	name    string
	results []result
}

// summary is one backend's line in the report.
type summary struct {
	backend    string
	tasks      int
	passed     int
	framework  int
	reasoning  int
	answered   int
	ungrounded int
	median     time.Duration
	p95        time.Duration
}

func summarize(r backendRun) summary {
	s := summary{backend: r.name, tasks: len(r.results)}
	var times []time.Duration
	for _, res := range r.results {
		switch res.outcome {
		case pass:
			s.passed++
		case frameworkError:
			s.framework++
		case reasoningError:
			s.reasoning++
		}
		// Only answers are timed. A backend that fails fast would otherwise post the
		// best latency in the report.
		if res.answered {
			s.answered++
			times = append(times, res.took)
			if !res.grounded {
				s.ungrounded++
			}
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	s.median, s.p95 = percentile(times, 0.5), percentile(times, 0.95)
	return s
}

// percentile by nearest rank. With seven tasks p95 is the slowest one, which is
// the honest reading of a sample this size.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}

func render(tasks []task, runs []backendRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\ncluster-bench: %d diagnosis tasks over %s, one backend at a time\n\n", len(tasks), fixtures)

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	row(tw, "TASK\tBLOCKING OBJECT\tSTALL CLASS\tFIXTURE\n")
	for _, t := range tasks {
		row(tw, "%s\t%s\t%s\t%s\n", t.name, t.object, t.code, source(t))
	}
	flush(tw)

	b.WriteString("\n")
	tw = tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	row(tw, "BACKEND\tTASKS\tPASS\tRATE\tMEDIAN\tP95\tFRAMEWORK\tREASONING\tUNGROUNDED\n")
	for _, r := range runs {
		s := summarize(r)
		row(tw, "%s\t%d\t%d\t%s\t%s\t%s\t%d\t%d\t%d\n",
			s.backend, s.tasks, s.passed, rate(s.passed, s.tasks),
			dur(s.median, s.answered), dur(s.p95, s.answered),
			s.framework, s.reasoning, s.ungrounded)
	}
	flush(tw)

	b.WriteString("\nFAILURES\n")
	tw = tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	failures := 0
	for _, r := range runs {
		for _, res := range r.results {
			if res.outcome == pass {
				continue
			}
			failures++
			row(tw, "%s\t%s\t%s\t%s\n", r.name, res.task, res.outcome, res.detail)
		}
	}
	flush(tw)
	if failures == 0 {
		b.WriteString("none\n")
	}
	return b.String()
}

func source(t task) string {
	if t.recorded {
		return "recorded"
	}
	return "synthetic"
}

func rate(passed, total int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%d%%", int(math.Round(100*float64(passed)/float64(total))))
}

func dur(d time.Duration, answered int) string {
	if answered == 0 {
		return "-"
	}
	switch {
	case d < time.Microsecond:
		return d.String()
	case d < time.Millisecond:
		return d.Round(time.Microsecond).String()
	case d < time.Second:
		return d.Round(100 * time.Microsecond).String()
	default:
		return d.Round(10 * time.Millisecond).String()
	}
}

// row and flush drop their errors: every table here is written into a
// strings.Builder, which never fails to write.
func row(tw *tabwriter.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(tw, format, args...)
}

func flush(tw *tabwriter.Writer) { _ = tw.Flush() }
