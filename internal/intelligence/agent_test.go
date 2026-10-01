// Evidence-backed agent tests (§29): natural language in, evidence-
// referenced answer out. Classification routes the supported question to
// the deterministic engine; unknown questions get an honest refusal; every
// cited object resolves back through the store — the agent never fabricates
// evidence.
package intelligence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge/aikoqltest"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// seedAgentWorld seeds the §23 shape with merged PRs only: three 2d-cycle
// PRs in the previous period, two 4d-cycle PRs in the current one.
func seedAgentWorld(t *testing.T, store knowledge.KnowledgeStore) {
	t.Helper()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})

	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		merged := aug.Add(time.Duration(i+3) * 24 * time.Hour)
		prKO := upsert(t, store, mapKO(t, ontology.PullRequest{
			Repository: "acme/widgets", Number: i + 1, Merged: true,
			CreatedAt: merged.Add(-2 * 24 * time.Hour), MergedAt: merged,
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: prKO.Koid, To: repo.Koid})
	}
	sep := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		merged := sep.Add(time.Duration(i+3) * 24 * time.Hour)
		prKO := upsert(t, store, mapKO(t, ontology.PullRequest{
			Repository: "acme/widgets", Number: i + 4, Merged: true,
			CreatedAt: merged.Add(-4 * 24 * time.Hour), MergedAt: merged,
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: prKO.Koid, To: repo.Koid})
	}
}

