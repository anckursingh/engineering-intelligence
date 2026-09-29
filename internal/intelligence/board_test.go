// Product board tests (§28): one response with the doc's five sections.
// Empty sections are honest notes, never fabricated numbers; the board
// carries no per-person data, so individual ranking is impossible by
// construction.
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

const boardURL = "/board?scope=github.com:org:acme&start=2026-09-01T00:00:00Z&end=2026-10-01T00:00:00Z"

func TestBoardSections(t *testing.T) {
	store := knowledge.NewMemory()
	seedInvestigationWorld(t, store)
	api := NewAPI(store)

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /board = %d, body %s", rec.Code, rec.Body.String())
	}
	var got board
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// §28's five sections, in doc order.
	wantTitles := []string{"Engineering Flow", "Quality", "Reliability", "AI Development", "Evidence Coverage"}
	if len(got.Sections) != len(wantTitles) {
		t.Fatalf("sections = %d, want %d", len(got.Sections), len(wantTitles))
	}
	for i, s := range got.Sections {
		if s.Title != wantTitles[i] {
			t.Errorf("section %d = %q, want %q", i, s.Title, wantTitles[i])
		}
	}

	// Month B of the §23 fixture: cycle time 4.0, review latency 2.0,
	// throughput 2 — each with calculated evidence.
	flow := got.Sections[0]
	if len(flow.Items) != 3 {
		t.Fatalf("flow items = %d, want 3", len(flow.Items))
	}
	byMetric := map[string]boardItem{}
	for _, it := range flow.Items {
		byMetric[it.Metric] = it
		if it.EpistemicState != "CALCULATED" || len(it.Evidence) == 0 {
			t.Errorf("flow item %s lacks evidence: %+v", it.Metric, it)
		}
	}
	if byMetric["cycle_time"].Value != 4.0 || byMetric["review_latency"].Value != 2.0 || byMetric["throughput"].Value != 2.0 {
		t.Errorf("flow values = %+v, want cycle_time 4.0, review_latency 2.0, throughput 2.0", byMetric)
	}

	// Empty sections are honest notes, not fabricated data.
	for _, i := range []int{1, 2} {
		if len(got.Sections[i].Items) != 0 || got.Sections[i].Note == "" {
			t.Errorf("section %q = %+v, want empty items + a note", got.Sections[i].Title, got.Sections[i])
		}
	}

	// AI Development: the §23 fixture has no contributions, but merged PRs
	// exist — 0.0 is a real zero, not absence.
	ai := got.Sections[3]
	if len(ai.Items) != 1 || ai.Items[0].Metric != "ai_assisted_pr_pct" || ai.Items[0].Value != 0.0 {
		t.Errorf("AI section = %+v, want ai_assisted_pr_pct 0.0", ai)
	}

	// Evidence Coverage: calculated evidence with objects behind it.
	cov := got.Sections[4]
	if len(cov.Items) == 0 {
		t.Fatalf("evidence coverage = %+v, want items", cov)
	}
	found := false
	for _, it := range cov.Items {
		if it.Value <= 0 {
			t.Errorf("coverage item %+v cites no objects", it)
		}
		if it.Label == "CALCULATED" {
			found = true
		}
	}
	if !found {
		t.Errorf("coverage lacks CALCULATED: %+v", cov.Items)
	}
}

// TestBoardNoIndividualRanking: §28 forbids ranking developers — the board
// carries no per-person data at all. (Structural check: no Engineer evidence
// types, no identity keys, no ranking fields. The word "Engineering" would
// trip a substring check on the doc's own section title.)
func TestBoardNoIndividualRanking(t *testing.T) {
	store := knowledge.NewMemory()
	seedInvestigationWorld(t, store)
	api := NewAPI(store)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /board = %d", rec.Code)
	}
	body := rec.Body.String()
	if containsAny(body, "login", "ranking", "rank", "github.com:user", "jira.com") {
		t.Errorf("board leaks individual data: %s", body)
	}
	var got board
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, sec := range got.Sections {
		for _, it := range sec.Items {
			for _, ev := range it.Evidence {
				if ev.Type == "Engineer" || ev.Type == "SourceIdentity" {
					t.Errorf("board cites an individual: %+v", ev)
				}
			}
		}
	}
}

