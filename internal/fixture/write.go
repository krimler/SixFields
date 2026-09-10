package fixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"capi-distro/internal/snapshot"
)

// Filename is the name an envelope is stored under inside its scenario directory:
// t+NNNs.json, zero-padded so a directory listing is in timeline order.
func Filename(e snapshot.Envelope) string {
	return fmt.Sprintf("t+%04ds.json", e.Meta.TPlusS)
}

// Write writes one timeline into dir/<scenario>/. It removes envelopes that are no
// longer produced, so a regenerated scenario never leaves a stale file behind.
func Write(dir string, tl Timeline) error {
	target := filepath.Join(dir, tl.Name)
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, e := range tl.Envelopes {
		// Provenance travels with the envelope, not with the directory: a fixture
		// file read on its own must still say where it came from.
		if e.Meta.Note == "" {
			e.Meta.Note = tl.Note
		}
		if e.Meta.Scenario == "" {
			e.Meta.Scenario = tl.Name
		}
		name := Filename(e)
		wanted[name] = true
		b, err := json.MarshalIndent(e, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(target, name), append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") && !wanted[entry.Name()] {
			if err := os.Remove(filepath.Join(target, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// Load reads every envelope of a scenario directory, in timeline order.
func Load(dir string) ([]snapshot.Envelope, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []snapshot.Envelope
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var e snapshot.Envelope
		if err := json.Unmarshal(b, &e); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		out = append(out, e)
	}
	return out, nil
}

// Scenarios lists the scenario directories under dir.
func Scenarios(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			out = append(out, entry.Name())
		}
	}
	return out, nil
}

// NowFor is the instant an envelope was taken: the cluster's creation time plus
// _meta.t_plus_s. A synthetic timeline starts at T0 and a recorded one starts
// whenever it was recorded, and this is what lets both fold the same way.
func NowFor(env snapshot.Envelope) time.Time {
	start := T0
	if cluster, ok := env.Cluster(); ok {
		if created := cluster.CreationTimestamp(); !created.IsZero() {
			start = created
		}
	}
	return start.Add(time.Duration(env.Meta.TPlusS) * time.Second)
}