func agentQuery(text string) Query {
	return Query{
		Text:  text,
		Scope: ontology.OrgExternalID("acme"),
		From:  time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		To:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestAskCycleTimeChange: the supported question through classification,
// derived comparison window, deterministic engine — and evidence references
// that resolve back through the store.
func TestAskCycleTimeChange(t *testing.T) {
	store := knowledge.NewMemory()
	seedAgentWorld(t, store)
	ans, err := Ask(context.Background(), store, agentQuery("Why did cycle time change?"))
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if ans.Class != "cycle_time_change" {
		t.Errorf("class = %q, want cycle_time_change", ans.Class)
	}
	// Milestone F exit contract (item 47): every answer carries its time
	// window, population (scope) and epistemic state alongside the claim.
	if ans.Scope != ontology.OrgExternalID("acme") || ans.EpistemicState != "CALCULATED" {
		t.Errorf("scope/state = %q/%q, want acme org and CALCULATED", ans.Scope, ans.EpistemicState)
	}
	if !ans.From.Equal(agentQuery("").From) || !ans.To.Equal(agentQuery("").To) {
		t.Errorf("window = %v..%v, want the queried period", ans.From, ans.To)
	}
	want := "Cycle time increased from 2.0 to 4.0 days. " +
		"Throughput decreased by 33.3%. " +
		"The data supports an association, but does not establish causality."
	if ans.Statement != want {
		t.Errorf("statement = %q\nwant       %q", ans.Statement, want)
	}
	if len(ans.Evidence) == 0 {
		t.Fatal("no evidence references — the answer must cite its objects (§29)")
	}
	for _, ev := range ans.Evidence {
		for _, id := range ev.ObjectIDs {
			if _, err := store.GetByExternalID(context.Background(), id); err != nil {
				t.Errorf("cited object %s does not exist in the store: %v — fabricated evidence", id, err)
			}
		}
	}
	if len(ans.Limitations) == 0 {
		t.Error("limitations missing")
	}
}

// TestAskClassification: recognized phrasings route to their engine; anything
// else is an honest refusal with no evidence — never an invented answer.
func TestAskClassification(t *testing.T) {
	store := knowledge.NewMemory()
	seedAgentWorld(t, store)

	cases := map[string]string{
		"why is my cycle time higher?":                      "cycle_time_change",
		"CYCLE TIME SLOWER??":                               "cycle_time_change",
		"cycle time changed":                                "cycle_time_change",
		"is cycle time worse now?":                          "cycle_time_change",
		"Why did CI quality change?":                        "ci_quality_change",
		"did CI quality change?":                            "ci_quality_change",
		"What changed after AI adoption increased?":         "ai_adoption_change",
		"What is associated with increased review latency?": "review_latency_change",
		"did review latency change?":                        "review_latency_change",
		"Where are agent tasks failing?":                    "task_failures",
		"which tasks failed?":                               "task_failures",
	}
	for text, want := range cases {
		ans, err := Ask(context.Background(), store, agentQuery(text))
		if err != nil {
			t.Fatalf("ask %q: %v", text, err)
		}
		if ans.Class != want {
			t.Errorf("ask %q classified as %q, want %s", text, ans.Class, want)
		}
	}

	for _, v := range []string{"why is the sky blue?", "how many engineers do we have?", "did deployments change?"} {
		ans, err := Ask(context.Background(), store, agentQuery(v))
		if err != nil {
			t.Fatalf("ask %q: %v", v, err)
		}
		if ans.Class != "unsupported" {
			t.Errorf("ask %q classified as %q, want unsupported", v, ans.Class)
		}
		if !strings.Contains(ans.Statement, "I can only answer") {
			t.Errorf("refusal = %q, want the supported question listed", ans.Statement)
		}
		if len(ans.Evidence) != 0 {
			t.Errorf("refusal carries evidence: %v", ans.Evidence)
		}
		if ans.EpistemicState != "UNKNOWN" {
			t.Errorf("refusal epistemic state = %q, want UNKNOWN — nothing was computed", ans.EpistemicState)
		}
	}
}

// TestAskUnknownScope: a scope the store does not know is an error, not a
// fabricated answer.
func TestAskUnknownScope(t *testing.T) {
	store := knowledge.NewMemory()
	q := agentQuery("Why did cycle time change?")
	q.Scope = "github.com:org:nope"
	if _, err := Ask(context.Background(), store, q); err == nil {
		t.Fatal("unknown scope returned no error")
	}
}

// seedAgentCIWorld is seedAgentWorld plus the CI dimension: each Month A PR
// gets a +1d review, each Month B PR a +2d review, and the repo's builds run
// 3:1 pass in August and 1:3 pass in September.
func seedAgentCIWorld(t *testing.T, store knowledge.KnowledgeStore) {
	t.Helper()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})

	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		created := aug.Add(time.Duration(i+1) * 24 * time.Hour)
		prKO := upsert(t, store, mapKO(t, ontology.PullRequest{
			Repository: "acme/widgets", Number: i + 1, Merged: true,
			CreatedAt: created, MergedAt: aug.Add(time.Duration(i+3) * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: prKO.Koid, To: repo.Koid})
		revKO := upsert(t, store, mapKO(t, ontology.Review{
			Repository: "acme/widgets", PRNumber: i + 1, ID: int64(100 + i),
			SubmittedAt: created.Add(24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsReview), From: prKO.Koid, To: revKO.Koid})
	}
	sep := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		created := sep.Add(time.Duration(i+1) * 24 * time.Hour)
		prKO := upsert(t, store, mapKO(t, ontology.PullRequest{
			Repository: "acme/widgets", Number: i + 4, Merged: true,
			CreatedAt: created, MergedAt: sep.Add(time.Duration(i+3) * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: prKO.Koid, To: repo.Koid})
		revKO := upsert(t, store, mapKO(t, ontology.Review{
			Repository: "acme/widgets", PRNumber: i + 4, ID: int64(104 + i),
			SubmittedAt: created.Add(2 * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsReview), From: prKO.Koid, To: revKO.Koid})
	}
	for i := 0; i < 3; i++ {
		bKO := upsert(t, store, mapKO(t, ontology.Build{
			Repository: "acme/widgets", ID: int64(i + 1), Conclusion: "success",
			CompletedAt: aug.Add(time.Duration(2+i) * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsBuild), From: repo.Koid, To: bKO.Koid})
	}
	bKO := upsert(t, store, mapKO(t, ontology.Build{
		Repository: "acme/widgets", ID: 4, Conclusion: "failure",
		CompletedAt: aug.Add(5 * 24 * time.Hour),
	}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsBuild), From: repo.Koid, To: bKO.Koid})
	for i := 0; i < 3; i++ {
		bKO := upsert(t, store, mapKO(t, ontology.Build{
			Repository: "acme/widgets", ID: int64(5 + i), Conclusion: "failure",
			CompletedAt: sep.Add(time.Duration(2+i) * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsBuild), From: repo.Koid, To: bKO.Koid})
	}
	bKO = upsert(t, store, mapKO(t, ontology.Build{
		Repository: "acme/widgets", ID: 8, Conclusion: "success",
		CompletedAt: sep.Add(5 * 24 * time.Hour),
	}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsBuild), From: repo.Koid, To: bKO.Koid})
}

// TestAskCIDispatch (Milestone F, item 44): a CI-quality question classifies
// to its engine and the answer runs the shared two-window engine with
// ci_pass_rate as primary — evidence resolves back through the store.
func TestAskCIDispatch(t *testing.T) {
	store := knowledge.NewMemory()
	seedAgentCIWorld(t, store)
	ans, err := Ask(context.Background(), store, agentQuery("Why did CI quality change?"))
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if ans.Class != "ci_quality_change" {
		t.Errorf("class = %q, want ci_quality_change", ans.Class)
	}
	if !strings.Contains(ans.Statement, "CI pass rate decreased from 75.0 to 25.0 %.") {
		t.Errorf("statement = %q, want the CI pass rate primary", ans.Statement)
	}
	if len(ans.Evidence) == 0 {
		t.Fatal("no evidence references")
	}
	for _, ev := range ans.Evidence {
		for _, id := range ev.ObjectIDs {
			if _, err := store.GetByExternalID(context.Background(), id); err != nil {
				t.Errorf("cited object %s does not exist in the store: %v — fabricated evidence", id, err)
			}
		}
	}
	if len(ans.Limitations) == 0 {
		t.Error("limitations missing")
	}
}

// TestAskTaskFailures (Milestone F, item 46): "Where are agent tasks
// failing?" reports the window's failed tasks grouped by session, with
// evidence citing the task and session objects themselves.
func TestAskTaskFailures(t *testing.T) {
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
	task := upsert(t, store, mapKO(t, ontology.AgentTask{Source: "claude-code", ID: "toolu_1", Status: "failed", CompletedAt: sep(3)}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsTask), From: session.Koid, To: task.Koid})
	contrib := upsert(t, store, mapKO(t, ontology.CodeContribution{
		Source: "claude-code", ID: "c1", Repository: "acme/widgets", PRNumber: 1,
		Session:     ontology.SessionExternalID("claude-code", "s1"),
		Attribution: ontology.Attribution{Level: ontology.AttributionDirect, Source: "claude-code", Evidence: "session:s1"},
	}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelAIContributes), From: contrib.Koid, To: pr.Koid})
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelAuthored), From: task.Koid, To: contrib.Koid})

	ans, err := Ask(context.Background(), store, agentQuery("Where are agent tasks failing?"))
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if ans.Class != "task_failures" {
		t.Errorf("class = %q, want task_failures", ans.Class)
	}
	if ans.Scope != ontology.OrgExternalID("acme") || ans.EpistemicState != "OBSERVED" {
		t.Errorf("scope/state = %q/%q, want acme org and OBSERVED (raw object report)", ans.Scope, ans.EpistemicState)
	}
	want := "1 agent task failed in the period across 1 session: " +
		"ei.com:coding-session:claude-code:s1: 1 (ei.com:agent-task:claude-code:toolu_1)."
	if ans.Statement != want {
		t.Errorf("statement = %q\nwant       %q", ans.Statement, want)
	}
	if len(ans.Evidence) != 2 {
		t.Fatalf("evidence = %d entries, want 2 (one task, one session)", len(ans.Evidence))
	}
	types := map[string]bool{}
	for _, ev := range ans.Evidence {
		types[ev.Type] = true
		for _, id := range ev.ObjectIDs {
			if _, err := store.GetByExternalID(context.Background(), id); err != nil {
				t.Errorf("cited object %s does not exist in the store: %v — fabricated evidence", id, err)
			}
		}
	}
	if !types["AgentTask"] || !types["CodingSession"] {
		t.Errorf("evidence types = %v, want AgentTask + CodingSession", types)
	}
	if len(ans.Limitations) == 0 {
		t.Error("limitations missing")
	}
}

