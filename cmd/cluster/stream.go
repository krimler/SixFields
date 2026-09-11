package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mattn/go-isatty"
	"golang.org/x/term"

	"sixfields/internal/eta"
	"sixfields/internal/fold"
	"sixfields/internal/msg"
	"sixfields/internal/render"
	"sixfields/internal/snapshot"
	"sixfields/internal/why"
)

// source produces snapshots, from a live cluster or from a recorded fixture. The
// two paths share everything after this interface, so `--replay` exercises exactly
// the code a live run uses.
type source interface {
	Snapshots(ctx context.Context) (<-chan snapshot.Envelope, error)
	Close() error
}

type streamOptions struct {
	out        io.Writer
	globals    *globals
	stallAfter time.Duration
	timeout    time.Duration
	// blocking makes the command wait for Ready; `status` returns after the first
	// snapshot, `up` waits.
	blocking bool
	history  eta.Store
	provider string
	now      func() time.Time
}

// run reads snapshots until the cluster is ready, the context ends, or the
// timeout is hit, and returns the documented exit code as an error.
func run(ctx context.Context, src source, opt streamOptions) error {
	defer func() { _ = src.Close() }()

	snapshots, err := src.Snapshots(ctx)
	if err != nil {
		return err
	}

	renderer := chooseRenderer(opt.globals, opt.out)
	stream := &render.Stream{
		Out:      opt.out,
		Renderer: renderer,
		Now:      opt.now,
		Width:    terminalWidth,
	}

	var deadline <-chan time.Time
	if opt.timeout > 0 {
		timer := time.NewTimer(opt.timeout)
		defer timer.Stop()
		deadline = timer.C
	}

	var last *render.View
	for {
		select {
		case <-ctx.Done():
			return finish(last, opt)
		case <-deadline:
			return finish(last, opt)
		case env, ok := <-snapshots:
			if !ok {
				return finish(last, opt)
			}
			view := build(env, opt)
			last = &view
			if err := stream.Update(view); err != nil {
				return err
			}
			if view.Result.Ready {
				recordHistory(view, opt)
				return nil
			}
			if !opt.blocking {
				return nil
			}
		}
	}
}

// build folds one envelope into everything a renderer needs. It is the only place
// fold, why and eta are wired together.
func build(env snapshot.Envelope, opt streamOptions) render.View {
	now := opt.now()
	res := fold.Fold(env, fold.Options{Now: now, StallAfter: opt.stallAfter})
	view := render.View{
		Result:    res,
		Verbose:   opt.globals.verbose,
		Elapsed:   res.Elapsed,
		Estimates: estimates(res, opt),
		Envelope:  &env,
	}
	if _, stalled := res.Stalled(); stalled {
		if stall, ok := why.Rank(env, res, why.Options{Now: now}); ok {
			view.Stall = &stall
		}
	}
	return view
}

func estimates(res fold.Result, opt streamOptions) map[fold.PhaseName]eta.Estimate {
	if opt.history == nil {
		return nil
	}
	out := make(map[fold.PhaseName]eta.Estimate, len(res.Phases))
	for _, p := range res.Phases {
		est, err := eta.Get(opt.history, key(res, opt.provider, p.Name))
		if err != nil {
			continue
		}
		out[p.Name] = est
	}
	return out
}

func key(res fold.Result, provider string, phase fold.PhaseName) eta.Key {
	placement := res.Placement
	if placement == "" {
		placement = "self"
	}
	return eta.Key{Provider: provider, Class: res.Class, Placement: placement, Phase: string(phase)}
}

// recordHistory stores the durations of a completed run, so tomorrow's estimate is
// calibrated by today's work without anyone opting in.
func recordHistory(v render.View, opt streamOptions) {
	if opt.history == nil {
		return
	}
	for _, p := range v.Result.Phases {
		if p.State != fold.Done || p.Elapsed <= 0 {
			continue
		}
		// An error here costs an estimate, not a cluster.
		_ = opt.history.Append(key(v.Result, opt.provider, p.Name), p.Elapsed)
	}
}

// finish maps the final view to the exit-code contract: 0 ready, 2 still running
// or stalled when the wait ended.
func finish(v *render.View, opt streamOptions) error {
	if v == nil {
		return msg.New(msg.ClusterNotFound, msg.Vars{Object: "", Namespace: opt.globals.namespace})
	}
	if v.Result.Ready {
		recordHistory(*v, opt)
		return nil
	}
	// The renderer has already printed the stall block, so this only carries the
	// exit code.
	if v.Stall != nil {
		return quiet{code: msg.ExitStalled}
	}
	if !opt.blocking {
		return nil
	}
	return &msg.Error{
		Code:    msg.ClusterNotFound,
		Summary: fmt.Sprintf("%s is still provisioning after %s.", v.Result.Cluster, render.Short(v.Result.Elapsed)),
		Action:  "cluster status " + v.Result.Cluster.Name,
	}
}

func chooseRenderer(g *globals, out io.Writer) render.Renderer {
	theme := render.Theme{Color: useColor(g, out)}
	switch {
	case g.jsonOut:
		return &render.JSON{}
	case g.noTTY || !isTerminal(out):
		return &render.Plain{Theme: theme}
	default:
		return &render.TTY{Theme: theme}
	}
}

func useColor(g *globals, out io.Writer) bool {
	if g.noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminal(out)
}

func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}

func terminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return render.DefaultWidth
	}
	return width
}

func parseDuration(value string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", value, err)
	}
	return d, nil
}
