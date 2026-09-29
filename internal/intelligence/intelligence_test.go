// Package intelligence: the first investigation (§22-23) — "why did cycle
// time change?" — as a deterministic function of a Population. §23's fixture
// is pinned verbatim: Month A cycle time 2 days, Month B 4 days, and the
// investigation names the changed factors WITHOUT claiming causality.
package intelligence

import (
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

func pr(extID string, num int, created, merged time.Time) metrics.Entity[ontology.PullRequest] {
	return metrics.Entity[ontology.PullRequest]{
		ExternalID: extID,
		Value: ontology.PullRequest{
			Repository: "acme/widgets", Number: num, Merged: true,
			CreatedAt: created, MergedAt: merged,
		},
	}
}

func review(extID string, prNum int, submitted time.Time) metrics.Entity[ontology.Review] {
	return metrics.Entity[ontology.Review]{
		ExternalID: extID,
		Value:      ontology.Review{Repository: "acme/widgets", PRNumber: prNum, SubmittedAt: submitted},
	}
}

// monthA/monthB are the §23 fixture: Month A cycle time 2 days, Month B 4.
// Review latency doubles (1 → 2 days); throughput falls (3 → 2 merged PRs).
func monthA() (metrics.Population, Window) {
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	pop := metrics.Population{
		PullRequests: []metrics.Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base.Add(24*time.Hour), base.Add(3*24*time.Hour)),   // 2d
			pr("github.com:pr:acme/widgets#2", 2, base.Add(2*24*time.Hour), base.Add(4*24*time.Hour)), // 2d
			pr("github.com:pr:acme/widgets#3", 3, base.Add(3*24*time.Hour), base.Add(5*24*time.Hour)), // 2d
		},
		Reviews: []metrics.Entity[ontology.Review]{
			review("github.com:review:acme/widgets#1@1", 1, base.Add(2*24*time.Hour)), // 1d after created
			review("github.com:review:acme/widgets#2@1", 2, base.Add(3*24*time.Hour)),
			review("github.com:review:acme/widgets#3@1", 3, base.Add(4*24*time.Hour)),
		},
	}
	return pop, Window{Name: "Month A", Range: metrics.Window{Start: base, End: base.Add(31 * 24 * time.Hour)}}
}

func monthB() (metrics.Population, Window) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	pop := metrics.Population{
		PullRequests: []metrics.Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#4", 4, base.Add(24*time.Hour), base.Add(5*24*time.Hour)),   // 4d
			pr("github.com:pr:acme/widgets#5", 5, base.Add(2*24*time.Hour), base.Add(6*24*time.Hour)), // 4d
		},
		Reviews: []metrics.Entity[ontology.Review]{
			review("github.com:review:acme/widgets#4@1", 4, base.Add(3*24*time.Hour)), // 2d after created
			review("github.com:review:acme/widgets#5@1", 5, base.Add(4*24*time.Hour)),
		},
	}
	return pop, Window{Name: "Month B", Range: metrics.Window{Start: base, End: base.Add(30 * 24 * time.Hour)}}
}

// TestCycleTimeChangeIdentifiesCandidates is §23 verbatim: the investigation
// names review latency and throughput as candidate contributing factors, and
// states the association without claiming causality.
func TestCycleTimeChangeIdentifiesCandidates(t *testing.T) {
	popA, a := monthA()
	popB, b := monthB()
	pop := metrics.Population{
		PullRequests: append(append([]metrics.Entity[ontology.PullRequest]{}, popA.PullRequests...), popB.PullRequests...),
		Reviews:      append(append([]metrics.Entity[ontology.Review]{}, popA.Reviews...), popB.Reviews...),
	}
	got := CycleTimeChange(pop, a, b)

	want := "Cycle time increased from 2.0 to 4.0 days. " +
		"Review latency increased by 100.0%. " +
		"Throughput decreased by 33.3%. " +
		"The data supports an association, but does not establish causality."
	if got.Statement != want {
		t.Errorf("statement = %q\nwant       %q", got.Statement, want)
	}
	if got.Question == "" {
		t.Error("question missing")
	}
	if got.State != evidence.StateCalculated {
		t.Errorf("state = %q, want CALCULATED (§21: association is deterministic math)", got.State)
	}
	if got.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1.0 (arithmetic of the comparison)", got.Confidence)
	}
	if got.Primary.From != 2.0 || got.Primary.To != 4.0 {
		t.Errorf("primary = %v→%v, want 2.0→4.0", got.Primary.From, got.Primary.To)
	}
	if len(got.Factors) != 2 {
		t.Fatalf("factors = %d, want 2 (review_latency, throughput)", len(got.Factors))
	}
	if got.Factors[0].Metric != "review_latency" || got.Factors[1].Metric != "throughput" {
		t.Errorf("factor order = %v, want review_latency then throughput", got.Factors)
	}
	for i, f := range append([]Finding{got.Primary}, got.Factors...) {
		if len(f.Evidence) == 0 {
			t.Errorf("finding %d has no evidence — lineage must reconstruct (§20)", i)
		}
	}
	if len(got.Limitations) == 0 {
		t.Error("limitations missing")
	}
}

