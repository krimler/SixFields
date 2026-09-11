package main

import (
	"context"
	"time"

	"sixfields/internal/fixture"
	"sixfields/internal/snapshot"
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

// A speed of zero means "as fast as possible": the one-shot commands want the
// last envelope, not the timeline.
func newReplaySource(dir string, speed float64, clock *time.Time) *replaySource {
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
			wait := time.Duration(0)
			if r.speed > 0 {
				wait = time.Duration(float64(gap) / r.speed)
			}
			if wait > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
			// The epoch is the cluster's own creation time, not a constant: a
			// synthetic timeline starts at fixture.T0 and a recorded one starts
			// whenever it was recorded. Using the constant for both made every
			// recorded fixture replay with a negative elapsed time.
			*r.clock = fixture.NowFor(env)
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
