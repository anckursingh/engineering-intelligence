// Package evidence defines the generic evidence representation (§20) and the
// epistemic states (§21) every insight in EI carries. An explanation must
// reconstruct from the objects ObjectIDs cites plus deterministic
// calculations — generated text is never the source of truth.
package evidence

import "time"

// State is the epistemic status of a piece of evidence (§21). The four states
// never collapse: each says how directly the evidence rests on data.
type State int

const (
	StateObserved     State = iota // read directly from a source
	StateCalculated                // deterministic math over observed objects
	StateInferred                  // pattern or statistical inference
	StateHypothesized              // candidate, not yet supported
	StateUnknown                   // state never set — weakest
)

// Strength orders states by how directly they rest on data: observed is
// strongest, unknown weakest (§21).
func (s State) Strength() int {
	switch s {
	case StateObserved:
		return 0
	case StateCalculated:
		return 1
	case StateInferred:
		return 2
	case StateHypothesized:
		return 3
	default:
		return 4
	}
}

func (s State) String() string {
	switch s {
	case StateObserved:
		return "OBSERVED"
	case StateCalculated:
		return "CALCULATED"
	case StateInferred:
		return "INFERRED"
	case StateHypothesized:
		return "HYPOTHESIZED"
	default:
		return "UNKNOWN"
	}
}

// Evidence is the generic representation (§20): what kind of thing, where it
// came from, which persisted objects back it, when it was seen, and how much
// it is believed. ObjectIDs are the reconstruction handle — they must resolve
// through the store.
type Evidence struct {
	Type       string    `json:"type"`
	Source     string    `json:"source,omitempty"`
	SourceURL  string    `json:"source_url,omitempty"`
	ObjectIDs  []string  `json:"object_ids,omitempty"`
	ObservedAt time.Time `json:"observed_at,omitempty"`
	Confidence float64   `json:"confidence,omitempty"`
	State      State     `json:"state"`
}
