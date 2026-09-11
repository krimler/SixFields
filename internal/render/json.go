package render

import (
	"encoding/json"
	"time"

	"sixfields/internal/eta"
	"sixfields/internal/fold"
	"sixfields/internal/snapshot"
	"sixfields/internal/why"
)

// SchemaVersion is the contract in docs/schema/status.v1.json. Changing the shape
// means bumping this and the file, and TestUX_JSONMatchesSchema fails otherwise.
const SchemaVersion = "v1"

// Status is `cluster status --json`. It is the envelope every other tool builds
// on, including the AI paths, which is why the schema is versioned.
type Status struct {
	Version   string                  `json:"version"`
	Status    fold.Result             `json:"status"`
	Stall     *why.Stall              `json:"stall,omitempty"`
	Estimates map[string]eta.Estimate `json:"estimates,omitempty"`
	Elapsed   time.Duration           `json:"elapsed_ns"`
	// Envelope is every object the fold was computed from. It is large, so it is
	// included only under --verbose; the escape hatch stays available either way.
	Envelope *snapshot.Envelope `json:"envelope,omitempty"`
}

// JSON renders the machine-readable view. It is a full document each time, not a
// stream of deltas: a consumer should never have to reassemble state.
type JSON struct{ Indent bool }

func (j *JSON) Render(v View, _ int) (string, bool) {
	b, err := json.MarshalIndent(Document(v), "", "  ")
	if err != nil {
		return "", false
	}
	return string(b) + "\n", true
}

func Document(v View) Status {
	doc := Status{Version: SchemaVersion, Status: v.Result, Stall: v.Stall, Elapsed: v.Elapsed}
	if v.Verbose && v.Envelope != nil {
		doc.Envelope = v.Envelope
	}
	if len(v.Estimates) > 0 {
		doc.Estimates = make(map[string]eta.Estimate, len(v.Estimates))
		for name, est := range v.Estimates {
			doc.Estimates[string(name)] = est
		}
	}
	return doc
}
