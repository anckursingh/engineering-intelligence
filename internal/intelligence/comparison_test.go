// Package intelligence: population comparison (Milestone F, item 45) — the
// cross-sectional companion to MetricChange's temporal comparison. One
// window's merged PRs partition by evidence-qualified AI attribution. PR-scoped
// metrics compare the AI-attributed and no-positive-evidence groups. A metric
// that computes on only one side keeps an honest gap on the other; one that
// computes on neither is dropped.
package intelligence

import (
	"fmt"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// TestComparePopulationsPartitionsAI: contributions on PRs #2 and #4 split
// the window's four merged PRs; cycle time compares 5.0 vs 2.5 days, review
// latency exists only on the no-positive-evidence side (no reviews on the
// AI-attributed PRs), pr_size is dropped (no diff data), review cycles are silence on the
// unreviewed side and a real zero on the reviewed one.
func TestComparePopulationsPartitionsAI(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	win := metrics.Window{Start: base, End: base.Add(30 * 24 * time.Hour)}
	pop := metrics.Population{
		PullRequests: []metrics.Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base.Add(1*24*time.Hour), base.Add(3*24*time.Hour)), // 2d
			pr("github.com:pr:acme/widgets#2", 2, base.Add(1*24*time.Hour), base.Add(5*24*time.Hour)), // 4d
			pr("github.com:pr:acme/widgets#3", 3, base.Add(2*24*time.Hour), base.Add(5*24*time.Hour)), // 3d
			pr("github.com:pr:acme/widgets#4", 4, base.Add(1*24*time.Hour), base.Add(7*24*time.Hour)), // 6d
		},
		Reviews: []metrics.Entity[ontology.Review]{
			review("github.com:review:acme/widgets#1@1", 1, base.Add(2*24*time.Hour)), // +1d
			review("github.com:review:acme/widgets#3@1", 3, base.Add(3*24*time.Hour)), // +1d
		},
		CodeContributions: []metrics.Entity[ontology.CodeContribution]{
			aiContrib("ei.com:ai-contribution:claude-code:c2", 2),
			aiContrib("ei.com:ai-contribution:claude-code:c4", 4),
		},
	}
	assistedCount, plainCount, rows := ComparePopulations(pop, win)

	if assistedCount != 2 || plainCount != 2 {
		t.Errorf("partition = %d AI-attributed / %d without positive AI evidence, want 2 / 2", assistedCount, plainCount)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (cycle_time, review_latency, review_cycles — pr_size dropped)", len(rows))
	}

	cycle := rows[0]
	if cycle.Metric != "cycle_time" || cycle.Label != "Cycle time" {
		t.Errorf("rows[0] = %q/%q, want cycle_time", cycle.Metric, cycle.Label)
	}
	if cycle.AIAttributed == nil || cycle.AIAttributed.Value != 5.0 {
		t.Errorf("AI-attributed cycle time = %+v, want 5.0 (4d + 6d)", cycle.AIAttributed)
	}
	if cycle.NoPositiveAIEvidence == nil || cycle.NoPositiveAIEvidence.Value != 2.5 {
		t.Errorf("no-positive-AI-evidence cycle time = %+v, want 2.5 (2d + 3d)", cycle.NoPositiveAIEvidence)
	}

	latency := rows[1]
	if latency.Metric != "review_latency" {
		t.Errorf("rows[1] = %q, want review_latency", latency.Metric)
	}
	if latency.AIAttributed != nil {
		t.Errorf("AI-attributed review latency = %+v, want absent — no reviews on those PRs", latency.AIAttributed)
	}
	if latency.NoPositiveAIEvidence == nil || latency.NoPositiveAIEvidence.Value != 1.0 {
		t.Errorf("no-positive-AI-evidence review latency = %+v, want 1.0", latency.NoPositiveAIEvidence)
	}

	cycles := rows[2]
	if cycles.Metric != "review_cycles" {
		t.Errorf("rows[2] = %q, want review_cycles", cycles.Metric)
	}
	// The AI-attributed PRs carry no reviews at all — silence, not a fake zero
	// (the metric's item-35 contract); the other side is reviewed
	// with zero change requests: a real 0.0.
	if cycles.AIAttributed != nil {
		t.Errorf("AI-attributed review cycles = %+v, want absent — no reviews on those PRs", cycles.AIAttributed)
	}
	if cycles.NoPositiveAIEvidence == nil || cycles.NoPositiveAIEvidence.Value != 0.0 {
		t.Errorf("no-positive-AI-evidence review cycles = %+v, want a real 0.0", cycles.NoPositiveAIEvidence)
	}

	// Every computed side carries CALCULATED evidence that reconstructs.
	for _, row := range rows {
		for name, v := range map[string]*ComparisonValue{"AI-attributed": row.AIAttributed, "no-positive-AI-evidence": row.NoPositiveAIEvidence} {
			if v == nil {
				continue
			}
			if v.EpistemicState != evidence.StateCalculated.String() {
				t.Errorf("%s %s state = %q, want CALCULATED", row.Metric, name, v.EpistemicState)
			}
			if len(v.Evidence) == 0 {
				t.Errorf("%s %s has no evidence", row.Metric, name)
			}
		}
	}
}

