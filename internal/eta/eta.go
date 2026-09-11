// Package eta turns the durations of past runs into "how much longer". It is the
// other half of the answer to a long opaque wait: the phase names say where you
// are, the estimate says how far.
//
// Pure: a Store interface and arithmetic. The file-backed store lives in store.go
// and touches nothing but the filesystem.
package eta

import (
	"fmt"
	"sort"
	"time"
)

// Key partitions history. Two runs are comparable only if they used the same
// provider, class and control-plane placement: a hosted control plane takes a
// different amount of time from a machine-based one, and mixing them would make
// both estimates wrong.
type Key struct {
	Provider  string `json:"provider"`
	Class     string `json:"class"`
	Placement string `json:"placement"`
	Phase     string `json:"phase"`
}

func (k Key) String() string {
	return fmt.Sprintf("%s/%s/%s/%s", k.Provider, k.Class, k.Placement, k.Phase)
}

// Store keeps completed phase durations. The interface exists because the local
// file is v0 and a ConfigMap in the management cluster is the obvious next step
// (PLAN.md, deferred).
type Store interface {
	Append(k Key, d time.Duration) error
	Durations(k Key) ([]time.Duration, error)
}

// Window is how many recent runs an estimate is drawn from. Older runs describe a
// machine that no longer exists.
const Window = 20

// MinSamples is the point below which an estimate would be a guess dressed up as a
// number.
const MinSamples = 3

// Estimate is what the renderer shows. Known is false when there is not enough
// history, and then the renderer says "no history yet" rather than inventing one.
type Estimate struct {
	Known   bool          `json:"known"`
	P50     time.Duration `json:"p50_ns"`
	P95     time.Duration `json:"p95_ns"`
	Samples int           `json:"samples"`
}

// Estimate reads the last Window durations for a key and returns their p50 and p95.
func Get(s Store, k Key) (Estimate, error) {
	durations, err := s.Durations(k)
	if err != nil {
		return Estimate{}, err
	}
	return From(durations), nil
}

// From computes an estimate from durations in recording order; only the last
// Window entries are used.
func From(durations []time.Duration) Estimate {
	if len(durations) > Window {
		durations = durations[len(durations)-Window:]
	}
	if len(durations) < MinSamples {
		return Estimate{Samples: len(durations)}
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return Estimate{
		Known:   true,
		P50:     percentile(sorted, 0.50),
		P95:     percentile(sorted, 0.95),
		Samples: len(sorted),
	}
}

// Remaining is how much longer a running phase is expected to take. It never goes
// below zero and never claims a number for a phase that is over its p95, at that
// point the honest answer is that history does not cover this run.
func (e Estimate) Remaining(elapsed time.Duration) (time.Duration, bool) {
	if !e.Known {
		return 0, false
	}
	if elapsed >= e.P95 {
		return 0, false
	}
	remaining := e.P50 - elapsed
	if remaining < 0 {
		remaining = 0
	}
	return remaining.Round(time.Second), true
}

// percentile uses nearest-rank on an ascending slice: with twenty samples an
// interpolated p95 would imply a precision this data does not have.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(p*float64(len(sorted)) + 0.5)
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}
