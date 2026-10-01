// Review cycles tests: mean count of CHANGES_REQUESTED reviews per merged,
// reviewed PR, anchored at merged_at (§19). A reviewed PR with zero
// changes_requested reviews is a real 0.0; a merged PR with no reviews at
// all is silence, never zero.
package metrics

import (
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

func rev(extID string, repo string, pr int, state string) Entity[ontology.Review] {
	return Entity[ontology.Review]{
		ExternalID: extID,
		Value:      ontology.Review{Repository: repo, PRNumber: pr, State: state},
	}
}

// TestReviewCyclesExact: p1 had 2 change-request rounds, p2 one, p3 reviews
// but none requesting changes (real 0.0) — mean 1.0; p4 merged without any
// review (silence) and p5 unmerged never count. Evidence cites the 3 PRs and
// the 3 changes_requested reviews.
func TestReviewCyclesExact(t *testing.T) {
	pop := Population{
		PullRequests: []Entity[ontology.PullRequest]{
			prEntity("p1", 1, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), true, 0, 0),
			prEntity("p2", 2, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), true, 0, 0),
			prEntity("p3", 3, time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), true, 0, 0),
			prEntity("p4", 4, time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), true, 0, 0), // no reviews
			prEntity("p5", 5, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), false, 0, 0),
		},
		Reviews: []Entity[ontology.Review]{
			rev("r1", "acme/widgets", 1, "CHANGES_REQUESTED"),
			rev("r2", "acme/widgets", 1, "CHANGES_REQUESTED"),
			rev("r3", "acme/widgets", 1, "APPROVED"),
			rev("r4", "acme/widgets", 2, "CHANGES_REQUESTED"),
			rev("r5", "acme/widgets", 3, "COMMENTED"),
		},
	}
	got := ReviewCycles(pop, ciWin)
	oneValue(t, got, 1.0)
	ev := got[0].Evidence
	if len(ev) != 2 || len(ev[0].ObjectIDs) != 3 || len(ev[1].ObjectIDs) != 3 {
		t.Errorf("evidence = %v, want 3 PRs + 3 changes_requested reviews", ev)
	}
}

func TestReviewCyclesAbsence(t *testing.T) {
	none(t, ReviewCycles(Population{}, ciWin)) // empty population
	none(t, ReviewCycles(Population{
		PullRequests: []Entity[ontology.PullRequest]{
			prEntity("p1", 1, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), true, 0, 0),   // no reviews
			prEntity("p2", 2, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC), true, 0, 0), // merged outside window
			prEntity("p3", 3, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), false, 0, 0),  // unmerged
		},
		Reviews: []Entity[ontology.Review]{rev("r1", "acme/widgets", 2, "CHANGES_REQUESTED")},
	}, ciWin))
}
