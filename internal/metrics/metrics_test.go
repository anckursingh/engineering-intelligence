// Package metrics: deterministic tests per §18 — given known objects and
// known timestamps, each metric returns an exact expected value. The edge
// cases of §18 and the temporal semantics of §19 are pinned here.
package metrics

import (
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// inZone re-expresses an instant in a named IANA zone — timezone-boundary
// cases (§18) prove metrics compare instants, not wall-clock strings.
func inZone(t *testing.T, in time.Time, zone string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Fatalf("load location %s: %v", zone, err)
	}
	return in.In(loc)
}

func pr(extID string, num int, created, merged time.Time, isMerged bool) Entity[ontology.PullRequest] {
	return Entity[ontology.PullRequest]{
		ExternalID: extID,
		Value: ontology.PullRequest{
			Repository: "acme/widgets",
			Number:     num,
			Merged:     isMerged,
			CreatedAt:  created,
			MergedAt:   merged,
		},
	}
}

func review(extID string, prNum int, submitted time.Time) Entity[ontology.Review] {
	return Entity[ontology.Review]{
		ExternalID: extID,
		Value: ontology.Review{
			Repository:  "acme/widgets",
			PRNumber:    prNum,
			SubmittedAt: submitted,
		},
	}
}

// TestCycleTimeExactValue is the §19 example verbatim: created 09-01, merged
// 09-05, ingested (updated_at) 09-08 — cycle time is four days, not seven.
func TestCycleTimeExactValue(t *testing.T) {
	created := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	merged := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	pop := Population{PullRequests: []Entity[ontology.PullRequest]{{
		ExternalID: "github.com:pr:acme/widgets#1",
		Value: ontology.PullRequest{
			Repository: "acme/widgets", Number: 1, Merged: true,
			CreatedAt: created, MergedAt: merged,
			UpdatedAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), // ingested later
		},
	}}}
	got := CycleTime(pop, Window{Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)})
	if len(got) != 1 {
		t.Fatalf("cycle time observations = %d, want 1", len(got))
	}
	if got[0].Value != 4.0 {
		t.Errorf("cycle time = %v days, want 4.0 (event times, not observation time)", got[0].Value)
	}
	if got[0].Entity != "github.com:pr:acme/widgets#1" {
		t.Errorf("entity = %q", got[0].Entity)
	}
	if len(got[0].Evidence) != 1 || len(got[0].Evidence[0].ObjectIDs) != 1 || got[0].Evidence[0].ObjectIDs[0] != "github.com:pr:acme/widgets#1" {
		t.Errorf("evidence = %v, want the PR", got[0].Evidence)
	}
	if got[0].Evidence[0].Type != "PullRequest" {
		t.Errorf("evidence type = %q, want PullRequest", got[0].Evidence[0].Type)
	}
	if got[0].Evidence[0].State != evidence.StateCalculated {
		t.Errorf("evidence state = %q, want CALCULATED (§21: deterministic math over observed objects)", got[0].Evidence[0].State)
	}
}

// TestCycleTimeEdges pins §18: empty population, missing timestamps, partial
// data, out-of-order events, and half-open window boundaries.
func TestCycleTimeEdges(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	w := Window{Start: base, End: base.Add(30 * 24 * time.Hour)}
	cases := []struct {
		name string
		pop  Population
		want int
	}{
		{"empty population", Population{}, 0},
		{"missing created_at", Population{PullRequests: []Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, time.Time{}, base.Add(4*24*time.Hour), true),
		}}, 0},
		{"missing merged_at", Population{PullRequests: []Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base, time.Time{}, true),
		}}, 0},
		{"unmerged", Population{PullRequests: []Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base, base.Add(4*24*time.Hour), false),
		}}, 0},
		{"out-of-order (merged before created)", Population{PullRequests: []Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base.Add(4*24*time.Hour), base, true),
		}}, 0},
		{"merged before window", Population{PullRequests: []Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base.Add(-2*24*time.Hour), base.Add(-1*time.Hour), true),
		}}, 0},
		{"merged at window start (included)", Population{PullRequests: []Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base.Add(-24*time.Hour), base, true),
		}}, 1},
		{"merged at window end (excluded)", Population{PullRequests: []Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base, base.Add(30*24*time.Hour), true),
		}}, 0},
		{"late-arriving event counts by event time", Population{PullRequests: []Entity[ontology.PullRequest]{{
			ExternalID: "github.com:pr:acme/widgets#1",
			Value: ontology.PullRequest{
				Repository: "acme/widgets", Number: 1, Merged: true,
				CreatedAt: base, MergedAt: base.Add(4 * 24 * time.Hour),
				UpdatedAt: base.Add(60 * 24 * time.Hour), // source update after the window: must not matter
			},
		}}}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CycleTime(c.pop, w); len(got) != c.want {
				t.Errorf("observations = %d, want %d", len(got), c.want)
			}
		})
	}
}

// TestCycleTimeDuplicateEvent: the same PR twice in a population is one
// observation (idempotent population handling).
func TestCycleTimeDuplicateEvent(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	pop := Population{PullRequests: []Entity[ontology.PullRequest]{
		pr("github.com:pr:acme/widgets#1", 1, base, base.Add(4*24*time.Hour), true),
		pr("github.com:pr:acme/widgets#1", 1, base, base.Add(4*24*time.Hour), true),
	}}
	got := CycleTime(pop, Window{Start: base, End: base.Add(30 * 24 * time.Hour)})
	if len(got) != 1 {
		t.Fatalf("observations = %d, want 1 (duplicate collapsed)", len(got))
	}
}

