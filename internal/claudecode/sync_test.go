// Claude Code telemetry connector tests (§25-26, §38 MVP path): one real AI
// development telemetry source. A transcript is a log the tool itself wrote —
// every extracted object is backed by a line in it, and every DIRECT
// attribution cites the tool_use record as evidence. Anything the transcript
// does not prove is dropped or left unlinked, never invented.
package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge/aikoqltest"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

const (
	src    = "claude-code"
	model  = "claude-sonnet-5"
	prExt  = "github.com:pr:acme/widgets#42"
	cwdDir = `C:\dev\widgets`
)

// fixtureLine marshals one transcript entry. Shapes mirror the real format
// probed from live transcripts (type/sessionId/timestamp/cwd/uuid/message).
func fixtureLine(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}

type msg struct {
	Role    string `json:"role"`
	Model   string `json:"model,omitempty"`
	Content []any  `json:"content"`
}

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolUseBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type toolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   []any  `json:"content"`
	IsError   bool   `json:"is_error"`
}

func userText(uuid, ts, text string) msg {
	return msg{Role: "user", Content: []any{textBlock{Type: "text", Text: text}}}
}

func userResult(uuid, ts string, blocks ...toolResultBlock) msg {
	content := make([]any, len(blocks))
	for i, b := range blocks {
		content[i] = b
	}
	return msg{Role: "user", Content: content}
}

func assistantTools(uuid, ts string, model string, blocks ...toolUseBlock) msg {
	content := make([]any, len(blocks))
	for i, b := range blocks {
		content[i] = b
	}
	return msg{Role: "assistant", Model: model, Content: content}
}

// fixtureTranscript is the canonical session: one prompt, three assistant
// tool-calling runs (Edit, Bash `gh pr create` whose result carries the PR
// URL, Write) - 3 tasks, 2 contributions linked to PR 42.
func fixtureTranscript() []byte {
	var out []byte
	add := func(lineType, uuid, ts, cwd string, m msg) {
		out = append(out, fixtureLine(map[string]any{
			"type": lineType, "uuid": uuid, "sessionId": "s1",
			"timestamp": ts, "cwd": cwd, "message": m,
		})...)
	}
	add("user", "u1", "2026-09-01T10:00:00.123Z", cwdDir, userText("u1", "", "Add a helper"))
	add("assistant", "a1", "2026-09-01T10:01:00.456Z", cwdDir, assistantTools("a1", "", model,
		toolUseBlock{Type: "tool_use", ID: "toolu_1", Name: "Edit", Input: map[string]any{"file_path": `C:\dev\widgets\util.go`}}))
	add("user", "u2", "2026-09-01T10:01:05.789Z", cwdDir, userResult("u2", "",
		toolResultBlock{Type: "tool_result", ToolUseID: "toolu_1", Content: []any{textBlock{Type: "text", Text: "edited"}}}))
	add("assistant", "a2", "2026-09-01T10:02:00Z", cwdDir, assistantTools("a2", "", model,
		toolUseBlock{Type: "tool_use", ID: "toolu_2", Name: "Bash", Input: map[string]any{"command": "gh pr create --fill"}}))
	add("user", "u3", "2026-09-01T10:02:10Z", cwdDir, userResult("u3", "",
		toolResultBlock{Type: "tool_result", ToolUseID: "toolu_2", Content: []any{textBlock{Type: "text", Text: "created https://github.com/acme/widgets/pull/42"}}}))
	add("assistant", "a3", "2026-09-01T10:03:00Z", cwdDir, assistantTools("a3", "", model,
		toolUseBlock{Type: "tool_use", ID: "toolu_3", Name: "Write", Input: map[string]any{"file_path": `C:\dev\widgets\util2.go`}}))
	add("user", "u4", "2026-09-01T10:04:00Z", cwdDir, userResult("u4", "",
		toolResultBlock{Type: "tool_result", ToolUseID: "toolu_3", Content: []any{textBlock{Type: "text", Text: "written"}}}))
	return out
}

