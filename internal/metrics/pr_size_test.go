// PR size tests: deterministic over a Population's PullRequests — mean of
// additions+deletions per merged PR, anchored at merged_at (§19), with
// honest absence when no merged PR carries a diff size.
package metrics

import (
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

func prEntity(extID string, created, mergedAt time.Time, merged bool, additions, deletions int) Entity[ontology.PullRequest] {
	return Entity[ontology.PullRequest]{
		ExternalID: extID,
		Value: ontology.PullRequest{
			Repository: "acme/widgets", Merged: merged, CreatedAt: created, MergedAt: mergedAt,
			Additions: additions, Deletions: deletions,
		},
	}
}

// TestPRSizeExact: two merged PRs with diff sizes (150 and 30 lines) in the
// window — mean 90, both cited in evidence; a third merged PR with no diff
// size and an unmerged PR never count.
func TestPRSizeExact(t *testing.T) {
	pop := Population{PullRequests: []Entity[ontology.PullRequest]{
		prEntity("p1", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), true, 100, 50),
		prEntity("p2", time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), true, 20, 10),
		prEntity("p3", time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), true, 0, 0), // no diff size
		prEntity("p4", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), false, 400, 400),
	}}
	got := PRSize(pop, ciWin)
	oneValue(t, got, 90.0)
	if len(got[0].Evidence) != 1 || len(got[0].Evidence[0].ObjectIDs) != 2 {
		t.Errorf("evidence = %v, want the 2 sized merged PRs", got[0].Evidence)
	}
}

func TestPRSizeAbsence(t *testing.T) {
	none(t, PRSize(Population{}, ciWin)) // empty population
	none(t, PRSize(Population{PullRequests: []Entity[ontology.PullRequest]{
		prEntity("p1", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), true, 0, 0),     // no diff size
		prEntity("p2", time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), true, 10, 10), // merged outside window
		prEntity("p3", time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), false, 10, 10),  // unmerged
	}}, ciWin))
}