const askBody = `{"question":"Why did cycle time change?","scope":"github.com:org:acme",` +
	`"from":"2026-09-01T00:00:00Z","to":"2026-10-01T00:00:00Z"}`

// TestAskHTTP: the agent over the API — POST /ask returns the structured,
// evidence-referenced answer.
func TestAskHTTP(t *testing.T) {
	store := knowledge.NewMemory()
	seedAgentWorld(t, store)
	api := NewAPI(store)

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader(askBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /ask = %d, body %s", rec.Code, rec.Body.String())
	}
	var ans Answer
	if err := json.Unmarshal(rec.Body.Bytes(), &ans); err != nil {
		t.Fatal(err)
	}
	if ans.Class != "cycle_time_change" || len(ans.Evidence) == 0 {
		t.Errorf("answer = %+v", ans)
	}

	for _, body := range []string{
		`{`, // malformed
		`{"question":"","scope":"github.com:org:acme","from":"2026-09-01T00:00:00Z","to":"2026-10-01T00:00:00Z"}`,                           // no question
		`{"question":"q","scope":"github.com:org:acme","from":"nope","to":"2026-10-01T00:00:00Z"}`,                                          // bad time
		`{"question":"Why did cycle time change?","scope":"github.com:org:nope","from":"2026-09-01T00:00:00Z","to":"2026-10-01T00:00:00Z"}`, // unknown scope
		`{"question":"q","scope":"github.com:org:acme","from":"2026-10-01T00:00:00Z","to":"2026-09-01T00:00:00Z"}`,                          // inverted
	} {
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ask", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /ask %s = %d, want 400", body, rec.Code)
		}
	}
}

// TestAskLiveAikoql: the agent end to end against the real store.
func TestAskLiveAikoql(t *testing.T) {
	store := aikoqltest.Live(t)
	seedAgentWorld(t, store)
	ans, err := Ask(context.Background(), store, agentQuery("Why did cycle time change?"))
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if ans.Class != "cycle_time_change" || !strings.Contains(ans.Statement, "Cycle time increased from 2.0 to 4.0 days.") {
		t.Errorf("answer = %+v", ans)
	}
	if len(ans.Evidence) == 0 {
		t.Error("no evidence references")
	}
}
