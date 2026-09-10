package main

import (
	"context"
	"path/filepath"
	"time"

	"capi-distro/internal/fixture"
	"capi-distro/internal/snapshot"
)

// replaySource plays a recorded timeline back through the same code a live run
// uses. UI work happens here: no cluster, no waiting, and the exact timings of a
// real run.
type replaySource struct {
	dir   string
	speed float64
	// clock is advanced to each envelope's recorded instant, so the renderer sees
	// the timings that were recorded rather than the wall clock of the replay.
	clock *time.Time
}

func newReplaySource(dir string, speed float64, clock *time.Time) *replaySource {
	if speed <= 0 {
		speed = 1
	}
	return &replaySource{dir: dir, speed: speed, clock: clock}
}

func (r *replaySource) Snapshots(ctx context.Context) (<-chan snapshot.Envelope, error) {
	envelopes, err := fixture.Load(r.dir)
	if err != nil {
		return nil, err
	}
	out := make(chan snapshot.Envelope)
	go func() {
		defer close(out)
		var previous int
		for _, env := range envelopes {
			gap := time.Duration(env.Meta.TPlusS-previous) * time.Second
			previous = env.Meta.TPlusS
			if wait := time.Duration(float64(gap) / r.speed); wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
			*r.clock = fixture.T0.Add(time.Duration(env.Meta.TPlusS) * time.Second)
			select {
			case <-ctx.Done():
				return
			case out <- env:
			}
		}
	}()
	return out, nil
}

func (r *replaySource) Close() error { return nil }

// replayName is the scenario a replay directory holds, used where a cluster name
// would otherwise come from the command line.
func replayName(dir string) string { return filepath.Base(dir) }
