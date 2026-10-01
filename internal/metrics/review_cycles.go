// Review cycles (roadmap Layer A): mean count of CHANGES_REQUESTED reviews
// per merged, reviewed PR, anchored at merged_at (§19 event time). A
// reviewed PR with no change-request round is a real 0.0; a merged PR with
// no reviews at all is silence — never zero.
package metrics

import (
	"sort"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// DefReviewCycles declares the metric per §16.
var DefReviewCycles = Definition{
	Name:        "review_cycles",
	Formula:     "count(Reviews with state CHANGES_REQUESTED) per PR",
	Population:  "merged, reviewed PullRequests + their Reviews",
	Window:      "merged_at in window (event time)",
	Sources:     []string{"github"},
	Filters:     []string{"merged", "review belongs to PR", "state CHANGES_REQUESTED counts"},
	Aggregation: "mean over reviewed merged PRs — one observation per window",
	Limitations: []string{
		"counts changes_requested review submissions, not resubmission events — a dismissed/superseded request still counts",
		"merged PRs with no reviews at all are excluded (silence); reviewed PRs with none are real zeros",
	},
}

// ReviewCycles computes the mean number of change-request rounds per merged
// PR whose merged_at falls in the window. PRs without any review carry no
// data and never count; duplicates collapse by external ID. Evidence cites
// every counted PR and its changes_requested reviews.
func ReviewCycles(pop Population, w Window) []Observation {
	if windowEmpty(w) {
		return nil
	}
	type key struct {
		repo string
		num  int
	}
	perPR := map[key][]Entity[ontology.Review]{}
	for _, r := range pop.Reviews {
		k := key{r.Value.Repository, r.Value.PRNumber}
		perPR[k] = append(perPR[k], r)
	}
	seen := map[string]bool{}
	var total float64
	var prIDs, revIDs []string
	for _, e := range pop.PullRequests {
		if seen[e.ExternalID] || !valid(e.Value) || e.Value.MergedAt.Before(w.Start) || !e.Value.MergedAt.Before(w.End) {
			continue
		}
		revs, ok := perPR[key{e.Value.Repository, e.Value.Number}]
		if !ok {
			continue
		}
		seen[e.ExternalID] = true
		prIDs = append(prIDs, e.ExternalID)
		for _, r := range revs {
			if r.Value.State == "CHANGES_REQUESTED" {
				total++
				revIDs = append(revIDs, r.ExternalID)
			}
		}
	}
	if len(prIDs) == 0 {
		return nil
	}
	sort.Strings(prIDs)
	sort.Strings(revIDs)
	ev := []evidence.Evidence{{Type: "PullRequest", ObjectIDs: prIDs, State: evidence.StateCalculated}}
	if len(revIDs) > 0 {
		ev = append(ev, evidence.Evidence{Type: "Review", ObjectIDs: revIDs, State: evidence.StateCalculated})
	}
	return []Observation{{
		Metric:   DefReviewCycles.Name,
		Value:    total / float64(len(prIDs)),
		Window:   w,
		Evidence: ev,
	}}
}