func writeTranscript(t *testing.T, name string, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestParseSession: the canonical transcript normalizes into the §25 types,
// with the PR link and DIRECT attribution taken from telemetry only.
func TestParseSession(t *testing.T) {
	dir := writeTranscript(t, "s1.jsonl", fixtureTranscript())
	sess, err := parseFile(filepath.Join(dir, "s1.jsonl"), func(string) string { return "acme/widgets" })
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if sess.session.Source != src || sess.session.SessionID != "s1" || sess.session.Agent != model {
		t.Errorf("session = %+v", sess.session)
	}
	if !sess.session.StartedAt.Equal(time.Date(2026, 9, 1, 10, 0, 0, 123000000, time.UTC)) ||
		!sess.session.EndedAt.Equal(time.Date(2026, 9, 1, 10, 4, 0, 0, time.UTC)) {
		t.Errorf("session times = %v..%v", sess.session.StartedAt, sess.session.EndedAt)
	}

	if len(sess.interactions) != 1 {
		t.Fatalf("interactions = %d, want 1 (the prompt)", len(sess.interactions))
	}
	if sess.interactions[0].ID != "u1" || sess.interactions[0].Agent != src {
		t.Errorf("interaction = %+v", sess.interactions[0])
	}

	if len(sess.runs) != 3 {
		t.Fatalf("runs = %d, want 3 (one per tool-calling assistant message)", len(sess.runs))
	}
	for _, r := range sess.runs {
		if r.Agent != model || r.Status != "completed" || r.CostUSD != 0 || r.CostReported {
			t.Errorf("run = %+v", r)
		}
	}
	if !sess.runs[0].EndedAt.Equal(time.Date(2026, 9, 1, 10, 1, 5, 789000000, time.UTC)) {
		t.Errorf("run 0 end = %v", sess.runs[0].EndedAt)
	}

	if len(sess.tasks) != 3 {
		t.Fatalf("tasks = %d, want 3", len(sess.tasks))
	}
	byName := map[string]ontology.AgentTask{}
	for _, tk := range sess.tasks {
		byName[tk.Description] = tk
	}
	if byName["Edit"].Status != "completed" || byName["Bash"].Status != "completed" || byName["Write"].Status != "completed" {
		t.Errorf("task statuses = %+v", byName)
	}
	if byName["Edit"].Run != ontology.AgentRunExternalID(src, "a1") ||
		byName["Bash"].Run != ontology.AgentRunExternalID(src, "a2") ||
		byName["Write"].Run != ontology.AgentRunExternalID(src, "a3") {
		t.Errorf("task runs = %+v", byName)
	}

	if len(sess.contributions) != 2 {
		t.Fatalf("contributions = %d, want 2 (Edit + Write; Bash is not a code tool)", len(sess.contributions))
	}
	for _, c := range sess.contributions {
		if c.Repository != "acme/widgets" || c.PRNumber != 42 || c.Session != ontology.SessionExternalID(src, "s1") {
			t.Errorf("contribution = %+v", c)
		}
		if c.Attribution.Level != ontology.AttributionDirect || c.Attribution.Source != src || c.Attribution.Evidence == "" {
			t.Errorf("attribution = %+v", c.Attribution)
		}
		if err := c.Attribution.Validate(); err != nil {
			t.Errorf("attribution invalid: %v", err)
		}
	}
}

// TestParseFailuresAndIntervention: is_error results mark the task and run
// failed; a permission denial is recorded as human intervention — telemetry
// facts, not judgments.
func TestParseFailuresAndIntervention(t *testing.T) {
	var out []byte
	add := func(lineType, uuid, ts string, m msg) {
		out = append(out, fixtureLine(map[string]any{
			"type": lineType, "uuid": uuid, "sessionId": "s2",
			"timestamp": ts, "cwd": cwdDir, "message": m,
		})...)
	}
	add("assistant", "a1", "2026-09-02T10:00:00Z", assistantTools("a1", "", model,
		toolUseBlock{Type: "tool_use", ID: "toolu_1", Name: "Edit", Input: map[string]any{"file_path": `C:\dev\widgets\x.go`}}))
	add("user", "u1", "2026-09-02T10:01:00Z", userResult("u1", "",
		toolResultBlock{Type: "tool_result", ToolUseID: "toolu_1", IsError: true,
			Content: []any{textBlock{Type: "text", Text: "permission denied for this file"}}}))
	add("mode", "m1", "2026-09-02T10:02:00Z", msg{}) // unknown types are skipped

	dir := writeTranscript(t, "s2.jsonl", out)
	sess, err := parseFile(filepath.Join(dir, "s2.jsonl"), func(string) string { return "acme/widgets" })
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sess.runs) != 1 || sess.runs[0].Status != "failed" {
		t.Fatalf("runs = %+v, want one failed run", sess.runs)
	}
	if len(sess.tasks) != 1 {
		t.Fatalf("tasks = %+v, want 1", sess.tasks)
	}
	if sess.tasks[0].Status != "failed" || !sess.tasks[0].HumanIntervention {
		t.Errorf("task = %+v, want failed + human_intervention", sess.tasks[0])
	}
	if !sess.tasks[0].CompletedAt.Equal(time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC)) {
		t.Errorf("completed_at = %v", sess.tasks[0].CompletedAt)
	}
}

