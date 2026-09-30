// comparison.go is population comparison (Milestone F, item 45): the
// cross-sectional companion to MetricChange's temporal comparison. One
// window's merged PRs partition into AI-attributed and no-positive-AI-evidence
// groups. Only validated DIRECT/STRONG contributions support AI attribution.
// A metric that computes on only one side keeps an honest gap on the other;
// one that computes on neither is dropped.
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

// PopulationComparison is one metric compared between AI-attributed and
// no-positive-AI-evidence populations; a nil side is an honest gap, never zero.
type PopulationComparison struct {
	Metric               string           `json:"metric"`
	Label                string           `json:"label"`
	Unit                 string           `json:"unit"`
	AIAttributed         *ComparisonValue `json:"ai_attributed,omitempty"`
	NoPositiveAIEvidence *ComparisonValue `json:"no_positive_ai_evidence,omitempty"`
}

// comparisonMetrics are the PR-scoped metrics compared across populations —
// the metrics anchored on the PR, not on repo-wide or telemetry objects.
var comparisonMetrics = []string{"cycle_time", "review_latency", "pr_size", "review_cycles"}

// ComparePopulations partitions the window's merged PRs by AI attribution
// and compares the PR-scoped metrics between the two populations, returning
// each population's merged-PR count alongside the metric rows.
func ComparePopulations(pop metrics.Population, win metrics.Window) (aiAttributedCount, noPositiveAIEvidenceCount int, rows []PopulationComparison) {
	aiAttributed, noPositiveAIEvidence := partitionPRs(pop, win)
	if len(aiAttributed) == 0 && len(noPositiveAIEvidence) == 0 {
		return 0, 0, nil
	}
	aiPop := metrics.Population{PullRequests: aiAttributed, Reviews: reviewsOf(pop, aiAttributed)}
	noPositiveAIPop := metrics.Population{PullRequests: noPositiveAIEvidence, Reviews: reviewsOf(pop, noPositiveAIEvidence)}
	for _, name := range comparisonMetrics {
		c, ok := candidateByName(name)
		if !ok {
			continue
		}
		aiValue, hasAIValue, aiEvidence := summarize(c.compute(aiPop, win))
		noPositiveValue, hasNoPositiveValue, noPositiveEvidence := summarize(c.compute(noPositiveAIPop, win))
		if !hasAIValue && !hasNoPositiveValue {
			continue
		}
		row := PopulationComparison{Metric: c.name, Label: c.label, Unit: c.unit}
		if hasAIValue {
			row.AIAttributed = &ComparisonValue{Value: aiValue, EpistemicState: evidence.StateCalculated.String(), Evidence: aiEvidence}
		}
		if hasNoPositiveValue {
			row.NoPositiveAIEvidence = &ComparisonValue{Value: noPositiveValue, EpistemicState: evidence.StateCalculated.String(), Evidence: noPositiveEvidence}
		}
		rows = append(rows, row)
	}
	return len(aiAttributed), len(noPositiveAIEvidence), rows
}

// partitionPRs splits merged PRs by positive AI evidence. Unknown, inferred,
// and invalid attribution remains in the no-positive-evidence group; that
// group does not claim the work was human-authored.
func partitionPRs(pop metrics.Population, win metrics.Window) (aiAttributed, noPositiveAIEvidence []metrics.Entity[ontology.PullRequest]) {
	aiAttributedNums := map[string]map[int]bool{}
	for _, c := range pop.CodeContributions {
		if c.Value.PRNumber == 0 || !hasEvidenceQualifiedAttribution(c.Value.Attribution) {
			continue
		}
		m := aiAttributedNums[c.Value.Repository]
		if m == nil {
			m = map[int]bool{}
			aiAttributedNums[c.Value.Repository] = m
		}
		m[c.Value.PRNumber] = true
	}
	for _, e := range pop.PullRequests {
		if !e.Value.Merged || e.Value.MergedAt.IsZero() || e.Value.MergedAt.Before(win.Start) || !e.Value.MergedAt.Before(win.End) {
			continue
		}
		if aiAttributedNums[e.Value.Repository][e.Value.Number] {
			aiAttributed = append(aiAttributed, e)
		} else {
			noPositiveAIEvidence = append(noPositiveAIEvidence, e)
		}
	}
	return aiAttributed, noPositiveAIEvidence
}

func hasEvidenceQualifiedAttribution(a ontology.Attribution) bool {
	if a.Level != ontology.AttributionDirect && a.Level != ontology.AttributionStrong {
		return false
	}
	return a.Validate() == nil
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
