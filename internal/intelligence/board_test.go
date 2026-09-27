// Product board tests (§28): one response with the doc's five sections.
// Empty sections are honest notes, never fabricated numbers; the board
// carries no per-person data, so individual ranking is impossible by
// construction.
package intelligence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge/aikoqltest"
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
