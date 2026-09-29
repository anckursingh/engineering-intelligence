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
// AI contribution, and the repo one completed CI build.
func seedWorld(t *testing.T, store knowledge.KnowledgeStore) {
	t.Helper()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})
	buildKO := upsert(t, store, mapKO(t, ontology.Build{
		Repository: "acme/widgets", ID: 200, Name: "CI", HeadSHA: "abc",
		Conclusion: "success", Status: "completed",
		CompletedAt: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
	}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsBuild), From: repo.Koid, To: buildKO.Koid})

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
	if len(pop.Builds) != 1 {
		t.Fatalf("population = %d builds, want 1", len(pop.Builds))
	}
	if b := pop.Builds[0]; b.Value.Conclusion != "success" || b.Value.CompletedAt.IsZero() {
		t.Errorf("build did not round-trip: %+v", b)
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

// TestPopulationCollectsTelemetry: the walk reaches the session's
// interactions, runs and tasks through the contribution's producing task —
// contribution → AUTHORED inbound → task → CONTAINS_TASK inbound → session →
// children. The path that feeds the board's AI workflow metrics (§27).
func TestPopulationCollectsTelemetry(t *testing.T) {
	store := knowledge.NewMemory()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	sep := func(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC) }
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})
	pr := upsert(t, store, mapKO(t, ontology.PullRequest{
		Repository: "acme/widgets", Number: 1, Merged: true, CreatedAt: sep(1), MergedAt: sep(5),
	}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: pr.Koid, To: repo.Koid})

	session := upsert(t, store, mapKO(t, ontology.CodingSession{Source: "claude-code", SessionID: "s1", StartedAt: sep(1)}.KnowledgeObject, prov))
	i1 := upsert(t, store, mapKO(t, ontology.Interaction{Source: "claude-code", ID: "i1", StartedAt: sep(2)}.KnowledgeObject, prov))
	r1 := upsert(t, store, mapKO(t, ontology.AgentRun{Source: "claude-code", ID: "r1", StartedAt: sep(2), Status: "completed", CostUSD: 1.5}.KnowledgeObject, prov))
	t1 := upsert(t, store, mapKO(t, ontology.AgentTask{Source: "claude-code", ID: "t1", Status: "completed", CompletedAt: sep(3)}.KnowledgeObject, prov))
	t2 := upsert(t, store, mapKO(t, ontology.AgentTask{Source: "claude-code", ID: "t2", Status: "failed", Retries: 1, CompletedAt: sep(4)}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsInteraction), From: session.Koid, To: i1.Koid})
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsRun), From: session.Koid, To: r1.Koid})
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsTask), From: session.Koid, To: t1.Koid})
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsTask), From: session.Koid, To: t2.Koid})
	contrib := upsert(t, store, mapKO(t, ontology.CodeContribution{
		Source: "claude-code", ID: "c1", Repository: "acme/widgets", PRNumber: 1,
		Attribution: ontology.Attribution{Level: ontology.AttributionDirect, Source: "claude-code", Evidence: "session:s1"},
	}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelAIContributes), From: contrib.Koid, To: pr.Koid})
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelAuthored), From: t1.Koid, To: contrib.Koid})

	pop, err := Population(context.Background(), store, ontology.OrgExternalID("acme"))
	if err != nil {
		t.Fatalf("population: %v", err)
	}
	if len(pop.Interactions) != 1 || len(pop.AgentRuns) != 1 || len(pop.AgentTasks) != 2 || len(pop.CodeContributions) != 1 {
		t.Fatalf("population = %d interactions, %d runs, %d tasks, %d contributions; want 1/1/2/1",
			len(pop.Interactions), len(pop.AgentRuns), len(pop.AgentTasks), len(pop.CodeContributions))
	}
}

// TestPopulationFromUserScope: a user account scope root walks exactly like
// an org one — the graph shape is identical, only the root type differs.
func TestPopulationFromUserScope(t *testing.T) {
	store := knowledge.NewMemory()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	user := upsert(t, store, mapKO(t, ontology.User{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: user.Koid})

	created := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	prKO := upsert(t, store, mapKO(t, ontology.PullRequest{
		Repository: "acme/widgets", Number: 1, Merged: true,
		CreatedAt: created, MergedAt: created.Add(4 * 24 * time.Hour),
	}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: prKO.Koid, To: repo.Koid})

	pop, err := Population(context.Background(), store, ontology.AccountExternalID("acme"))
	if err != nil {
		t.Fatalf("population: %v", err)
	}
	if len(pop.PullRequests) != 1 {
		t.Errorf("population = %d PRs, want 1", len(pop.PullRequests))
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