// TestParseHonestGaps: a contribution without a PR link keeps PRNumber 0; a
// contribution with no repository at all is dropped (unattributed) — the
// transcript does not prove either, so the connector records the gap.
func TestParseHonestGaps(t *testing.T) {
	var out []byte
	add := func(lineType, uuid, ts string, m msg) {
		out = append(out, fixtureLine(map[string]any{
			"type": lineType, "uuid": uuid, "sessionId": "s3",
			"timestamp": ts, "cwd": cwdDir, "message": m,
		})...)
	}
	add("assistant", "a1", "2026-09-03T10:00:00Z", assistantTools("a1", "", model,
		toolUseBlock{Type: "tool_use", ID: "toolu_1", Name: "Edit", Input: map[string]any{"file_path": `C:\dev\widgets\y.go`}}))
	add("user", "u1", "2026-09-03T10:01:00Z", userResult("u1", "",
		toolResultBlock{Type: "tool_result", ToolUseID: "toolu_1", Content: []any{textBlock{Type: "text", Text: "edited"}}}))

	dir := writeTranscript(t, "s3.jsonl", out)
	sess, err := parseFile(filepath.Join(dir, "s3.jsonl"), func(string) string { return "" })
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sess.contributions) != 0 || sess.unattributed != 1 {
		t.Errorf("contributions = %d, unattributed = %d, want 0 and 1", len(sess.contributions), sess.unattributed)
	}

	// With a repo but no PR URL: contribution kept, PRNumber 0.
	sess, err = parseFile(filepath.Join(dir, "s3.jsonl"), func(string) string { return "acme/widgets" })
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sess.contributions) != 1 || sess.contributions[0].PRNumber != 0 {
		t.Errorf("contributions = %+v, want one with PRNumber 0", sess.contributions)
	}
}

// seedPR puts a merged PR 42 in the store so AI_CONTRIBUTES has a target.
func seedPR(t *testing.T, store knowledge.KnowledgeStore) {
	t.Helper()
	prov := ontology.NewProvenance("https://github.com/acme/widgets", time.Now())
	ko, _ := ontology.PullRequest{
		Repository: "acme/widgets", Number: 42, Merged: true,
		CreatedAt: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
		MergedAt:  time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}.KnowledgeObject(prov)
	if _, err := store.Upsert(context.Background(), ko); err != nil {
		t.Fatalf("seed PR: %v", err)
	}
}

func syncCfg(dir string, store knowledge.KnowledgeStore) Config {
	return Config{Dir: dir, Store: store, RepoOf: func(string) string { return "acme/widgets" }}
}