// TestCycleTimeChangeUnchanged: no primary change → no investigation beyond
// the unchanged statement, no factor candidates.
func TestCycleTimeChangeUnchanged(t *testing.T) {
	popA, a := monthA()
	_, b := monthA() // same data, different window name
	got := CycleTimeChange(metrics.Population{
		PullRequests: popA.PullRequests,
		Reviews:      popA.Reviews,
	}, a, b)
	if got.Statement != "Cycle time did not change (2.0 days)." {
		t.Errorf("statement = %q", got.Statement)
	}
	if len(got.Factors) != 0 {
		t.Errorf("factors = %v, want none", got.Factors)
	}
}

// TestCycleTimeChangeMissingData: a window without cycle-time observations
// cannot be investigated — an honest statement, not an error.
func TestCycleTimeChangeMissingData(t *testing.T) {
	popA, a := monthA()
	got := CycleTimeChange(popA, a, Window{Name: "Month B", Range: metrics.Window{
		Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}})
	if got.Statement != "Not enough cycle_time data in Month B to investigate." {
		t.Errorf("statement = %q", got.Statement)
	}
}

// TestCycleTimeChangeFactorAppeared: a factor with no observations in Month
// A has no percentage and no fake zero — the statement says it appeared. The
// symmetric case says it disappeared.
func TestCycleTimeChangeFactorAppeared(t *testing.T) {
	popA, a := monthA()
	popB, b := monthB()
	prs := append(append([]metrics.Entity[ontology.PullRequest]{}, popA.PullRequests...), popB.PullRequests...)

	got := CycleTimeChange(metrics.Population{PullRequests: prs, Reviews: popB.Reviews}, a, b)
	want := "Cycle time increased from 2.0 to 4.0 days. " +
		"Review latency appeared in Month B at 2.0 days. " +
		"Throughput decreased by 33.3%. " +
		"Review cycles appeared in Month B at 0.0 cycles. " +
		"The data supports an association, but does not establish causality."
	if got.Statement != want {
		t.Errorf("appeared statement = %q\nwant               %q", got.Statement, want)
	}

	got = CycleTimeChange(metrics.Population{PullRequests: prs, Reviews: popA.Reviews}, a, b)
	want = "Cycle time increased from 2.0 to 4.0 days. " +
		"Review latency disappeared in Month B. " +
		"Throughput decreased by 33.3%. " +
		"Review cycles disappeared in Month B. " +
		"The data supports an association, but does not establish causality."
	if got.Statement != want {
		t.Errorf("disappeared statement = %q\nwant                 %q", got.Statement, want)
	}
}

// TestCycleTimeChangeFlatFactorsExcluded: factors that did not move are not
// candidates — the statement lists only the primary change.
func TestCycleTimeChangeFlatFactorsExcluded(t *testing.T) {
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	a := Window{Name: "Month A", Range: metrics.Window{Start: base, End: base.Add(31 * 24 * time.Hour)}}
	b := Window{Name: "Month B", Range: metrics.Window{Start: base.Add(31 * 24 * time.Hour), End: base.Add(61 * 24 * time.Hour)}}
	// Two identical PRs, one per window: cycle time 2→2, one review each at
	// +1d, throughput 1→1. Nothing but the primary sentence.
	pop := metrics.Population{
		PullRequests: []metrics.Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base.Add(24*time.Hour), base.Add(3*24*time.Hour)),
			pr("github.com:pr:acme/widgets#2", 2, base.Add(32*24*time.Hour), base.Add(34*24*time.Hour)),
		},
		Reviews: []metrics.Entity[ontology.Review]{
			review("github.com:review:acme/widgets#1@1", 1, base.Add(2*24*time.Hour)),
			review("github.com:review:acme/widgets#2@1", 2, base.Add(33*24*time.Hour)),
		},
	}
	got := CycleTimeChange(pop, a, b)
	want := "Cycle time did not change (2.0 days)."
	if got.Statement != want {
		t.Errorf("statement = %q\nwant       %q", got.Statement, want)
	}
}

