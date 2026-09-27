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

// TestAskClassification: recognized phrasings route to the engine; anything
// else is an honest refusal with no evidence — never an invented answer.
func TestAskClassification(t *testing.T) {
	store := knowledge.NewMemory()
	seedAgentWorld(t, store)

	for _, v := range []string{"why is my cycle time higher?", "CYCLE TIME SLOWER??", "cycle time changed", "is cycle time worse now?"} {
		ans, err := Ask(context.Background(), store, agentQuery(v))
		if err != nil {
			t.Fatalf("ask %q: %v", v, err)
		}
		if ans.Class != "cycle_time_change" {
			t.Errorf("ask %q classified as %q, want cycle_time_change", v, ans.Class)
		}
	}

	for _, v := range []string{"why is the sky blue?", "how many engineers do we have?", "did review latency change?"} {
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