// TestComparePopulationsNoPositiveEvidenceAbsent: when every merged PR has
// positive AI evidence, the other side is empty and its PR-scoped metrics are
// honest gaps, not zeros.
func TestComparePopulationsNoPositiveEvidenceAbsent(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	win := metrics.Window{Start: base, End: base.Add(30 * 24 * time.Hour)}
	pop := metrics.Population{
		PullRequests: []metrics.Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base.Add(1*24*time.Hour), base.Add(3*24*time.Hour)),
		},
		CodeContributions: []metrics.Entity[ontology.CodeContribution]{
			aiContrib("ei.com:ai-contribution:claude-code:c1", 1),
		},
	}
	assistedCount, plainCount, rows := ComparePopulations(pop, win)
	if assistedCount != 1 || plainCount != 0 {
		t.Errorf("partition = %d AI-attributed / %d without positive AI evidence, want 1 / 0", assistedCount, plainCount)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (cycle_time only)", len(rows))
	}
	if rows[0].NoPositiveAIEvidence != nil {
		t.Errorf("no-positive-AI-evidence cycle time = %+v, want absent — no PRs in that group", rows[0].NoPositiveAIEvidence)
	}
	if rows[0].AIAttributed == nil || rows[0].AIAttributed.Value != 2.0 {
		t.Errorf("AI-attributed cycle time = %+v, want 2.0", rows[0].AIAttributed)
	}
}

func TestComparePopulationsRequiresEvidenceQualifiedAttribution(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	win := metrics.Window{Start: base, End: base.Add(30 * 24 * time.Hour)}
	var prs []metrics.Entity[ontology.PullRequest]
	for n := 1; n <= 5; n++ {
		created := base.Add(time.Duration(n) * 24 * time.Hour)
		prs = append(prs, pr(fmt.Sprintf("github.com:pr:acme/widgets#%d", n), n, created, created.Add(24*time.Hour)))
	}
	pop := metrics.Population{
		PullRequests: prs,
		CodeContributions: []metrics.Entity[ontology.CodeContribution]{
			{Value: ontology.CodeContribution{Repository: "acme/widgets", PRNumber: 1, Attribution: ontology.Attribution{Level: ontology.AttributionDirect, Source: "claude-code", Evidence: "session:s1"}}},
			{Value: ontology.CodeContribution{Repository: "acme/widgets", PRNumber: 2, Attribution: ontology.Attribution{Level: ontology.AttributionStrong, Source: "claude-code", Evidence: "session:s2"}}},
			{Value: ontology.CodeContribution{Repository: "acme/widgets", PRNumber: 3, Attribution: ontology.Attribution{Level: ontology.AttributionInferred, Source: "heuristic", Evidence: "link:l3"}}},
			{Value: ontology.CodeContribution{Repository: "acme/widgets", PRNumber: 4, Attribution: ontology.Attribution{Level: ontology.AttributionUnknown}}},
			{Value: ontology.CodeContribution{Repository: "acme/widgets", PRNumber: 5, Attribution: ontology.Attribution{Level: ontology.AttributionDirect, Source: "claude-code"}}},
		},
	}

	aiAttributed, noPositiveEvidence, _ := ComparePopulations(pop, win)
	if aiAttributed != 2 || noPositiveEvidence != 3 {
		t.Errorf("partition = %d AI-attributed / %d without positive evidence, want 2 / 3", aiAttributed, noPositiveEvidence)
	}
}
