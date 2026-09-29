// PR size (roadmap Layer A): the diff magnitude of merged PRs — additions +
// deletions in lines, anchored at merged_at (§19 event time). One observation
// per window with every counted PR in evidence; PRs without diff data are
// excluded (silence, never zero).
package metrics

import (
	"sort"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
)

// DefPRSize declares the metric per §16.
var DefPRSize = Definition{
	Name:        "pr_size",
	Formula:     "additions + deletions, in lines",
	Population:  "merged PullRequests",
	Window:      "merged_at in window (event time)",
	Sources:     []string{"github"},
	Filters:     []string{"merged", "diff size present (additions or deletions set)"},
	Aggregation: "mean over PRs — one observation per window",
	Limitations: []string{
		"excludes PRs synced before diff-size ingestion — no data, not zero",
	},
}

// PRSize computes the mean diff size of merged PRs whose merged_at falls in
// the window. A PR with both fields zero carries no diff data and never
// counts; duplicates collapse by external ID.
func PRSize(pop Population, w Window) []Observation {
	if windowEmpty(w) {
		return nil
	}
	seen := map[string]bool{}
	var total float64
	var ids []string
	for _, e := range pop.PullRequests {
		if seen[e.ExternalID] || !valid(e.Value) || e.Value.MergedAt.Before(w.Start) || !e.Value.MergedAt.Before(w.End) {
			continue
		}
		if e.Value.Additions == 0 && e.Value.Deletions == 0 {
			continue
		}
		seen[e.ExternalID] = true
		total += float64(e.Value.Additions + e.Value.Deletions)
		ids = append(ids, e.ExternalID)
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return []Observation{{
		Metric: DefPRSize.Name,
		Value:  total / float64(len(ids)),
		Window: w,
		Evidence: []evidence.Evidence{
			{Type: "PullRequest", ObjectIDs: ids, State: evidence.StateCalculated},
		},
	}}
}
