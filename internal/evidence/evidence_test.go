// Package evidence_test pins the §20-21 contract: the generic evidence
// representation, the epistemic state hierarchy, and — the core requirement —
// that an explanation RECONSTRUCTS from persisted source objects and
// deterministic calculations. No generated explanation text is the source of
// truth anywhere.
package evidence_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// TestStatesDistinctAndOrdered pins §21: the four epistemic states never
// collapse, and they order by how directly each rests on data.
func TestStatesDistinctAndOrdered(t *testing.T) {
	states := []evidence.State{
		evidence.StateObserved,
		evidence.StateCalculated,
		evidence.StateInferred,
		evidence.StateHypothesized,
	}
	seen := map[evidence.State]bool{}
	for i, s := range states {
		if seen[s] {
			t.Errorf("state %q duplicated — states collapsed", s)
		}
		seen[s] = true
		if got := s.Strength(); got != i {
			t.Errorf("%s strength = %d, want %d (observed < calculated < inferred < hypothesized)", s, got, i)
		}
	}
}

// TestEvidenceHoldsAllFields pins §20: the generic representation carries
// type, source, source_url, object_ids, observed_at and confidence.
func TestEvidenceHoldsAllFields(t *testing.T) {
	e := evidence.Evidence{
		Type:       "PullRequest",
		Source:     "github",
		SourceURL:  "https://github.com/acme/widgets/pull/1",
		ObjectIDs:  []string{"github.com:pr:acme/widgets#1"},
		ObservedAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
		Confidence: 1.0,
		State:      evidence.StateObserved,
	}
	if e.Type != "PullRequest" || e.Source != "github" || e.SourceURL == "" ||
		len(e.ObjectIDs) != 1 || e.ObservedAt.IsZero() || e.Confidence != 1.0 || e.State != evidence.StateObserved {
		t.Errorf("evidence lost a field: %+v", e)
	}
}

// TestExplanationReconstructs pins §20's core rule: a calculated insight is
// explained by re-reading its cited objects from the store and re-running the
// deterministic calculation — same value, every time, no stored prose.
func TestExplanationReconstructs(t *testing.T) {
	created := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	merged := time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC)
	prOnt := ontology.PullRequest{
		Repository: "acme/widgets", Number: 1, Merged: true,
		CreatedAt: created, MergedAt: merged,
	}
	prov := ontology.NewProvenance("https://github.com/acme/widgets/pull/1", merged)

	// The source object is persisted first — the OBSERVED half of the story.
	store := knowledge.NewMemory()
	ko, err := prOnt.KnowledgeObject(prov)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(context.Background(), ko); err != nil {
		t.Fatal(err)
	}

	// The metric over that object — the CALCULATED half.
	w := metrics.Window{Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)}
	pop := metrics.Population{PullRequests: []metrics.Entity[ontology.PullRequest]{
		{ExternalID: ko.ExternalID, Value: prOnt},
	}}
	obs := metrics.CycleTime(pop, w)
	if len(obs) != 1 {
		t.Fatalf("cycle time observations = %d, want 1", len(obs))
	}

	// Reconstruction: read every cited object back out of the store and
	// recompute. The value must be identical — the explanation never depends
	// on stored text, only on the objects the evidence names.
	var recovered metrics.Population
	for _, ev := range obs[0].Evidence {
		if ev.State != evidence.StateCalculated {
			t.Fatalf("calculated insight carries state %q, want CALCULATED", ev.State)
		}
		for _, id := range ev.ObjectIDs {
			stored, err := store.GetByExternalID(context.Background(), id)
			if err != nil {
				t.Fatalf("evidence cites unresolvable object %s: %v", id, err)
			}
			props, err := json.Marshal(stored.Properties)
			if err != nil {
				t.Fatal(err)
			}
			var pr ontology.PullRequest
			if err := json.Unmarshal(props, &pr); err != nil {
				t.Fatal(err)
			}
			recovered.PullRequests = append(recovered.PullRequests,
				metrics.Entity[ontology.PullRequest]{ExternalID: id, Value: pr})
		}
	}
	if len(recovered.PullRequests) != 1 {
		t.Fatalf("reconstructed %d objects, want 1", len(recovered.PullRequests))
	}
	again := metrics.CycleTime(recovered, w)
	if len(again) != 1 || again[0].Value != obs[0].Value {
		t.Errorf("reconstructed value = %v, want %v — explanation did not reconstruct", again, obs[0].Value)
	}
}
