// CI metrics tests: deterministic over a Population's Builds — pass rate is
// success / verdict runs * 100, anchored at completed_at (§19), with honest
// absence when no completed run carries a verdict.
package metrics

import (
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

var ciWin = Window{Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}

func build(extID string, completed time.Time, conclusion string) Entity[ontology.Build] {
	return Entity[ontology.Build]{
		ExternalID: extID,
		Value:      ontology.Build{Repository: "acme/widgets", Conclusion: conclusion, CompletedAt: completed},
	}
}

// TestCIPassRateExact: two successes against four verdict runs (success,
// failure, timed_out count; cancelled is no verdict) — 50.0, with every
// counted build cited in evidence.
func TestCIPassRateExact(t *testing.T) {
	pop := Population{Builds: []Entity[ontology.Build]{
		build("b1", time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), "success"),
		build("b2", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), "success"),
		build("b3", time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), "failure"),
		build("b4", time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), "timed_out"),
		build("b5", time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), "cancelled"),
	}}
	got := CIPassRate(pop, ciWin)
	oneValue(t, got, 50.0)
	if len(got[0].Evidence) != 1 || len(got[0].Evidence[0].ObjectIDs) != 4 {
		t.Errorf("evidence = %v, want the 4 verdict builds", got[0].Evidence)
	}
}

// TestCIPassRateExcludesNoVerdict: in-progress runs (no completed_at), a
// success without a completion time, and a completed run with no conclusion
// never count — only the one completed success remains, so the rate is 100.0.
func TestCIPassRateExcludesNoVerdict(t *testing.T) {
	pop := Population{Builds: []Entity[ontology.Build]{
		build("b1", time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), "success"),
		build("b2", time.Time{}, "success"),                          // no completion time
		build("b3", time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), ""), // completed, no conclusion
		{ExternalID: "b4", Value: ontology.Build{Repository: "acme/widgets", Status: "in_progress"}},
	}}
	got := CIPassRate(pop, ciWin)
	oneValue(t, got, 100.0)
	if len(got[0].Evidence[0].ObjectIDs) != 1 {
		t.Errorf("evidence = %v, want only the counted success", got[0].Evidence)
	}
}

func TestCIPassRateAbsence(t *testing.T) {
	none(t, CIPassRate(Population{}, ciWin)) // empty population
	none(t, CIPassRate(Population{Builds: []Entity[ontology.Build]{
		build("b1", time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), "success"), // completed outside window
		build("b2", time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), "cancelled"),
	}}, ciWin))
}
