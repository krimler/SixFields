package eta_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"capi-distro/internal/eta"
)

func secs(values ...int) []time.Duration {
	out := make([]time.Duration, len(values))
	for i, v := range values {
		out[i] = time.Duration(v) * time.Second
	}
	return out
}

func TestETA_PercentilesUseNearestRank(t *testing.T) {
	for _, tc := range []struct {
		name     string
		samples  []time.Duration
		p50, p95 time.Duration
	}{
		{"three samples", secs(10, 20, 30), 20 * time.Second, 30 * time.Second},
		{"even count", secs(10, 20, 30, 40), 20 * time.Second, 40 * time.Second},
		{"outlier moves p95 only", secs(10, 10, 10, 10, 600), 10 * time.Second, 600 * time.Second},
		{"unsorted input", secs(30, 10, 20), 20 * time.Second, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := eta.From(tc.samples)
			require.True(t, got.Known)
			require.Equal(t, tc.p50, got.P50)
			require.Equal(t, tc.p95, got.P95)
		})
	}
}

// Below three samples an estimate would be a guess with a number on it.
func TestETA_TooLittleHistoryIsNotAnEstimate(t *testing.T) {
	for _, samples := range [][]time.Duration{nil, secs(10), secs(10, 20)} {
		got := eta.From(samples)
		require.False(t, got.Known)
		require.Equal(t, len(samples), got.Samples)
		_, ok := got.Remaining(time.Second)
		require.False(t, ok)
	}
}

// Only the last Window runs count: older ones describe a machine that has changed.
func TestETA_OnlyTheLastWindowCounts(t *testing.T) {
	var samples []time.Duration
	for i := 0; i < eta.Window; i++ {
		samples = append(samples, time.Hour)
	}
	samples = append([]time.Duration{999 * time.Hour}, samples...)
	got := eta.From(samples)
	require.Equal(t, eta.Window, got.Samples)
	require.Equal(t, time.Hour, got.P95)
}

func TestETA_RemainingCountsDownAndThenStopsClaiming(t *testing.T) {
	est := eta.From(secs(100, 100, 100, 200))
	remaining, ok := est.Remaining(30 * time.Second)
	require.True(t, ok)
	require.Equal(t, 70*time.Second, remaining)

	remaining, ok = est.Remaining(150 * time.Second)
	require.True(t, ok)
	require.Equal(t, time.Duration(0), remaining, "past p50 but inside p95: no time left, still an estimate")

	// Past p95 the run is not like the history, so there is no honest number.
	_, ok = est.Remaining(300 * time.Second)
	require.False(t, ok)
}

// History for a hosted control plane must not be mixed with a machine-based one.
func TestETA_KeysPartitionByPlacement(t *testing.T) {
	store := eta.NewMemStore()
	self := eta.Key{Provider: "docker", Class: "std", Placement: "self", Phase: "control plane"}
	hosted := self
	hosted.Placement = "hosted"

	for i := 0; i < 3; i++ {
		require.NoError(t, store.Append(self, 200*time.Second))
		require.NoError(t, store.Append(hosted, 20*time.Second))
	}
	selfEst, err := eta.Get(store, self)
	require.NoError(t, err)
	hostedEst, err := eta.Get(store, hosted)
	require.NoError(t, err)
	require.Equal(t, 200*time.Second, selfEst.P50)
	require.Equal(t, 20*time.Second, hostedEst.P50)
}

func TestETA_FileStoreRoundTripsAndCaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	store := eta.NewFileStore(path)
	key := eta.Key{Provider: "docker", Class: "std", Placement: "self", Phase: "workers"}

	for i := 0; i < eta.Window+5; i++ {
		require.NoError(t, store.Append(key, time.Duration(i)*time.Second))
	}
	reopened := eta.NewFileStore(path)
	durations, err := reopened.Durations(key)
	require.NoError(t, err)
	require.Len(t, durations, eta.Window, "the file keeps only the window")
	require.Equal(t, time.Duration(eta.Window+4)*time.Second, durations[len(durations)-1])
}

// A corrupt history file is a convenience lost, not a cluster that will not come up.
func TestETA_CorruptHistoryIsNotFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o644))
	store := eta.NewFileStore(path)
	durations, err := store.Durations(eta.Key{Phase: "workers"})
	require.NoError(t, err)
	require.Empty(t, durations)
}