// TestBoardEmptyWindow: a window with no data yields the same five sections,
// with notes — the board never fabricates numbers.
func TestBoardEmptyWindow(t *testing.T) {
	store := knowledge.NewMemory()
	seedInvestigationWorld(t, store)
	api := NewAPI(store)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/board?scope=github.com:org:acme&start=2027-01-01T00:00:00Z&end=2027-02-01T00:00:00Z", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /board = %d", rec.Code)
	}
	var got board
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sections) != 5 {
		t.Fatalf("sections = %d, want 5", len(got.Sections))
	}
	if len(got.Sections[0].Items) != 0 || got.Sections[0].Note == "" {
		t.Errorf("flow = %+v, want empty + note", got.Sections[0])
	}
	if len(got.Sections[3].Items) != 0 || got.Sections[3].Note == "" {
		t.Errorf("AI = %+v, want empty + note", got.Sections[3])
	}
	if len(got.Sections[4].Items) != 0 || got.Sections[4].Note == "" {
		t.Errorf("coverage = %+v, want empty + note", got.Sections[4])
	}
}

func TestBoardBadRequests(t *testing.T) {
	api := NewAPI(knowledge.NewMemory())
	cases := []string{
		"/board", // no params
		"/board?scope=x&start=2026-09-01T00:00:00Z&end=2026-10-01T00:00:00Z",   // unknown scope
		"/board?scope=github.com:org:acme&start=nope&end=2026-10-01T00:00:00Z", // bad start
		"/board?scope=github.com:org:acme&start=2026-09-01T00:00:00Z",          // missing end
	}
	for _, u := range cases {
		rec := httptest.NewRecorder()
		api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, u, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", u, rec.Code)
		}
	}
}

// TestBoardQualityWithCI: with CI builds ingested, the Quality section
// carries the pass rate (CALCULATED + evidence); the static "no CI source"
// note is for worlds without builds.
func TestBoardQualityWithCI(t *testing.T) {
	store := knowledge.NewMemory()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})
	sep := func(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC) }
	for i, verdict := range []string{"success", "success", "success", "failure"} {
		b := upsert(t, store, mapKO(t, ontology.Build{
			Repository: "acme/widgets", ID: int64(200 + i), Name: "CI",
			Conclusion: verdict, Status: "completed", CompletedAt: sep(3 + i),
		}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsBuild), From: repo.Koid, To: b.Koid})
	}

	api := NewAPI(store)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /board = %d, body %s", rec.Code, rec.Body.String())
	}
	var got board
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	quality := got.Sections[1]
	if len(quality.Items) != 1 || quality.Items[0].Metric != "ci_pass_rate" {
		t.Fatalf("Quality section = %+v, want one ci_pass_rate item", quality)
	}
	it := quality.Items[0]
	if it.Value != 75.0 || it.Unit != "%" || it.EpistemicState != "CALCULATED" || len(it.Evidence) == 0 {
		t.Errorf("ci_pass_rate = %+v, want 75.0%% CALCULATED with evidence", it)
	}
	if quality.Note != "" {
		t.Errorf("Quality note = %q, want none when data exists", quality.Note)
	}
}

