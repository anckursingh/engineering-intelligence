// External test package: knowledgetest imports knowledge, so this stays
// external too (see memory_test.go).
package knowledge_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ancku/aikoql-sdk"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge/knowledgetest"
)

// liveStore spawns a fresh per-test aikoql server over stdio and returns the
// adapter wrapped around it. Gated on AIKOQL_MCP_BIN (CI sets it; see TESTING.md).
func liveStore(t *testing.T) knowledge.KnowledgeStore {
	t.Helper()
	bin := os.Getenv("AIKOQL_MCP_BIN")
	if bin == "" {
		t.Skip("AIKOQL_MCP_BIN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	dbDir := filepath.Join(t.TempDir(), "kb") // non-existent: auto-created by the server
	c, err := aikoql.DialStdio(ctx, bin, "serve", dbDir)
	if err != nil {
		t.Fatalf("DialStdio: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return knowledge.NewAikoql(c)
}

// AikoqlStore is graded against the same frozen contract as Memory (Phase 1B).
func TestAikoqlContract(t *testing.T) {
	knowledgetest.RunContract(t, liveStore)
}

// Phase 1E (§7): persistence across process restart — data written by one
// server process must be visible to a fresh one on the same database.
func TestAikoqlRestartPersistence(t *testing.T) {
	bin := os.Getenv("AIKOQL_MCP_BIN")
	if bin == "" {
		t.Skip("AIKOQL_MCP_BIN not set")
	}
	dbDir := filepath.Join(t.TempDir(), "kb")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	mk := func() (*aikoql.Client, knowledge.KnowledgeStore) {
		c, err := aikoql.DialStdio(ctx, bin, "serve", dbDir)
		if err != nil {
			t.Fatalf("DialStdio: %v", err)
		}
		if err := c.Initialize(ctx); err != nil {
			t.Fatalf("Initialize: %v", err)
		}
		return c, knowledge.NewAikoql(c)
	}

	c1, s1 := mk()
	org, err := s1.Upsert(ctx, knowledge.KnowledgeObject{
		TypeName: "Organization", ExternalID: "github.com:org:acme",
		Properties: map[string]any{"login": "acme"},
		Provenance: knowledge.Provenance{Source: "test", ObservedAt: time.Now()},
	})
	if err != nil {
		t.Fatalf("upsert org: %v", err)
	}
	repo, err := s1.Upsert(ctx, knowledge.KnowledgeObject{
		TypeName: "Repository", ExternalID: "github.com:repo:acme/r",
		Properties: map[string]any{"name": "r"},
		Provenance: knowledge.Provenance{Source: "test", ObservedAt: time.Now()},
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	if err := s1.Relate(ctx, knowledge.Relationship{Type: "PART_OF", From: repo.Koid, To: org.Koid}); err != nil {
		t.Fatalf("relate: %v", err)
	}
	c1.Close() // stdin EOF: graceful checkpoint + exit

	// Fresh process on the same database must see everything.
	c2, s2 := mk()
	defer c2.Close()
	got, err := s2.GetByExternalID(ctx, "github.com:org:acme")
	if err != nil {
		t.Fatalf("GetByExternalID after restart: %v", err)
	}
	if got.Koid != org.Koid || got.Properties["login"] != "acme" {
		t.Errorf("org after restart = %+v, want koid %s login acme", got, org.Koid)
	}
	got, err = s2.GetByExternalID(ctx, "github.com:repo:acme/r")
	if err != nil || got.Koid != repo.Koid {
		t.Fatalf("repo after restart = %+v, %v", got, err)
	}
	trav, err := s2.Traverse(ctx, repo.Koid, "PART_OF", knowledge.Outbound, 1)
	if err != nil {
		t.Fatalf("traverse after restart: %v", err)
	}
	if len(trav) != 1 || trav[0].Koid != org.Koid {
		t.Errorf("traverse after restart = %+v, want [%s]", trav, org.Koid)
	}
}

// --- unit tests against a scripted fake (no server) ---

type fakeCall struct {
	tool string
	out  json.RawMessage
	err  error
}

// fakeDB plays a scripted sequence of tool calls. Each call must match the
// next expected tool name; args are captured for the test to inspect.
type fakeDB struct {
	script []fakeCall
	calls  []struct {
		tool string
		args map[string]any
	}
}

func (f *fakeDB) CallTool(_ context.Context, name string, arguments any) (json.RawMessage, error) {
	args, _ := arguments.(map[string]any)
	f.calls = append(f.calls, struct {
		tool string
		args map[string]any
	}{name, args})
	next := f.script[0]
	f.script = f.script[1:]
	if next.tool != name {
		return nil, errors.New("fakeDB: unexpected tool " + name + ", want " + next.tool)
	}
	return next.out, next.err
}

func notFoundErr(tool, koid string) error {
	return &aikoql.McpError{Code: "NOT_FOUND", Message: koid}
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const idxType = "ExternalIDIndex" // adapter bookkeeping type (adapter-owned, generic)

func TestAikoqlErrorMapping(t *testing.T) {
	f := &fakeDB{script: []fakeCall{{tool: "get", err: notFoundErr("get", "k1")}}}
	s := knowledge.NewAikoql(f)
	_, err := s.Get(context.Background(), "k1")
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("Get missing = %v, want ErrNotFound", err)
	}
}

func TestAikoqlTypeConflictViaIndex(t *testing.T) {
	f := &fakeDB{script: []fakeCall{{
		tool: "aikoql",
		out: rawJSON(t, map[string]any{"results": []map[string]any{{
			"koid": "k1", "type_name": "Issue", "version": 1,
			"properties": map[string]any{"external_id": "e1", "koid": "k1", "type_name": "Issue"},
		}}}),
	}}}
	s := knowledge.NewAikoql(f)
	_, err := s.Upsert(context.Background(), knowledge.KnowledgeObject{
		TypeName: "Commit", ExternalID: "e1",
	})
	if !errors.Is(err, knowledge.ErrTypeConflict) {
		t.Errorf("err = %v, want ErrTypeConflict", err)
	}
}

func TestAikoqlCreateWritesIndexAndInjectsBookkeeping(t *testing.T) {
	f := &fakeDB{script: []fakeCall{
		{tool: "aikoql", err: &aikoql.McpError{Code: "COMPILE_ERROR", Message: "unknown type"}}, // fresh DB
		{tool: "remember", out: rawJSON(t, map[string]any{"koid": "k1", "version": 1, "commit_ts": 1})},
		{tool: "get", out: rawJSON(t, map[string]any{
			"koid": "k1", "version": 1, "type_name": "Issue",
			"properties": map[string]any{"external_id": "e1", "n": float64(1)},
		})},
		{tool: "remember", out: rawJSON(t, map[string]any{"koid": "k2", "version": 1, "commit_ts": 2})},
	}}
	s := knowledge.NewAikoql(f)
	got, err := s.Upsert(context.Background(), knowledge.KnowledgeObject{
		TypeName: "Issue", ExternalID: "e1",
		Properties: map[string]any{"n": float64(1)},
		Provenance: knowledge.Provenance{Source: "s"},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got.Koid != "k1" || got.Version != 1 || got.ExternalID != "e1" {
		t.Errorf("got = %+v", got)
	}
	if len(f.calls) != 4 || f.calls[3].tool != "remember" {
		t.Fatalf("calls = %+v, want index remember last", f.calls)
	}
	idx := f.calls[3].args
	if idx["idempotency_key"] != "idx:e1" || idx["type_name"] != idxType {
		t.Errorf("index remember args = %+v", idx)
	}
	obj := f.calls[1].args
	props, _ := obj["properties"].(map[string]any)
	if props["external_id"] != "e1" {
		t.Errorf("object properties lack external_id: %+v", props)
	}
	if _, ok := props["_provenance"]; !ok {
		t.Errorf("object properties lack _provenance: %+v", props)
	}
}

func TestAikoqlCreateDetectsCrossTypeSilentReturn(t *testing.T) {
	// Server's global key index returns an existing object of another type
	// without error — the adapter must detect it, not trust the remember.
	f := &fakeDB{script: []fakeCall{
		{tool: "aikoql", out: rawJSON(t, map[string]any{"results": []any{}})},
		{tool: "remember", out: rawJSON(t, map[string]any{"koid": "k1", "version": 1, "commit_ts": 1})},
		{tool: "get", out: rawJSON(t, map[string]any{
			"koid": "k1", "version": 1, "type_name": "Issue",
			"properties": map[string]any{"external_id": "e1"},
		})},
	}}
	s := knowledge.NewAikoql(f)
	_, err := s.Upsert(context.Background(), knowledge.KnowledgeObject{
		TypeName: "Commit", ExternalID: "e1",
	})
	if !errors.Is(err, knowledge.ErrTypeConflict) {
		t.Errorf("err = %v, want ErrTypeConflict", err)
	}
	if len(f.calls) != 3 {
		t.Errorf("index write must be skipped on conflict; calls = %d", len(f.calls))
	}
}

func TestAikoqlUpdateBumpsVersionOnChangeOnly(t *testing.T) {
	base := []map[string]any{{
		"koid": "k1", "type_name": "Issue", "version": 1,
		"properties": map[string]any{"external_id": "e1", "koid": "k1", "type_name": "Issue"},
	}}
	getObj := map[string]any{
		"koid": "k1", "version": 1, "type_name": "Issue",
		"properties": map[string]any{"external_id": "e1", "n": float64(1)},
	}
	f := &fakeDB{script: []fakeCall{
		{tool: "aikoql", out: rawJSON(t, map[string]any{"results": base})},
		{tool: "get", out: rawJSON(t, getObj)},
		{tool: "remember", out: rawJSON(t, map[string]any{"koid": "k1", "version": 2, "commit_ts": 1})},
	}}
	s := knowledge.NewAikoql(f)
	got, err := s.Upsert(context.Background(), knowledge.KnowledgeObject{
		TypeName: "Issue", ExternalID: "e1",
		Properties: map[string]any{"n": float64(2)},
	})
	if err != nil {
		t.Fatalf("Upsert changed: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("version = %d, want 2", got.Version)
	}
	upd := f.calls[2].args
	if upd["koid"] != "k1" || upd["idempotency_key"] != nil {
		t.Errorf("update args = %+v, want koid without idempotency_key", upd)
	}
	ev, ok := upd["expected_version"].(uint64)
	if !ok || ev != 1 {
		t.Errorf("expected_version = %v, want 1", upd["expected_version"])
	}
}

// The server's traverse is undirected; the adapter runs a directed BFS of
// depth-1 traverses and filters hops by the per-hit direction label.
func TestAikoqlTraverseFiltersByDirectionLabel(t *testing.T) {
	f := &fakeDB{script: []fakeCall{
		{tool: "traverse", out: rawJSON(t, map[string]any{"hits": []map[string]any{
			{"koid": "k2", "rel_type": "PART_OF", "direction": "inbound"},  // kept
			{"koid": "k3", "rel_type": "PART_OF", "direction": "outbound"}, // filtered
		}})},
		{tool: "get", out: rawJSON(t, map[string]any{
			"koid": "k2", "version": 1, "type_name": "Repo",
			"properties": map[string]any{"external_id": "e2"},
		})},
	}}
	s := knowledge.NewAikoql(f)
	got, err := s.Traverse(context.Background(), "k1", "PART_OF", knowledge.Inbound, 1)
	if err != nil {
		t.Fatalf("Traverse: %v", err)
	}
	if len(got) != 1 || got[0].Koid != "k2" {
		t.Errorf("got = %+v, want [k2]", got)
	}
	args := f.calls[0].args
	if args["direction"] != "inbound" || args["depth"] != 1 || args["rel_type"] != "PART_OF" {
		t.Errorf("traverse args = %+v", args)
	}
}

// Depth-2 BFS with a cycle back to the start: the start is excluded and the
// loop must terminate.
func TestAikoqlTraverseDepthTwoCycleSafe(t *testing.T) {
	f := &fakeDB{script: []fakeCall{
		{tool: "traverse", out: rawJSON(t, map[string]any{"hits": []map[string]any{
			{"koid": "k2", "rel_type": "NEXT", "direction": "outbound"},
		}})},
		{tool: "traverse", out: rawJSON(t, map[string]any{"hits": []map[string]any{
			{"koid": "k1", "rel_type": "NEXT", "direction": "outbound"}, // cycle: start seen
			{"koid": "k3", "rel_type": "NEXT", "direction": "outbound"},
		}})},
		{tool: "get", out: rawJSON(t, map[string]any{
			"koid": "k2", "version": 1, "type_name": "Node",
			"properties": map[string]any{"external_id": "e2"},
		})},
		{tool: "get", out: rawJSON(t, map[string]any{
			"koid": "k3", "version": 1, "type_name": "Node",
			"properties": map[string]any{"external_id": "e3"},
		})},
	}}
	s := knowledge.NewAikoql(f)
	got, err := s.Traverse(context.Background(), "k1", "NEXT", knowledge.Outbound, 2)
	if err != nil {
		t.Fatalf("Traverse: %v", err)
	}
	if len(got) != 2 || got[0].Koid != "k2" || got[1].Koid != "k3" {
		t.Errorf("got = %+v, want [k2 k3]", got)
	}
}

func TestAikoqlDepthZeroRejected(t *testing.T) {
	s := knowledge.NewAikoql(&fakeDB{})
	if _, err := s.Traverse(context.Background(), "k1", "X", knowledge.Outbound, 0); err == nil {
		t.Error("depth 0 should error")
	}
}

// The ontology layer injects external_id into Properties; the stored side is
// read back with it stripped. The compare must ignore reserved keys or every
// rerun updates every object (AC-ING-005).
func TestAikoqlUpsertIgnoresReservedKeysInCompare(t *testing.T) {
	f := &fakeDB{script: []fakeCall{
		{tool: "aikoql", out: rawJSON(t, map[string]any{"results": []map[string]any{{
			"koid": "k1", "type_name": "Issue", "version": 1,
			"properties": map[string]any{"external_id": "e1", "koid": "k1", "type_name": "Issue"},
		}}})},
		{tool: "get", out: rawJSON(t, map[string]any{
			"koid": "k1", "version": 1, "type_name": "Issue",
			"properties": map[string]any{"n": float64(1)},
		})},
	}}
	s := knowledge.NewAikoql(f)
	got, err := s.Upsert(context.Background(), knowledge.KnowledgeObject{
		TypeName: "Issue", ExternalID: "e1",
		Properties: map[string]any{"external_id": "e1", "n": float64(1)},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("version = %d, want 1 (no update on identical payload)", got.Version)
	}
	if len(f.calls) != 2 {
		t.Errorf("calls = %d, want 2 (no update remember)", len(f.calls))
	}
}

func TestAikoqlRelateNotFound(t *testing.T) {
	f := &fakeDB{script: []fakeCall{{tool: "relate", err: notFoundErr("relate", "k9")}}}
	s := knowledge.NewAikoql(f)
	err := s.Relate(context.Background(), knowledge.Relationship{Type: "X", From: "k9", To: "k1"})
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAikoqlContextCancellation(t *testing.T) {
	s := knowledge.NewAikoql(&fakeDB{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Upsert(ctx, knowledge.KnowledgeObject{TypeName: "T", ExternalID: "e"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Upsert cancelled = %v", err)
	}
	if _, err := s.Get(ctx, "k"); !errors.Is(err, context.Canceled) {
		t.Errorf("Get cancelled = %v", err)
	}
	if err := s.Relate(ctx, knowledge.Relationship{}); !errors.Is(err, context.Canceled) {
		t.Errorf("Relate cancelled = %v", err)
	}
}

// The index lookup escapes external IDs into the KOQL string literal.
func TestAikoqlIndexLookupEscapesQuotes(t *testing.T) {
	f := &fakeDB{script: []fakeCall{{tool: "aikoql", out: rawJSON(t, map[string]any{"results": []any{}})}}}
	s := knowledge.NewAikoql(f)
	if _, err := s.GetByExternalID(context.Background(), `evil" OR "1`); !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	q, _ := f.calls[0].args["query"].(string)
	if !strings.Contains(q, `\"`) {
		t.Errorf("query not escaped: %s", q)
	}
}
