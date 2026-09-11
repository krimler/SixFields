package fixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sixfields/internal/snapshot"
)

// Filename is the name an envelope is stored under inside its scenario directory:
// t+NNNs.json, zero-padded so a directory listing is in timeline order.
func Filename(e snapshot.Envelope) string {
	return fmt.Sprintf("t+%04ds.json", e.Meta.TPlusS)
}

// IsRecorded reports whether a scenario directory holds envelopes taken from a
// real run. A recording is the source of truth and must not be overwritten by the
// synthetic generator that stood in for it.
func IsRecorded(dir string) bool {
	envelopes, err := Load(dir)
	if err != nil || len(envelopes) == 0 {
		return false
	}
	return !envelopes[0].Meta.Synthetic
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
		e = Redact(e)
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

// redactions are the fields a recorded cluster carries that nobody should publish.
// A fixture is committed to this repository and read by strangers, so the recorder
// replaces these before anything reaches disk. Real bootstrap tokens were found in
// seven fixtures during a release check.
var redactions = []struct {
	path        []string
	placeholder string
}{
	{[]string{"spec", "joinConfiguration", "discovery", "bootstrapToken", "token"}, "redacted.0123456789abcdef"},
	{[]string{"spec", "joinConfiguration", "discovery", "bootstrapToken", "caCertHashes"}, ""},
	{[]string{"spec", "clusterConfiguration", "certificateKey"}, "redacted"},
}

// Redact replaces every credential in an envelope. The shape is untouched, so a
// redacted fixture folds exactly as the run it came from did.
func Redact(env snapshot.Envelope) snapshot.Envelope {
	for _, o := range env.Objects {
		for _, r := range redactions {
			parent, ok := o.Map(r.path[:len(r.path)-1]...)
			if !ok {
				continue
			}
			key := r.path[len(r.path)-1]
			if _, present := parent[key]; !present {
				continue
			}
			if r.placeholder == "" {
				parent[key] = []any{"sha256:" + strings.Repeat("0", 64)}
				continue
			}
			parent[key] = r.placeholder
		}
	}
	return env
}
