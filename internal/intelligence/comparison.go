// comparison.go is population comparison (Milestone F, item 45): the
// cross-sectional companion to MetricChange's temporal comparison. One
// window's merged PRs partition into AI-assisted (a CodeContribution links
// to the PR) and unattributed, and the PR-scoped metrics compare between
// the two populations. A metric that computes on only one side keeps an
// honest gap on the other; one that computes on neither is dropped.
package intelligence

import (
	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// ComparisonValue is one metric value computed over one population side.
type ComparisonValue struct {
	Value          float64             `json:"value"`
	EpistemicState string              `json:"epistemic_state"`
	Evidence       []evidence.Evidence `json:"evidence"`
}

// PopulationComparison is one metric compared between the AI-assisted and
// unattributed populations; a nil side is an honest gap, never a zero.
type PopulationComparison struct {
	Metric       string           `json:"metric"`
	Label        string           `json:"label"`
	Unit         string           `json:"unit"`
	Assisted     *ComparisonValue `json:"ai_assisted,omitempty"`
	Unattributed *ComparisonValue `json:"unattributed,omitempty"`
}

// comparisonMetrics are the PR-scoped metrics compared across populations —
// the metrics anchored on the PR, not on repo-wide or telemetry objects.
var comparisonMetrics = []string{"cycle_time", "review_latency", "pr_size", "review_cycles"}

// ComparePopulations partitions the window's merged PRs by AI attribution
// and compares the PR-scoped metrics between the two populations, returning
// each population's merged-PR count alongside the metric rows.
func ComparePopulations(pop metrics.Population, win metrics.Window) (assistedCount, unattributedCount int, rows []PopulationComparison) {
	assisted, plain := partitionPRs(pop, win)
	if len(assisted) == 0 && len(plain) == 0 {
		return 0, 0, nil
	}
	aPop := metrics.Population{PullRequests: assisted, Reviews: reviewsOf(pop, assisted)}
	pPop := metrics.Population{PullRequests: plain, Reviews: reviewsOf(pop, plain)}
	for _, name := range comparisonMetrics {
		c, ok := candidateByName(name)
		if !ok {
			continue
		}
		aV, aOK, aEv := summarize(c.compute(aPop, win))
		pV, pOK, pEv := summarize(c.compute(pPop, win))
		if !aOK && !pOK {
			continue
		}
		row := PopulationComparison{Metric: c.name, Label: c.label, Unit: c.unit}
		if aOK {
			row.Assisted = &ComparisonValue{Value: aV, EpistemicState: evidence.StateCalculated.String(), Evidence: aEv}
		}
		if pOK {
			row.Unattributed = &ComparisonValue{Value: pV, EpistemicState: evidence.StateCalculated.String(), Evidence: pEv}
		}
		rows = append(rows, row)
	}
	return len(assisted), len(plain), rows
}

// partitionPRs splits the window's merged PRs by AI attribution: a PR is
// assisted when a CodeContribution names its repository and number.
func partitionPRs(pop metrics.Population, win metrics.Window) (assisted, plain []metrics.Entity[ontology.PullRequest]) {
	assistedNums := map[string]map[int]bool{}
	for _, c := range pop.CodeContributions {
		if c.Value.PRNumber == 0 {
			continue
		}
		m := assistedNums[c.Value.Repository]
		if m == nil {
			m = map[int]bool{}
			assistedNums[c.Value.Repository] = m
		}
		m[c.Value.PRNumber] = true
	}
	for _, e := range pop.PullRequests {
		if !e.Value.Merged || e.Value.MergedAt.IsZero() || e.Value.MergedAt.Before(win.Start) || !e.Value.MergedAt.Before(win.End) {
			continue
		}
		if assistedNums[e.Value.Repository][e.Value.Number] {
			assisted = append(assisted, e)
		} else {
			plain = append(plain, e)
		}
	}
	return assisted, plain
}

// reviewsOf keeps the reviews belonging to the given PRs (repository and
// number — the review carries both).
func reviewsOf(pop metrics.Population, prs []metrics.Entity[ontology.PullRequest]) []metrics.Entity[ontology.Review] {
	byNum := map[string]map[int]bool{}
	for _, e := range prs {
		m := byNum[e.Value.Repository]
		if m == nil {
			m = map[int]bool{}
			byNum[e.Value.Repository] = m
		}
		m[e.Value.Number] = true
	}
	var out []metrics.Entity[ontology.Review]
	for _, r := range pop.Reviews {
		if byNum[r.Value.Repository][r.Value.PRNumber] {
			out = append(out, r)
		}
	}
	return out
}