// TestBoardAIWorkflowMetrics: with AI telemetry linked to the scope, the AI
// Development section carries the workflow dimension (§27, Milestone E) —
// volume, task outcomes, and cost alongside the assisted-PR share, in the
// metric set's order.
func TestBoardAIWorkflowMetrics(t *testing.T) {
	store := knowledge.NewMemory()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	sep := func(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC) }
	org := upsert(t, store, mapKO(t, ontology.Organization{Login: "acme"}.KnowledgeObject, prov))
	repo := upsert(t, store, mapKO(t, ontology.Repository{Owner: "acme", Name: "widgets"}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelBelongsTo), From: repo.Koid, To: org.Koid})
	pr := upsert(t, store, mapKO(t, ontology.PullRequest{
		Repository: "acme/widgets", Number: 42, Merged: true, CreatedAt: sep(1), MergedAt: sep(5),
	}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelTargets), From: pr.Koid, To: repo.Koid})

	session := upsert(t, store, mapKO(t, ontology.CodingSession{Source: "claude-code", SessionID: "s1", StartedAt: sep(1)}.KnowledgeObject, prov))
	for _, id := range []string{"i1", "i2"} {
		ko := upsert(t, store, mapKO(t, ontology.Interaction{Source: "claude-code", ID: id, StartedAt: sep(2)}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsInteraction), From: session.Koid, To: ko.Koid})
	}
	for _, r := range []struct {
		id   string
		cost float64
	}{{"r1", 1.0}, {"r2", 2.0}} {
		ko := upsert(t, store, mapKO(t, ontology.AgentRun{Source: "claude-code", ID: r.id, StartedAt: sep(2), Status: "completed", CostUSD: r.cost}.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsRun), From: session.Koid, To: ko.Koid})
	}
	// 4 finished tasks: 3 completed (one intervened), one failed with retries
	// — completion 75%, intervention 25%, retry 50%, cost/task 3.0/3 = 1.0.
	tasks := []ontology.AgentTask{
		{Source: "claude-code", ID: "t1", Status: "completed", CompletedAt: sep(3)},
		{Source: "claude-code", ID: "t2", Status: "completed", HumanIntervention: true, CompletedAt: sep(3)},
		{Source: "claude-code", ID: "t3", Status: "failed", Retries: 2, CompletedAt: sep(4)},
		{Source: "claude-code", ID: "t4", Status: "completed", Retries: 1, CompletedAt: sep(4)},
	}
	for _, tk := range tasks {
		ko := upsert(t, store, mapKO(t, tk.KnowledgeObject, prov))
		relate(t, store, knowledge.Relationship{Type: string(ontology.RelContainsTask), From: session.Koid, To: ko.Koid})
	}
	contrib := upsert(t, store, mapKO(t, ontology.CodeContribution{
		Source: "claude-code", ID: "c1", Repository: "acme/widgets", PRNumber: 42,
		Session:     ontology.SessionExternalID("claude-code", "s1"),
		Attribution: ontology.Attribution{Level: ontology.AttributionDirect, Source: "claude-code", Evidence: "session:s1"},
	}.KnowledgeObject, prov))
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelAIContributes), From: contrib.Koid, To: pr.Koid})
	relate(t, store, knowledge.Relationship{Type: string(ontology.RelAuthored), From: storeMust(t, store, "ei.com:agent-task:claude-code:t1").Koid, To: contrib.Koid})

	api := NewAPI(store)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /board = %d, body %s", rec.Code, rec.Body.String())
	}
	var got board
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	ai := got.Sections[3]
	want := []boardItem{
		{Metric: "ai_assisted_pr_pct", Value: 100.0, Unit: "%"},
		{Metric: "ai_interaction_volume", Value: 2.0, Unit: "interactions"},
		{Metric: "ai_run_volume", Value: 2.0, Unit: "runs"},
		{Metric: "ai_task_completion", Value: 75.0, Unit: "%"},
		{Metric: "human_intervention_rate", Value: 25.0, Unit: "%"},
		{Metric: "retry_rate", Value: 50.0, Unit: "%"},
		{Metric: "ai_cost", Value: 3.0, Unit: "USD"},
		{Metric: "cost_per_completed_task", Value: 1.0, Unit: "USD"},
	}
	if len(ai.Items) != len(want) {
		t.Fatalf("AI items = %d, want %d", len(ai.Items), len(want))
	}
	for i, w := range want {
		it := ai.Items[i]
		if it.Metric != w.Metric || it.Value != w.Value || it.Unit != w.Unit {
			t.Errorf("AI item %d = %+v, want metric %s value %.1f unit %s", i, it, w.Metric, w.Value, w.Unit)
		}
		if it.EpistemicState != "CALCULATED" || len(it.Evidence) == 0 {
			t.Errorf("AI item %s lacks evidence: %+v", it.Metric, it)
		}
	}
	if ai.Note != "" {
		t.Errorf("AI note = %q, want none when telemetry exists", ai.Note)
	}
}

// storeMust fetches a stored object by external id; test helper.
func storeMust(t *testing.T, store knowledge.KnowledgeStore, extID string) knowledge.KnowledgeObject {
	t.Helper()
	ko, err := store.GetByExternalID(context.Background(), extID)
	if err != nil {
		t.Fatalf("get %s: %v", extID, err)
	}
	return ko
}

// TestBoardLiveAikoql: the board against the real store.
func TestBoardLiveAikoql(t *testing.T) {
	store := aikoqltest.Live(t)
	seedInvestigationWorld(t, store)
	api := NewAPI(store)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /board = %d, body %s", rec.Code, rec.Body.String())
	}
	var got board
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Sections) != 5 || got.Sections[0].Title != "Engineering Flow" {
		t.Errorf("board = %+v", got.Sections)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