// aiContrib builds a valid DIRECT contribution for a PR number.
func aiContrib(extID string, prNum int) metrics.Entity[ontology.CodeContribution] {
	return metrics.Entity[ontology.CodeContribution]{
		ExternalID: extID,
		Value: ontology.CodeContribution{
			Repository: "acme/widgets", PRNumber: prNum,
			Attribution: ontology.Attribution{Level: ontology.AttributionDirect, Source: "claude-code", Evidence: "session:s1"},
		},
	}
}

// TestCycleTimeChangeAIFactor (§27): when the AI-assisted PR share moves
// alongside cycle time it joins the candidate factors — same association,
// same non-causality statement.
func TestCycleTimeChangeAIFactor(t *testing.T) {
	popA, a := monthA()
	popB, b := monthB()
	pop := metrics.Population{
		PullRequests: append(append([]metrics.Entity[ontology.PullRequest]{}, popA.PullRequests...), popB.PullRequests...),
		Reviews:      append(append([]metrics.Entity[ontology.Review]{}, popA.Reviews...), popB.Reviews...),
		// No contributions in Month A; both Month B PRs DIRECT-attributed.
		CodeContributions: []metrics.Entity[ontology.CodeContribution]{
			aiContrib("ei.com:ai-contribution:claude-code:c4", 4),
			aiContrib("ei.com:ai-contribution:claude-code:c5", 5),
		},
	}
	got := CycleTimeChange(pop, a, b)
	want := "Cycle time increased from 2.0 to 4.0 days. " +
		"Review latency increased by 100.0%. " +
		"Throughput decreased by 33.3%. " +
		"AI-assisted PR percentage changed from 0.0 to 100.0 %. " +
		"The data supports an association, but does not establish causality."
	if got.Statement != want {
		t.Errorf("statement = %q\nwant       %q", got.Statement, want)
	}
	if len(got.Factors) != 3 {
		t.Fatalf("factors = %d, want 3", len(got.Factors))
	}
	if f := got.Factors[2]; f.Metric != "ai_assisted_pr_pct" || f.From != 0.0 || f.To != 100.0 {
		t.Errorf("AI factor = %+v, want ai_assisted_pr_pct 0.0→100.0", f)
	}
}

// ciBuild builds one verdict Build entity completed at the given time.
func ciBuild(extID string, num int, conclusion string, completed time.Time) metrics.Entity[ontology.Build] {
	return metrics.Entity[ontology.Build]{
		ExternalID: extID,
		Value: ontology.Build{
			Repository: "acme/widgets", ID: int64(num),
			Conclusion: conclusion, CompletedAt: completed,
		},
	}
}