// TestCycleTimeTimezoneBoundary: instants expressed in different zones must
// not shift the duration — metrics compare instants, not wall clocks.
func TestCycleTimeTimezoneBoundary(t *testing.T) {
	created := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	merged := time.Date(2026, 9, 5, 21, 0, 0, 0, time.UTC) // 4.5 days later
	pop := Population{PullRequests: []Entity[ontology.PullRequest]{{
		ExternalID: "github.com:pr:acme/widgets#1",
		Value: ontology.PullRequest{
			Repository: "acme/widgets", Number: 1, Merged: true,
			CreatedAt: inZone(t, created, "America/New_York"),
			MergedAt:  inZone(t, merged, "Asia/Tokyo"),
		},
	}}}
	got := CycleTime(pop, Window{Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)})
	if len(got) != 1 || got[0].Value != 4.5 {
		t.Fatalf("cycle time = %v, want 4.5 days across zones", got)
	}
}

// TestReviewLatencyExactValue: time to FIRST review, by the earliest valid
// review; wrong-PR and missing-timestamp reviews never count.
func TestReviewLatencyExactValue(t *testing.T) {
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	pop := Population{
		PullRequests: []Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base, base.Add(6*24*time.Hour), true),
		},
		Reviews: []Entity[ontology.Review]{
			review("github.com:review:acme/widgets#1@1002", 1, base.Add(3*24*time.Hour)), // second review
			review("github.com:review:acme/widgets#1@1001", 1, base.Add(12*time.Hour)),   // FIRST review
			review("github.com:review:acme/widgets#1@1003", 2, base.Add(1*time.Hour)),    // wrong PR
			review("github.com:review:acme/widgets#1@1004", 1, time.Time{}),              // missing timestamp
			review("github.com:review:acme/widgets#1@1001", 1, base.Add(12*time.Hour)),   // duplicate
		},
	}
	w := Window{Start: base, End: base.Add(30 * 24 * time.Hour)}
	got := ReviewLatency(pop, w)
	if len(got) != 1 {
		t.Fatalf("observations = %d, want 1", len(got))
	}
	if got[0].Value != 0.5 {
		t.Errorf("review latency = %v days, want 0.5 (earliest review)", got[0].Value)
	}
	ids := map[string]bool{}
	for _, e := range got[0].Evidence {
		for _, id := range e.ObjectIDs {
			ids[id] = true
		}
	}
	if !ids["github.com:review:acme/widgets#1@1001"] || !ids["github.com:pr:acme/widgets#1"] {
		t.Errorf("evidence = %v, want the PR and its first review", got[0].Evidence)
	}
}

// TestReviewLatencyEdges: no reviews / no valid reviews → no observation.
func TestReviewLatencyEdges(t *testing.T) {
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	w := Window{Start: base, End: base.Add(30 * 24 * time.Hour)}
	cases := []struct {
		name string
		pop  Population
	}{
		{"no reviews", Population{PullRequests: []Entity[ontology.PullRequest]{
			pr("github.com:pr:acme/widgets#1", 1, base, base.Add(24*time.Hour), true),
		}}},
		{"only invalid reviews", Population{
			PullRequests: []Entity[ontology.PullRequest]{
				pr("github.com:pr:acme/widgets#1", 1, base, base.Add(24*time.Hour), true),
			},
			Reviews: []Entity[ontology.Review]{
				review("github.com:review:acme/widgets#1@1001", 1, time.Time{}),
			},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ReviewLatency(c.pop, w); len(got) != 0 {
				t.Errorf("observations = %d, want 0", len(got))
			}
		})
	}
}

// TestThroughputExactValue: merged PRs in the window, one observation per
// window, evidence cites every counted PR.
func TestThroughputExactValue(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	pop := Population{PullRequests: []Entity[ontology.PullRequest]{
		pr("github.com:pr:acme/widgets#1", 1, base, base.Add(24*time.Hour), true),
		pr("github.com:pr:acme/widgets#2", 2, base, base.Add(48*time.Hour), true),
		pr("github.com:pr:acme/widgets#3", 3, base.Add(-24*time.Hour), base.Add(-1*time.Hour), true), // before window
		pr("github.com:pr:acme/widgets#4", 4, base, time.Time{}, false),                              // unmerged
	}}
	got := Throughput(pop, Window{Start: base, End: base.Add(7 * 24 * time.Hour)})
	if len(got) != 1 {
		t.Fatalf("observations = %d, want 1 per window", len(got))
	}
	if got[0].Value != 2.0 {
		t.Errorf("throughput = %v, want 2.0", got[0].Value)
	}
	if len(got[0].Evidence) != 1 || len(got[0].Evidence[0].ObjectIDs) != 2 {
		t.Errorf("evidence = %v, want one entry citing the two counted PRs", got[0].Evidence)
	}
}

// TestThroughputEmptyWindow: invalid or empty population → no observations.
func TestThroughputEmptyWindow(t *testing.T) {
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if got := Throughput(Population{}, Window{Start: base, End: base.Add(24 * time.Hour)}); len(got) != 0 {
		t.Errorf("empty population observations = %d, want 0", len(got))
	}
	if got := Throughput(Population{PullRequests: []Entity[ontology.PullRequest]{
		pr("github.com:pr:acme/widgets#1", 1, base, base.Add(time.Hour), true),
	}}, Window{Start: base.Add(24 * time.Hour), End: base}); len(got) != 0 {
		t.Errorf("inverted window observations = %d, want 0", len(got))
	}
}

// TestDefinitionsDeclared: every implemented metric has a §16 definition.
func TestDefinitionsDeclared(t *testing.T) {
	for _, d := range []Definition{DefCycleTime, DefReviewLatency, DefThroughput} {
		if d.Name == "" || d.Formula == "" || d.Population == "" || d.Window == "" {
			t.Errorf("incomplete definition: %+v", d)
		}
	}
}