// TestSyncWritesTelemetry: one run lands the session, interactions, runs,
// tasks and contributions, linked to the seeded PR — and a rerun is
// idempotent (re-seen identical counts nothing).
func TestSyncWritesTelemetry(t *testing.T) {
	store := knowledge.NewMemory()
	seedPR(t, store)
	dir := writeTranscript(t, "s1.jsonl", fixtureTranscript())

	res, err := Sync(context.Background(), syncCfg(dir, store))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.Sessions != 1 || res.Relationships != 11 || res.Unlinked != 0 {
		t.Errorf("result = %+v, want 1 session, 11 edges (2 AI_CONTRIBUTES + 2 AUTHORED + 7 session containment), 0 unlinked", res)
	}
	wantCounts := map[string]int{
		"CodingSession": 1, "Interaction": 1, "AgentRun": 3, "AgentTask": 3, "CodeContribution": 2,
	}
	for typ, n := range wantCounts {
		if res.Counts[typ].New != n {
			t.Errorf("counts[%s] = %+v, want %d new", typ, res.Counts[typ], n)
		}
	}

	// The §26 evidence path: the seeded PR's inbound AI_CONTRIBUTES walk
	// reaches both contributions.
	prKO, err := store.GetByExternalID(context.Background(), prExt)
	if err != nil {
		t.Fatalf("get PR: %v", err)
	}
	got, err := store.Traverse(context.Background(), prKO.Koid, string(ontology.RelAIContributes), knowledge.Inbound, 1)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("traverse AI_CONTRIBUTES = %d objects, want 2", len(got))
	}

	// Rerun: everything re-seen identical — zero new, zero updated.
	res2, err := Sync(context.Background(), syncCfg(dir, store))
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	for typ, c := range res2.Counts {
		if c.New != 0 || c.Updated != 0 {
			t.Errorf("rerun counts[%s] = %+v, want all re-seen identical", typ, c)
		}
	}
}

// TestSyncUnlinkedContribution: with no PR 42 in the store, the contribution
// is persisted but left without an edge — reported, never fabricated.
func TestSyncUnlinkedContribution(t *testing.T) {
	store := knowledge.NewMemory()
	dir := writeTranscript(t, "s1.jsonl", fixtureTranscript())
	res, err := Sync(context.Background(), syncCfg(dir, store))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.Unlinked != 2 {
		t.Errorf("unlinked = %d, want 2", res.Unlinked)
	}
	if res.Counts["CodeContribution"].New != 2 {
		t.Errorf("contributions = %+v, want 2 stored even when unlinked", res.Counts["CodeContribution"])
	}
}

// TestSyncAgentTaskLinks: the task whose tool_use produced a code edit links
// AUTHORED to its contribution — the Milestone D graph AgentTask →
// CodeContribution → PR reconstructs from telemetry alone (a non-code task
// produces nothing).
func TestSyncAgentTaskLinks(t *testing.T) {
	store := knowledge.NewMemory()
	seedPR(t, store)
	dir := writeTranscript(t, "s1.jsonl", fixtureTranscript())
	if _, err := Sync(context.Background(), syncCfg(dir, store)); err != nil {
		t.Fatalf("sync: %v", err)
	}

	task1, err := store.GetByExternalID(context.Background(), "ei.com:agent-task:claude-code:toolu_1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Traverse(context.Background(), task1.Koid, string(ontology.RelAuthored), knowledge.Outbound, 1)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 1 || got[0].ExternalID != "ei.com:ai-contribution:claude-code:s1:toolu_1" {
		t.Errorf("task1 AUTHORED = %v, want exactly its contribution", got)
	}

	// The full Milestone D chain: the contribution reaches its PR.
	contrib, err := store.GetByExternalID(context.Background(), "ei.com:ai-contribution:claude-code:s1:toolu_1")
	if err != nil {
		t.Fatal(err)
	}
	got, err = store.Traverse(context.Background(), contrib.Koid, string(ontology.RelAIContributes), knowledge.Outbound, 1)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 1 || got[0].ExternalID != prExt {
		t.Errorf("contribution AI_CONTRIBUTES = %v, want the PR", got)
	}

	// toolu_2 (Bash) created no code: no AUTHORED outbound.
	task2, err := store.GetByExternalID(context.Background(), "ei.com:agent-task:claude-code:toolu_2")
	if err != nil {
		t.Fatal(err)
	}
	got, err = store.Traverse(context.Background(), task2.Koid, string(ontology.RelAuthored), knowledge.Outbound, 1)
	if err != nil || len(got) != 0 {
		t.Errorf("task2 AUTHORED = %v, %v, want none", got, err)
	}
}