// TestMetricChangeCIQuality (Milestone F, item 44): any supported question
// runs the shared two-window engine with its own primary — here CI quality,
// 75% pass in Month A falling to 25% in Month B — and the primary never
// doubles as one of its own factors.
func TestMetricChangeCIQuality(t *testing.T) {
	popA, a := monthA()
	popB, b := monthB()
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	pop := metrics.Population{
		PullRequests: append(append([]metrics.Entity[ontology.PullRequest]{}, popA.PullRequests...), popB.PullRequests...),
		Reviews:      append(append([]metrics.Entity[ontology.Review]{}, popA.Reviews...), popB.Reviews...),
		Builds: []metrics.Entity[ontology.Build]{
			// Month A: 3 pass, 1 fail → 75%.
			ciBuild("github.com:build:acme/widgets:1", 1, "success", base.Add(2*24*time.Hour)),
			ciBuild("github.com:build:acme/widgets:2", 2, "success", base.Add(4*24*time.Hour)),
			ciBuild("github.com:build:acme/widgets:3", 3, "success", base.Add(6*24*time.Hour)),
			ciBuild("github.com:build:acme/widgets:4", 4, "failure", base.Add(8*24*time.Hour)),
			// Month B: 1 pass, 3 fail → 25%.
			ciBuild("github.com:build:acme/widgets:5", 5, "success", base.Add(33*24*time.Hour)),
			ciBuild("github.com:build:acme/widgets:6", 6, "failure", base.Add(35*24*time.Hour)),
			ciBuild("github.com:build:acme/widgets:7", 7, "failure", base.Add(37*24*time.Hour)),
			ciBuild("github.com:build:acme/widgets:8", 8, "failure", base.Add(39*24*time.Hour)),
		},
	}
	got := MetricChange(pop, a, b, "ci_pass_rate", QuestionCIQuality)

	want := "CI pass rate decreased from 75.0 to 25.0 %. " +
		"Cycle time increased by 100.0%. " +
		"Review latency increased by 100.0%. " +
		"Throughput decreased by 33.3%. " +
		"The data supports an association, but does not establish causality."
	if got.Statement != want {
		t.Errorf("statement = %q\nwant       %q", got.Statement, want)
	}
	if got.Question != QuestionCIQuality {
		t.Errorf("question = %q, want %q", got.Question, QuestionCIQuality)
	}
	if got.Primary.Metric != "ci_pass_rate" || got.Primary.From != 75.0 || got.Primary.To != 25.0 {
		t.Errorf("primary = %+v, want ci_pass_rate 75.0→25.0", got.Primary)
	}
	if len(got.Factors) != 3 {
		t.Fatalf("factors = %d, want 3 (cycle_time, review_latency, throughput)", len(got.Factors))
	}
	for _, f := range got.Factors {
		if f.Metric == "ci_pass_rate" {
			t.Errorf("primary listed as its own factor: %+v", f)
		}
	}
}

// TestMetricChangeMissingData (Milestone F): the honest missing-data
// statement names the primary metric and the first window lacking data.
func TestMetricChangeMissingData(t *testing.T) {
	popA, a := monthA()
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	pop := metrics.Population{
		PullRequests: popA.PullRequests,
		Reviews:      popA.Reviews,
		Builds: []metrics.Entity[ontology.Build]{
			ciBuild("github.com:build:acme/widgets:1", 1, "success", base.Add(2*24*time.Hour)),
			ciBuild("github.com:build:acme/widgets:2", 2, "failure", base.Add(4*24*time.Hour)),
		},
	}
	got := MetricChange(pop, a, Window{Name: "Month B", Range: metrics.Window{
		Start: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
	}}, "ci_pass_rate", QuestionCIQuality)
	if got.Statement != "Not enough ci_pass_rate data in Month B to investigate." {
		t.Errorf("statement = %q", got.Statement)
	}
	if got.Question != QuestionCIQuality {
		t.Errorf("question = %q, want %q", got.Question, QuestionCIQuality)
	}
}

// TestCycleTimeChangeAIFlatExcluded: an unchanged AI-assisted share (100% in
// both windows) is not a candidate factor.
func TestCycleTimeChangeAIFlatExcluded(t *testing.T) {
	popA, a := monthA()
	popB, b := monthB()
	pop := metrics.Population{
		PullRequests: append(append([]metrics.Entity[ontology.PullRequest]{}, popA.PullRequests...), popB.PullRequests...),
		Reviews:      append(append([]metrics.Entity[ontology.Review]{}, popA.Reviews...), popB.Reviews...),
		CodeContributions: []metrics.Entity[ontology.CodeContribution]{
			aiContrib("c1", 1), aiContrib("c2", 2), aiContrib("c3", 3), // Month A: 100%
			aiContrib("c4", 4), aiContrib("c5", 5), // Month B: 100%
		},
	}
	got := CycleTimeChange(pop, a, b)
	if len(got.Factors) != 2 {
		t.Errorf("factors = %d, want 2 — flat AI share is not a candidate", len(got.Factors))
	}
	for _, f := range got.Factors {
		if f.Metric == "ai_assisted_pr_pct" {
			t.Errorf("flat ai_assisted_pr_pct listed as factor: %+v", f)
		}
	}
}
