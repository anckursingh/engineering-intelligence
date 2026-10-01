// CI metric (§17's "the set that has data"): the pass rate of completed
// workflow runs. Event times only (§19) — the rate anchors at completed_at.
// A run is a "verdict" when its conclusion judges the code (success,
// failure, timed_out); cancelled/skipped runs never tested the code and a
// completed run without a conclusion is unclassifiable — none count.
package metrics

import (
	"sort"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// DefCIPassRate declares the CI pass-rate contract.
var DefCIPassRate = Definition{
	Name:        "ci_pass_rate",
	Formula:     "count(Builds with conclusion success) / count(Builds with conclusion in success|failure|timed_out) * 100",
	Population:  "Builds (workflow runs)",
	Window:      "completed_at in window (event time)",
	Sources:     []string{"github"},
	Filters:     []string{"completed_at set", "conclusion is a verdict (success/failure/timed_out)"},
	Aggregation: "one observation per window",
	Limitations: []string{
		"cancelled/skipped runs excluded — no verdict on the code",
		"the list endpoint carries no completed_at; a completed run's updated_at stands for it",
	},
}

// verdict reports whether a conclusion judges the code. Cancelled and
// skipped runs never ran the checks; an empty conclusion (even on a
// completed run) is unclassifiable.
func verdict(b ontology.Build) bool {
	switch b.Conclusion {
	case "success", "failure", "timed_out":
		return true
	}
	return false
}

// CIPassRate computes the share of verdict builds whose conclusion is
// success, for builds completed in the window: one observation per window
// with every counted build in evidence.
func CIPassRate(pop Population, w Window) []Observation {
	if windowEmpty(w) {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	var passed int
	for _, e := range pop.Builds {
		if seen[e.ExternalID] || e.Value.CompletedAt.IsZero() || e.Value.CompletedAt.Before(w.Start) || !e.Value.CompletedAt.Before(w.End) {
			continue
		}
		if !verdict(e.Value) {
			continue
		}
		seen[e.ExternalID] = true
		ids = append(ids, e.ExternalID)
		if e.Value.Conclusion == "success" {
			passed++
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return []Observation{{
		Metric:   DefCIPassRate.Name,
		Value:    float64(passed) / float64(len(ids)) * 100,
		Window:   w,
		Evidence: []evidence.Evidence{{Type: "Build", ObjectIDs: ids, State: evidence.StateCalculated}},
	}}
}