// TestSyncSessionContainment: the session contains its interactions, runs
// and tasks — the graph path the population walk (intelligence) uses to
// reach telemetry from a PR-scoped contribution (Milestone E board).
func TestSyncSessionContainment(t *testing.T) {
	store := knowledge.NewMemory()
	dir := writeTranscript(t, "s1.jsonl", fixtureTranscript())
	if _, err := Sync(context.Background(), syncCfg(dir, store)); err != nil {
		t.Fatalf("sync: %v", err)
	}
	session, err := store.GetByExternalID(context.Background(), "ei.com:coding-session:claude-code:s1")
	if err != nil {
		t.Fatal(err)
	}
	want := map[ontology.RelType][]string{
		ontology.RelContainsInteraction: {"ei.com:interaction:claude-code:u1"},
		ontology.RelContainsRun:         {"ei.com:agent-run:claude-code:a1", "ei.com:agent-run:claude-code:a2", "ei.com:agent-run:claude-code:a3"},
		ontology.RelContainsTask:        {"ei.com:agent-task:claude-code:toolu_1", "ei.com:agent-task:claude-code:toolu_2", "ei.com:agent-task:claude-code:toolu_3"},
	}
	for rel, ids := range want {
		got, err := store.Traverse(context.Background(), session.Koid, string(rel), knowledge.Outbound, 1)
		if err != nil {
			t.Fatalf("traverse %s: %v", rel, err)
		}
		if len(got) != len(ids) {
			t.Fatalf("%s = %d children, want %d", rel, len(got), len(ids))
		}
		for _, ko := range got {
			found := false
			for _, id := range ids {
				if ko.ExternalID == id {
					found = true
				}
			}
			if !found {
				t.Errorf("%s reached unexpected %s", rel, ko.ExternalID)
			}
		}
	}
}

// TestDefaultRepoFromGitConfig: the real repo derivation parses .git/config.
func TestDefaultRepoFromGitConfig(t *testing.T) {
	cfg := `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = https://github.com/acme/widgets.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`
	if got, err := ownerRepoFromGitConfig(strings.NewReader(cfg)); err != nil || got != "acme/widgets" {
		t.Errorf("https = %q, %v", got, err)
	}
	ssh := `[remote "origin"]
	url = git@github.com:acme/widgets.git
`
	if got, err := ownerRepoFromGitConfig(strings.NewReader(ssh)); err != nil || got != "acme/widgets" {
		t.Errorf("ssh = %q, %v", got, err)
	}
	if _, err := ownerRepoFromGitConfig(strings.NewReader(`[core]`)); err == nil {
		t.Error("no remote: expected error")
	}
}

// TestSyncLiveAikoql: the telemetry source against the real store.
func TestSyncLiveAikoql(t *testing.T) {
	store := aikoqltest.Live(t)
	seedPR(t, store)
	dir := writeTranscript(t, "s1.jsonl", fixtureTranscript())
	res, err := Sync(context.Background(), syncCfg(dir, store))
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.Sessions != 1 || res.Counts["CodeContribution"].New != 2 || res.Unlinked != 0 {
		t.Errorf("result = %+v", res)
	}
	prKO, err := store.GetByExternalID(context.Background(), prExt)
	if err != nil {
		t.Fatalf("get PR: %v", err)
	}
	got, err := store.Traverse(context.Background(), prKO.Koid, string(ontology.RelAIContributes), knowledge.Inbound, 1)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("traverse = %d, want 2", len(got))
	}
}
