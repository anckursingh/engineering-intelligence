// Package metrics computes deterministic engineering metrics (§16-19) from
// canonical ontology objects. Pure functions of a Population: no I/O, no
// store — investigations (item 17) own the traversal that builds one, so the
// same population always yields the same values (reproducible by §18).
//
// Temporal semantics (§19): every metric reads EVENT times only — created,
// merged, submitted — never source-update or observation/ingestion times.
// Cycle time of a PR created 09-01 and merged 09-05 is four days, no matter
// when it was ingested.
package metrics

import (
	"sort"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// Entity pairs an object with its store external ID — the evidence handle
// observations cite (§16, §20).
type Entity[T any] struct {
	ExternalID string
	Value      T
}

// Population is the object set a metric is computed over.
type Population struct {
	PullRequests      []Entity[ontology.PullRequest]
	Reviews           []Entity[ontology.Review]
	Builds            []Entity[ontology.Build]
	CodeContributions []Entity[ontology.CodeContribution]
	AgentRuns         []Entity[ontology.AgentRun]
	AgentTasks        []Entity[ontology.AgentTask]
	Interactions      []Entity[ontology.Interaction]
}

// Definition declares a metric per §16. The computation is a typed function;
// the definition carries the human-facing contract (formula, filters,
// aggregation, limitations) the investigation layer surfaces later.
type Definition struct {
	Name        string
	Formula     string
	Population  string
	Window      string
	Sources     []string
	Filters     []string
	Aggregation string
	Limitations []string
}

// Window is the half-open event-time window [Start, End) a metric answers
// for. Start >= End yields no observations.
type Window struct {
	Start time.Time
	End   time.Time
}

// Observation is one deterministic metric value. Value is days for duration
// metrics and a count for throughput. Evidence cites the objects the value
// was computed from (§20) with state CALCULATED (§21).
type Observation struct {
	Metric   string              `json:"metric"`
	Value    float64             `json:"value"`
	Entity   string              `json:"entity,omitempty"` // per-entity observations (cycle time, review latency)
	Window   Window              `json:"window"`
	Evidence []evidence.Evidence `json:"evidence"`
}

// Definitions of the implemented metrics (§17: flow first — the set that has
// data; deployment/CI/rework/AI metrics join when their sources exist).
var (
	DefCycleTime = Definition{
		Name:        "cycle_time",
		Formula:     "merged_at - created_at, in days",
		Population:  "merged PullRequests",
		Window:      "merged_at in window (event time)",
		Sources:     []string{"github"},
		Filters:     []string{"merged", "created_at set", "merged_at set", "merged_at >= created_at"},
		Aggregation: "none — one observation per PR",
		Limitations: []string{"reverts/redos count as new PRs"},
	}
	DefReviewLatency = Definition{
		Name:        "review_latency",
		Formula:     "first submitted_at - created_at, in days",
		Population:  "PullRequests with at least one review",
		Window:      "created_at in window (event time)",
		Sources:     []string{"github"},
		Filters:     []string{"submitted_at set", "review belongs to PR"},
		Aggregation: "none — one observation per PR",
		Limitations: []string{"first review = earliest submitted_at, not first human reviewer"},
	}
	DefThroughput = Definition{
		Name:        "throughput",
		Formula:     "count(merged PullRequests)",
		Population:  "merged PullRequests",
		Window:      "merged_at in window (event time)",
		Sources:     []string{"github"},
		Filters:     []string{"merged", "created_at set", "merged_at set", "merged_at >= created_at"},
		Aggregation: "count — one observation per window",
		Limitations: []string{"no per-repo or per-day buckets yet"},
	}
)

// valid returns whether a merged PR can be measured: event times present and
// ordered (out-of-order timestamps are data errors, not metrics).
func valid(pr ontology.PullRequest) bool {
	return pr.Merged && !pr.CreatedAt.IsZero() && !pr.MergedAt.IsZero() && !pr.MergedAt.Before(pr.CreatedAt)
}

// windowEmpty reports an inverted or empty window — no observations possible.
func windowEmpty(w Window) bool {
	return w.End.Before(w.Start) || w.End.Equal(w.Start)
}

// CycleTime computes PR cycle time: merged - created, in days (§17, §19).
// One observation per merged PR whose merged_at falls in the window;
// duplicates collapse by external ID; results are ordered by external ID.
func CycleTime(pop Population, w Window) []Observation {
	if windowEmpty(w) {
		return nil
	}
	seen := map[string]bool{}
	var out []Observation
	for _, e := range pop.PullRequests {
		if seen[e.ExternalID] || !valid(e.Value) || e.Value.MergedAt.Before(w.Start) || !e.Value.MergedAt.Before(w.End) {
			continue
		}
		seen[e.ExternalID] = true
		out = append(out, Observation{
			Metric: DefCycleTime.Name,
			Value:  e.Value.MergedAt.Sub(e.Value.CreatedAt).Hours() / 24,
			Entity: e.ExternalID,
			Window: w,
			Evidence: []evidence.Evidence{
				{Type: "PullRequest", ObjectIDs: []string{e.ExternalID}, State: evidence.StateCalculated},
			},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entity < out[j].Entity })
	return out
}

// ReviewLatency computes time to first review: earliest valid review's
// submitted_at - created_at, in days (§17). One observation per PR whose
// created_at falls in the window and that has at least one valid review of
// its own; duplicates collapse by external ID; ordered by PR external ID.
func ReviewLatency(pop Population, w Window) []Observation {
	if windowEmpty(w) {
		return nil
	}
	// Group valid reviews by their PR (repo + number), keeping the earliest
	// and the evidence handle of that review.
	type key struct {
		repo string
		num  int
	}
	type firstReview struct {
		at    time.Time
		extID string
	}
	earliest := map[key]firstReview{}
	for _, r := range pop.Reviews {
		if r.Value.SubmittedAt.IsZero() {
			continue
		}
		k := key{r.Value.Repository, r.Value.PRNumber}
		if cur, ok := earliest[k]; !ok || r.Value.SubmittedAt.Before(cur.at) {
			earliest[k] = firstReview{at: r.Value.SubmittedAt, extID: r.ExternalID}
		}
	}
	seen := map[string]bool{}
	var out []Observation
	for _, e := range pop.PullRequests {
		if seen[e.ExternalID] || e.Value.CreatedAt.IsZero() || e.Value.CreatedAt.Before(w.Start) || !e.Value.CreatedAt.Before(w.End) {
			continue
		}
		first, ok := earliest[key{e.Value.Repository, e.Value.Number}]
		if !ok {
			continue
		}
		seen[e.ExternalID] = true
		out = append(out, Observation{
			Metric: DefReviewLatency.Name,
			Value:  first.at.Sub(e.Value.CreatedAt).Hours() / 24,
			Entity: e.ExternalID,
			Window: w,
			Evidence: []evidence.Evidence{
				{Type: "PullRequest", ObjectIDs: []string{e.ExternalID}, State: evidence.StateCalculated},
				{Type: "Review", ObjectIDs: []string{first.extID}, State: evidence.StateCalculated},
			},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Entity < out[j].Entity })
	return out
}

// Throughput counts merged PRs whose merged_at falls in the window: one
// observation per window with every counted PR in evidence (§17).
func Throughput(pop Population, w Window) []Observation {
	if windowEmpty(w) {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, e := range pop.PullRequests {
		if seen[e.ExternalID] || !valid(e.Value) || e.Value.MergedAt.Before(w.Start) || !e.Value.MergedAt.Before(w.End) {
			continue
		}
		seen[e.ExternalID] = true
		ids = append(ids, e.ExternalID)
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return []Observation{{
		Metric: DefThroughput.Name,
		Value:  float64(len(ids)),
		Window: w,
		Evidence: []evidence.Evidence{
			{Type: "PullRequest", ObjectIDs: ids, State: evidence.StateCalculated},
		},
	}}
}
