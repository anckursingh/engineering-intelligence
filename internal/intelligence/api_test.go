// Investigation API tests (§24): the HTTP surface returns structured
// evidence — the doc's example JSON shape (metric/window/value/comparison/
// change/epistemic_state/evidence) — not only prose.
package intelligence

import (
	"encoding/json"
	"fmt"
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
// +2d reviews. Returns the repo KO so callers can hang further entities
// (e.g. builds) under it.
func seedInvestigationWorld(t *testing.T, store knowledge.KnowledgeStore) knowledge.KnowledgeObject {
	t.Helper()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})

	seedSection(t, store, repo, time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC), 2, 1, 3, 1)
	seedSection(t, store, repo, time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), 4, 2, 2, 4)
	return repo
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
	if len(defs) != 7 || defs[0].Name != "cycle_time" {
		t.Errorf("definitions = %+v, want the 3 flow metrics + ai_assisted_pr_pct + ci_pass_rate + pr_size + review_cycles", defs)
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

const ciQualityBody = `{"question":"Why did CI quality change?","scope":"github.com:org:acme",` +
	`"window_a":{"name":"Month A","start":"2026-08-01T00:00:00Z","end":"2026-09-01T00:00:00Z"},` +
	`"window_b":{"name":"Month B","start":"2026-09-01T00:00:00Z","end":"2026-10-01T00:00:00Z"}}`

// TestInvestigateQuestionDispatch (Milestone F, item 44): POST /investigations
// runs the engine named by the question — CI quality compares ci_pass_rate as
// the primary, and the stored result carries the question.
func TestInvestigateQuestionDispatch(t *testing.T) {
	store := knowledge.NewMemory()
	repo := seedInvestigationWorld(t, store)
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	aug := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	for i, c := range []string{"success", "success", "success", "failure"} {
		bKO := upsert(t, store, mapKO(t, ontology.Build{
			Repository: "acme/widgets", ID: int64(i + 1), Conclusion: c,
			CompletedAt: aug.Add(time.Duration(i) * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsBuild), From: repo.Koid, To: bKO.Koid})
	}
	sep := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	for i, c := range []string{"success", "failure", "failure", "failure"} {
		bKO := upsert(t, store, mapKO(t, ontology.Build{
			Repository: "acme/widgets", ID: int64(5 + i), Conclusion: c,
			CompletedAt: sep.Add(time.Duration(i) * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsBuild), From: repo.Koid, To: bKO.Koid})
	}

	api := NewAPI(store)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/investigations", strings.NewReader(ciQualityBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, body %s", rec.Code, rec.Body.String())
	}
	var got InvestigationResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Question != QuestionCIQuality {
		t.Errorf("question = %q, want %q", got.Question, QuestionCIQuality)
	}
	if got.Primary.Metric != "ci_pass_rate" || got.Primary.Value != 25.0 || got.Primary.Comparison != 75.0 {
		t.Errorf("primary = %+v, want ci_pass_rate 25.0 vs 75.0", got.Primary)
	}
	if !strings.Contains(got.Statement, "CI pass rate decreased from 75.0 to 25.0 %.") {
		t.Errorf("statement = %q, want the CI pass rate primary", got.Statement)
	}
}

// TestAPIComparisonsEndpoint (Milestone F, item 45): POST /comparisons
// partitions the window's merged PRs by AI attribution and compares the
// PR-scoped metrics between the two populations.
func TestAPIComparisonsEndpoint(t *testing.T) {
	store := knowledge.NewMemory()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	prKOs := map[int]knowledge.KnowledgeObject{}
	for i, cycle := range []int{2, 4, 3, 6} {
		num := i + 1
		created := base.Add(time.Duration(i+1) * 24 * time.Hour)
		prKO := upsert(t, store, mapKO(t, ontology.PullRequest{
			Repository: "acme/widgets", Number: num, Merged: true,
			CreatedAt: created, MergedAt: created.Add(time.Duration(cycle) * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: prKO.Koid, To: repo.Koid})
		prKOs[num] = prKO
	}
	for _, num := range []int{1, 3} { // reviews only on the unattributed PRs
		revKO := upsert(t, store, mapKO(t, ontology.Review{
			Repository: "acme/widgets", PRNumber: num, ID: int64(100 + num),
			SubmittedAt: base.Add(time.Duration(num+1) * 24 * time.Hour),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsReview), From: prKOs[num].Koid, To: revKO.Koid})
	}
	for _, num := range []int{2, 4} { // AI contributions on the assisted PRs
		cKO := upsert(t, store, mapKO(t, ontology.CodeContribution{
			Repository: "acme/widgets", PRNumber: num,
			ID: fmt.Sprintf("toolu_c%d", num), Source: "claude-code",
			Attribution: ontology.Attribution{Level: ontology.AttributionDirect, Source: "claude-code", Evidence: "session:s1"},
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelAIContributes), From: cKO.Koid, To: prKOs[num].Koid})
	}

	api := NewAPI(store)
	body := `{"scope":"github.com:org:acme","start":"2026-09-01T00:00:00Z","end":"2026-10-01T00:00:00Z"}`
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/comparisons", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /comparisons = %d, body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Populations []struct {
			ID        string `json:"id"`
			Label     string `json:"label"`
			MergedPRs int    `json:"merged_prs"`
		} `json:"populations"`
		Metrics []PopulationComparison `json:"metrics"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Populations) != 2 || got.Populations[0].ID != "ai_assisted" || got.Populations[0].MergedPRs != 2 ||
		got.Populations[1].ID != "unattributed" || got.Populations[1].MergedPRs != 2 {
		t.Errorf("populations = %+v, want ai_assisted 2 + unattributed 2", got.Populations)
	}
	if len(got.Metrics) < 1 || got.Metrics[0].Metric != "cycle_time" {
		t.Fatalf("metrics = %+v, want cycle_time first", got.Metrics)
	}
	if got.Metrics[0].Assisted == nil || got.Metrics[0].Assisted.Value != 5.0 ||
		got.Metrics[0].Unattributed == nil || got.Metrics[0].Unattributed.Value != 2.5 {
		t.Errorf("cycle_time = %+v vs %+v, want 5.0 vs 2.5", got.Metrics[0].Assisted, got.Metrics[0].Unattributed)
	}

	// Bad window → 400.
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/comparisons", strings.NewReader(`{"scope":"github.com:org:acme","start":"nope","end":"2026-10-01T00:00:00Z"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("POST /comparisons bad start = %d, want 400", rec.Code)
	}
}
