// Population builder tests (§22 flow's "graph relationships" step): the
// investigation API builds its Population by walking the knowledge graph
// from a scope root — org → repos → PRs → reviews.
package intelligence

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge/aikoqltest"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// mapKO converts an ontology value through its KnowledgeObject method.
func mapKO(t *testing.T, fn func(knowledge.Provenance) (knowledge.KnowledgeObject, error), prov knowledge.Provenance) knowledge.KnowledgeObject {
	t.Helper()
	ko, err := fn(prov)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	return ko
}

func upsert(t *testing.T, store knowledge.KnowledgeStore, ko knowledge.KnowledgeObject) knowledge.KnowledgeObject {
	t.Helper()
	out, err := store.Upsert(context.Background(), ko)
	if err != nil {
		t.Fatalf("upsert %s: %v", ko.ExternalID, err)
	}
	return out
}

func relate(t *testing.T, store knowledge.KnowledgeStore, rel knowledge.Relationship) {
	t.Helper()
	if err := store.Relate(context.Background(), rel); err != nil {
		t.Fatalf("relate %s: %v", rel.Type, err)
	}
}

// seedWorld builds the KG shape the population builder reads: org → repo →
// two PRs (4d cycle each) → one review each; PR 1 also carries one DIRECT
// AI contribution.
func seedWorld(t *testing.T, store knowledge.KnowledgeStore) {
	t.Helper()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})

	created := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	for _, n := range []int{1, 2} {
		prKO := upsert(t, store, mapKO(t, ontology.PullRequest{
			Repository: "acme/widgets", Number: n, Merged: true,
			CreatedAt: created, MergedAt: created.Add(4 * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: prKO.Koid, To: repo.Koid})
		revKO := upsert(t, store, mapKO(t, ontology.Review{
			Repository: "acme/widgets", PRNumber: n, ID: int64(1000 + n),
			SubmittedAt: created.Add(24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsReview), From: prKO.Koid, To: revKO.Koid})
		if n == 1 {
			contribKO := upsert(t, store, mapKO(t, ontology.CodeContribution{
				Repository: "acme/widgets", PRNumber: 1,
				Attribution: ontology.Attribution{Level: ontology.AttributionDirect, Source: "claude-code", Evidence: "session:s1"},
			}.KnowledgeObject, prov))
			relate(t, store, knowledge.Relationship{Type: string(ontology.RelAIContributes), From: contribKO.Koid, To: prKO.Koid})
		}
	}
}

// TestPopulationFromScope: walking org → repos → PRs → reviews recovers the
// typed objects, external IDs intact.
func TestPopulationFromScope(t *testing.T) {
	store := knowledge.NewMemory()
	seedWorld(t, store)
	pop, err := Population(context.Background(), store, ontology.OrgExternalID("acme"))
	if err != nil {
		t.Fatalf("population: %v", err)
	}
	if len(pop.PullRequests) != 2 || len(pop.Reviews) != 2 {
		t.Fatalf("population = %d PRs, %d reviews, want 2/2", len(pop.PullRequests), len(pop.Reviews))
	}
	if len(pop.CodeContributions) != 1 {
		t.Fatalf("population = %d AI contributions, want 1", len(pop.CodeContributions))
	}
	if c := pop.CodeContributions[0]; !strings.HasPrefix(c.ExternalID, "ei.com:ai-contribution:") || c.Value.Attribution.Level != ontology.AttributionDirect {
		t.Errorf("contribution did not round-trip: %+v", c)
	}
	for _, e := range pop.PullRequests {
		if !strings.HasPrefix(e.ExternalID, "github.com:pr:acme/widgets#") {
			t.Errorf("PR external id = %q", e.ExternalID)
		}
		if !e.Value.Merged || e.Value.MergedAt.Sub(e.Value.CreatedAt) != 4*24*time.Hour {
			t.Errorf("PR %s did not round-trip: %+v", e.ExternalID, e.Value)
		}
	}
	for _, e := range pop.Reviews {
		if e.Value.SubmittedAt.IsZero() {
			t.Errorf("review %s did not round-trip: %+v", e.ExternalID, e.Value)
		}
	}
}

// TestPopulationScopeIsolation: a second org's graph never leaks into the
// first scope.
func TestPopulationScopeIsolation(t *testing.T) {
	store := knowledge.NewMemory()
	seedWorld(t, store)

	prov := ontology.NewProvenance("https://github.com/other/tools", time.Now())
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "other"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "other", Name: "tools"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})
	prKO := upsert(t, store, mapKO(t, ontology.PullRequest{
		Repository: "other/tools", Number: 7, Merged: true,
		CreatedAt: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
		MergedAt:  time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC),
	}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: prKO.Koid, To: repo.Koid})

	pop, err := Population(context.Background(), store, ontology.OrgExternalID("acme"))
	if err != nil {
		t.Fatalf("population: %v", err)
	}
	if len(pop.PullRequests) != 2 {
		t.Errorf("population = %d PRs, want 2 (other org must not leak)", len(pop.PullRequests))
	}
}

// TestPopulationLiveAikoql: the same walk against the real store.
func TestPopulationLiveAikoql(t *testing.T) {
	store := aikoqltest.Live(t)
	seedWorld(t, store)
	pop, err := Population(context.Background(), store, ontology.OrgExternalID("acme"))
	if err != nil {
		t.Fatalf("population: %v", err)
	}
	if len(pop.PullRequests) != 2 || len(pop.Reviews) != 2 {
		t.Errorf("population = %d PRs, %d reviews, want 2/2", len(pop.PullRequests), len(pop.Reviews))
	}
	if len(pop.CodeContributions) != 1 {
		t.Errorf("population = %d AI contributions, want 1", len(pop.CodeContributions))
	}
}
