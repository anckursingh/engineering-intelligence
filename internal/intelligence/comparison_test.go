// Package intelligence: population comparison (Milestone F, item 45) — the
// cross-sectional companion to MetricChange's temporal comparison. One
// window's merged PRs partition into AI-assisted (a CodeContribution links
// to the PR) and unattributed, and the PR-scoped metrics compare between
// the two populations. A metric that computes on only one side keeps an
// honest gap on the other; one that computes on neither is dropped.
package intelligence

import (
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// TestComparePopulationsPartitionsAI: contributions on PRs #2 and #4 split
// the window's four merged PRs; cycle time compares 5.0 vs 2.5 days, review
// latency exists only on the unattributed side (no reviews on the assisted
// PRs), pr_size is dropped (no diff data), review cycles are silence on the
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
		t.Errorf("partition = %d assisted / %d unattributed, want 2 / 2", assistedCount, plainCount)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (cycle_time, review_latency, review_cycles — pr_size dropped)", len(rows))
	}

	cycle := rows[0]
	if cycle.Metric != "cycle_time" || cycle.Label != "Cycle time" {
		t.Errorf("rows[0] = %q/%q, want cycle_time", cycle.Metric, cycle.Label)
	}
	if cycle.Assisted == nil || cycle.Assisted.Value != 5.0 {
		t.Errorf("assisted cycle time = %+v, want 5.0 (4d + 6d)", cycle.Assisted)
	}
	if cycle.Unattributed == nil || cycle.Unattributed.Value != 2.5 {
		t.Errorf("unattributed cycle time = %+v, want 2.5 (2d + 3d)", cycle.Unattributed)
	}

	latency := rows[1]
	if latency.Metric != "review_latency" {
		t.Errorf("rows[1] = %q, want review_latency", latency.Metric)
	}
	if latency.Assisted != nil {
		t.Errorf("assisted review latency = %+v, want absent — no reviews on assisted PRs", latency.Assisted)
	}
	if latency.Unattributed == nil || latency.Unattributed.Value != 1.0 {
		t.Errorf("unattributed review latency = %+v, want 1.0", latency.Unattributed)
	}

	cycles := rows[2]
	if cycles.Metric != "review_cycles" {
		t.Errorf("rows[2] = %q, want review_cycles", cycles.Metric)
	}
	// The assisted PRs carry no reviews at all — silence, not a fake zero
	// (the metric's item-35 contract); the unattributed side is reviewed
	// with zero change requests: a real 0.0.
	if cycles.Assisted != nil {
		t.Errorf("assisted review cycles = %+v, want absent — no reviews on assisted PRs", cycles.Assisted)
	}
	if cycles.Unattributed == nil || cycles.Unattributed.Value != 0.0 {
		t.Errorf("unattributed review cycles = %+v, want a real 0.0", cycles.Unattributed)
	}

	// Every computed side carries CALCULATED evidence that reconstructs.
	for _, row := range rows {
		for name, v := range map[string]*ComparisonValue{"assisted": row.Assisted, "unattributed": row.Unattributed} {
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

// TestComparePopulationsUnattributedAbsent: when every merged PR is
// AI-assisted, the unattributed side is empty and its PR-scoped metrics are
// honest gaps, not zeros.
func TestComparePopulationsUnattributedAbsent(t *testing.T) {
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
		t.Errorf("partition = %d assisted / %d unattributed, want 1 / 0", assistedCount, plainCount)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (cycle_time only)", len(rows))
	}
	if rows[0].Unattributed != nil {
		t.Errorf("unattributed cycle time = %+v, want absent — no unattributed PRs", rows[0].Unattributed)
	}
	if rows[0].Assisted == nil || rows[0].Assisted.Value != 2.0 {
		t.Errorf("assisted cycle time = %+v, want 2.0", rows[0].Assisted)
	}
}
