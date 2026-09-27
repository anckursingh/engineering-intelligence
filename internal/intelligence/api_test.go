// Investigation API tests (§24): the HTTP surface returns structured
// evidence — the doc's example JSON shape (metric/window/value/comparison/
// change/epistemic_state/evidence) — not only prose.
package intelligence

import (
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

// seedSection seeds one window's worth of the §23 fixture under repo: `count`
// PRs numbered from numStart with `cycleDays` cycle each, one review per PR
// `reviewDelay` days in. Sections must use disjoint PR numbers — external
// IDs collide otherwise and upserts would overwrite the other window.
func seedSection(t *testing.T, store knowledge.KnowledgeStore, repoKO knowledge.KnowledgeObject, start time.Time, cycleDays, reviewDelay, count, numStart int) {
	t.Helper()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	for i := 0; i < count; i++ {
		num := numStart + i
		created := start.Add(time.Duration(i+1) * 24 * time.Hour)
		prKO := upsert(t, store, mapKO(t, ontology.PullRequest{
			Repository: "acme/widgets", Number: num, Merged: true,
			CreatedAt: created, MergedAt: created.Add(time.Duration(cycleDays) * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: prKO.Koid, To: repoKO.Koid})
		revKO := upsert(t, store, mapKO(t, ontology.Review{
			Repository: "acme/widgets", PRNumber: num, ID: int64(1000 + num),
			SubmittedAt: created.Add(time.Duration(reviewDelay) * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsReview), From: prKO.Koid, To: revKO.Koid})
	}
}

// seedInvestigationWorld is the §23 fixture as a knowledge graph: Month A
// three PRs at 2d cycle with +1d reviews; Month B two PRs at 4d cycle with
// +2d reviews.
func seedInvestigationWorld(t *testing.T, store knowledge.KnowledgeStore) {
	t.Helper()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})

	seedSection(t, store, repo, time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), 2, 1, 3, 1)
	seedSection(t, store, repo, time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), 4, 2, 2, 4)
}

const investigationBody = `{"scope":"github.com:org:acme",` +
	`"window_a":{"name":"Month A","start":"2026-08-01T00:00:00Z","end":"2026-09-01T00:00:00Z"},` +
	`"window_b":{"name":"Month B","start":"2026-09-01T00:00:00Z","end":"2026-10-01T00:00:00Z"}}`

const wantStatement = "Cycle time increased from 2.0 to 4.0 days. " +
	"Review latency increased by 100.0%. " +
	"Throughput decreased by 33.3%. " +
	"The data supports an association, but does not establish causality."

func TestAPIInvestigationEndpoints(t *testing.T) {
	store := knowledge.NewMemory()
	seedInvestigationWorld(t, store)
	api := NewAPI(store)

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/investigations", strings.NewReader(investigationBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /investigations = %d, body %s", rec.Code, rec.Body.String())
	}
	var got InvestigationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Statement != wantStatement {
		t.Errorf("statement = %q\nwant       %q", got.Statement, wantStatement)
	}
	if got.ID == "" || got.Question != QuestionCycleTime {
		t.Errorf("id/question = %q/%q", got.ID, got.Question)
	}
	// §24's structured shape: metric/window/value/comparison/change/
	// epistemic_state/evidence.
	if got.Primary.Metric != "cycle_time" || got.Primary.Window != "Month B" ||
		got.Primary.Value != 4.0 || got.Primary.Comparison != 2.0 || got.Primary.Change != 1.0 {
		t.Errorf("primary = %+v, want cycle_time/Month B/4.0/2.0/1.0", got.Primary)
	}
	if got.Primary.EpistemicState != "CALCULATED" || len(got.Primary.Evidence) == 0 {
		t.Errorf("primary epistemic/evidence = %q/%d — structured evidence required (§24)", got.Primary.EpistemicState, len(got.Primary.Evidence))
	}
	if len(got.Factors) != 2 {
		t.Errorf("factors = %d, want 2", len(got.Factors))
	}

	// GET /investigations/{id} returns the same result.
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/investigations/"+got.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /investigations/{id} = %d", rec.Code)
	}
	var again InvestigationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &again); err != nil {
		t.Fatal(err)
	}
	if again.Statement != got.Statement || again.ID != got.ID {
		t.Errorf("stored investigation did not match")
	}

	// Unknown id → 404.
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/investigations/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET unknown investigation = %d, want 404", rec.Code)
	}
}

func TestAPIMetricsAndHealth(t *testing.T) {
	api := NewAPI(knowledge.NewMemory())

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /health = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d", rec.Code)
	}
	var defs []metricDefinition
	if err := json.Unmarshal(rec.Body.Bytes(), &defs); err != nil {
		t.Fatal(err)
	}
	if len(defs) != 4 || defs[0].Name != "cycle_time" {
		t.Errorf("definitions = %+v, want the 3 flow metrics + ai_assisted_pr_pct", defs)
	}

	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics/cycle_time", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("GET /metrics/cycle_time = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /metrics/nope = %d, want 404", rec.Code)
	}
}

func TestAPIBadRequests(t *testing.T) {
	api := NewAPI(knowledge.NewMemory())

	cases := []struct {
		name string
		body string
	}{
		{"malformed json", `{`},
		{"bad window time", `{"scope":"github.com:org:acme","window_a":{"name":"A","start":"nope","end":"2026-09-01T00:00:00Z"},"window_b":{"name":"B","start":"2026-09-01T00:00:00Z","end":"2026-10-01T00:00:00Z"}}`},
		{"missing scope", `{"window_a":{"name":"A","start":"2026-08-01T00:00:00Z","end":"2026-09-01T00:00:00Z"},"window_b":{"name":"B","start":"2026-09-01T00:00:00Z","end":"2026-10-01T00:00:00Z"}}`},
		{"unsupported question", `{"question":"why is the sky blue?","scope":"github.com:org:acme","window_a":{"name":"A","start":"2026-08-01T00:00:00Z","end":"2026-09-01T00:00:00Z"},"window_b":{"name":"B","start":"2026-09-01T00:00:00Z","end":"2026-10-01T00:00:00Z"}}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			api.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/investigations", strings.NewReader(c.body)))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("POST = %d, want 400", rec.Code)
			}
		})
	}
}

// TestAPIInvestigationLiveAikoql: the §24 flow end to end against the real
// store — seed the §23 KG, POST, the full statement comes back.
func TestAPIInvestigationLiveAikoql(t *testing.T) {
	store := aikoqltest.Live(t)
	seedInvestigationWorld(t, store)

	api := NewAPI(store)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/investigations", strings.NewReader(investigationBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, body %s", rec.Code, rec.Body.String())
	}
	var got InvestigationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Statement != wantStatement {
		t.Errorf("statement = %q\nwant       %q", got.Statement, wantStatement)
	}
}
