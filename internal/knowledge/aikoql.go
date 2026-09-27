// AikoqlStore adapts the KnowledgeStore contract to the AIKOQL MCP tool
// surface. All wire facts below were verified empirically against the live
// server (see TESTING.md §AIKOQL wire contract) — the server's behavior is
// the source of truth, this file only encodes it:
//
//   - create: remember{type_name, properties, idempotency_key=ExternalID}
//     → version 1; an identical re-remember is a no-op.
//   - update: a keyed re-remember with changed properties is silently
//     IGNORED; content updates require remember{koid, expected_version}.
//   - remember's idempotency index is global and type-blind: reusing a key
//     under another type_name silently returns the existing object. The
//     adapter keeps its own ExternalIDIndex objects (one per domain object)
//     and checks type_name client-side — ErrTypeConflict never reaches the
//     server's no-op path.
//   - GetByExternalID: no untyped KOQL exists (MATCH * and unknown entity
//     names are COMPILE_ERROR), so lookups go through ExternalIDIndex.
//   - relate is set-semantic (duplicates are no-ops) and bumps the FROM
//     object's version; server-side traverse is UNDIRECTED and ignores the
//     direction argument — hits carry a per-hit "direction" label relative
//     to the query node, so directed traversal is a client-side BFS of
//     depth-1 traverses filtered by label (verified on a cycle topology).
//   - errors: NOT_FOUND and VALIDATION_ERROR (malformed koid) both mean
//     "no such object"; VERSION_CONFLICT = stale expected_version.
package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/ancku/aikoql-sdk"
)

// aikoqlDB is the tool-call surface AikoqlStore needs. *aikoql.Client
// satisfies it structurally; tests inject a scripted fake.
type aikoqlDB interface {
	CallTool(ctx context.Context, name string, arguments any) (json.RawMessage, error)
}

// Bookkeeping constants — adapter-owned storage mechanics, not domain schema.
const (
	idxType       = "ExternalIDIndex" // index object type (generic, not EI-specific)
	provenanceKey = "_provenance"     // reserved property key: Provenance payload
	externalIDKey = "external_id"     // injected so objects are self-describing
)

// AikoqlStore persists KnowledgeObjects in an AIKOQL database.
type AikoqlStore struct {
	db aikoqlDB
}

// NewAikoql wraps an aikoql client (or any aikoqlDB) as a KnowledgeStore.
func NewAikoql(db aikoqlDB) *AikoqlStore { return &AikoqlStore{db: db} }

// koWire is the shared shape of get and MATCH result objects.
type koWire struct {
	KOID       string         `json:"koid"`
	Version    uint64         `json:"version"`
	TypeName   string         `json:"type_name"`
	Properties map[string]any `json:"properties"`
}

// Upsert creates the object (Version 1) or replaces it when Properties
// changed. Provenance is stored under _provenance and excluded from the
// change comparison, so re-ingestion with a new ObservedAt never bumps.
func (s *AikoqlStore) Upsert(ctx context.Context, obj KnowledgeObject) (KnowledgeObject, error) {
	if err := ctx.Err(); err != nil {
		return KnowledgeObject{}, err
	}
	entry, err := s.indexGet(ctx, obj.ExternalID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return KnowledgeObject{}, err
	}
	if err == nil {
		if entry.TypeName != obj.TypeName {
			return KnowledgeObject{}, fmt.Errorf("%w: external_id %q belongs to %s", ErrTypeConflict, obj.ExternalID, entry.TypeName)
		}
		cur, err := s.Get(ctx, entry.KOID)
		if err != nil {
			return KnowledgeObject{}, err
		}
		if !sameProps(cur.Properties, obj.Properties) {
			return s.update(ctx, obj, cur)
		}
		return cur, nil
	}
	return s.create(ctx, obj)
}

func (s *AikoqlStore) create(ctx context.Context, obj KnowledgeObject) (KnowledgeObject, error) {
	props, err := objProps(obj)
	if err != nil {
		return KnowledgeObject{}, err
	}
	out, err := s.remember(ctx, obj.TypeName, obj.ExternalID, "", nil, props)
	if err != nil {
		return KnowledgeObject{}, err
	}
	// Defensive: a key reused by another type is a silent no-op server-side —
	// verify the returned object really is ours before trusting it.
	got, err := s.getWire(ctx, out.KOID)
	if err != nil {
		return KnowledgeObject{}, err
	}
	if got.TypeName != obj.TypeName {
		return KnowledgeObject{}, fmt.Errorf("%w: external_id %q belongs to %s", ErrTypeConflict, obj.ExternalID, got.TypeName)
	}
	// Index entry, written after the object so a crash only orphans an entry
	// (self-healed by the next create via the defensive check above).
	if _, err := s.remember(ctx, idxType, idxKey(obj.ExternalID), "", nil, map[string]any{
		externalIDKey: obj.ExternalID,
		"koid":        out.KOID,
		"type_name":   obj.TypeName,
	}); err != nil {
		return KnowledgeObject{}, err
	}
	ko, err := wireToKO(got)
	if err != nil {
		return KnowledgeObject{}, err
	}
	return ko, nil
}

func (s *AikoqlStore) update(ctx context.Context, obj, cur KnowledgeObject) (KnowledgeObject, error) {
	props, err := objProps(obj)
	if err != nil {
		return KnowledgeObject{}, err
	}
	v := uint64(cur.Version)
	out, err := s.remember(ctx, obj.TypeName, "", cur.Koid, &v, props)
	if err != nil {
		return KnowledgeObject{}, err
	}
	obj.Koid = cur.Koid
	obj.Version = int64(out.Version)
	return obj, nil
}

func (s *AikoqlStore) remember(ctx context.Context, typeName, key, koid string, expected *uint64, props map[string]any) (*rememberedWire, error) {
	args := map[string]any{"type_name": typeName, "properties": props}
	if key != "" {
		args["idempotency_key"] = key
	}
	if koid != "" {
		args["koid"] = koid
	}
	if expected != nil {
		args["expected_version"] = *expected
	}
	raw, err := s.db.CallTool(ctx, "remember", args)
	if err != nil {
		return nil, mapErr("remember", err)
	}
	var out rememberedWire
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("aikoql remember payload: %w", err)
	}
	return &out, nil
}

type rememberedWire struct {
	KOID     string `json:"koid"`
	Version  uint64 `json:"version"`
	CommitTS uint64 `json:"commit_ts"`
}

// Get fetches one object by koid.
func (s *AikoqlStore) Get(ctx context.Context, koid string) (KnowledgeObject, error) {
	if err := ctx.Err(); err != nil {
		return KnowledgeObject{}, err
	}
	w, err := s.getWire(ctx, koid)
	if err != nil {
		return KnowledgeObject{}, err
	}
	return wireToKO(w)
}

func (s *AikoqlStore) getWire(ctx context.Context, koid string) (*koWire, error) {
	raw, err := s.db.CallTool(ctx, "get", map[string]any{"koid": koid})
	if err != nil {
		return nil, mapErr("get", err)
	}
	var w koWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, fmt.Errorf("aikoql get payload: %w", err)
	}
	return &w, nil
}

// GetByExternalID resolves through the adapter's ExternalIDIndex.
func (s *AikoqlStore) GetByExternalID(ctx context.Context, externalID string) (KnowledgeObject, error) {
	if err := ctx.Err(); err != nil {
		return KnowledgeObject{}, err
	}
	entry, err := s.indexGet(ctx, externalID)
	if err != nil {
		return KnowledgeObject{}, err
	}
	return s.Get(ctx, entry.KOID)
}

type indexEntry struct {
	KOID     string
	TypeName string
}

// indexGet runs the typed MATCH against the index objects. COMPILE_ERROR
// means the index type does not exist yet (fresh database) — not found.
func (s *AikoqlStore) indexGet(ctx context.Context, externalID string) (indexEntry, error) {
	q := fmt.Sprintf(`MATCH %s WHERE external_id == "%s" RETURN *`, idxType, escapeKoqlLiteral(externalID))
	raw, err := s.db.CallTool(ctx, "aikoql", map[string]any{"query": q})
	if err != nil {
		var me *aikoql.McpError
		if errors.As(err, &me) && me.Code == "COMPILE_ERROR" {
			return indexEntry{}, fmt.Errorf("%w: %s", ErrNotFound, me.Message)
		}
		return indexEntry{}, mapErr("index lookup", err)
	}
	var res struct {
		Results []koWire `json:"results"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return indexEntry{}, fmt.Errorf("aikoql index lookup payload: %w", err)
	}
	if len(res.Results) == 0 {
		return indexEntry{}, fmt.Errorf("%w: %s", ErrNotFound, externalID)
	}
	p := res.Results[0].Properties
	return indexEntry{KOID: p["koid"].(string), TypeName: p["type_name"].(string)}, nil
}

// Relate adds an edge; duplicates collapse server-side (set semantics).
func (s *AikoqlStore) Relate(ctx context.Context, rel Relationship) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.db.CallTool(ctx, "relate", map[string]any{
		"from": rel.From, "to": rel.To, "rel_type": rel.Type,
	})
	return mapErr("relate", err)
}

// Traverse walks relType edges up to depth hops from a starting koid. The
// server's traverse is undirected, so the adapter runs a directed BFS of
// depth-1 traverses: each hit's direction label (relative to the node that
// found it) is the hop filter. Cycle-safe via the seen set; hits are
// materialized with get (the wire returns koids only).
func (s *AikoqlStore) Traverse(ctx context.Context, from, relType string, dir Direction, depth int) ([]KnowledgeObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth < 1 {
		return nil, fmt.Errorf("knowledge: traverse depth %d must be >= 1", depth)
	}
	dirStr := "outbound"
	if dir == Inbound {
		dirStr = "inbound"
	}
	seen := map[string]bool{from: true}
	var found []string // insertion order, start excluded
	frontier := []string{from}
	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		var next []string
		for _, node := range frontier {
			hits, err := s.traverseHits(ctx, node, relType, dirStr)
			if err != nil {
				return nil, err
			}
			for _, h := range hits {
				if h.Direction != dirStr || h.RelType != relType || seen[h.KOID] {
					continue
				}
				seen[h.KOID] = true
				found = append(found, h.KOID)
				next = append(next, h.KOID)
			}
		}
		frontier = next
	}
	out := make([]KnowledgeObject, 0, len(found))
	for _, koid := range found {
		w, err := s.getWire(ctx, koid)
		if err != nil {
			return nil, err
		}
		ko, err := wireToKO(w)
		if err != nil {
			return nil, err
		}
		out = append(out, ko)
	}
	return out, nil
}

// traverseHits runs one depth-1 hop and returns every neighbor with its
// direction label.
func (s *AikoqlStore) traverseHits(ctx context.Context, koid, relType, dirStr string) ([]traverseHitWire, error) {
	raw, err := s.db.CallTool(ctx, "traverse", map[string]any{
		"koid": koid, "rel_type": relType, "depth": 1, "direction": dirStr,
	})
	if err != nil {
		return nil, mapErr("traverse", err)
	}
	var res struct {
		Hits []traverseHitWire `json:"hits"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("aikoql traverse payload: %w", err)
	}
	return res.Hits, nil
}

type traverseHitWire struct {
	KOID      string `json:"koid"`
	RelType   string `json:"rel_type"`
	Direction string `json:"direction"`
}

// objProps builds the wire properties: the payload plus the adapter's
// bookkeeping keys (external_id, _provenance).
func objProps(obj KnowledgeObject) (map[string]any, error) {
	props := make(map[string]any, len(obj.Properties)+2)
	for k, v := range obj.Properties {
		props[k] = v
	}
	props[externalIDKey] = obj.ExternalID
	pb, err := json.Marshal(obj.Provenance)
	if err != nil {
		return nil, fmt.Errorf("knowledge: marshal provenance: %w", err)
	}
	var pv map[string]any
	if err := json.Unmarshal(pb, &pv); err != nil {
		return nil, fmt.Errorf("knowledge: unmarshal provenance: %w", err)
	}
	props[provenanceKey] = pv
	return props, nil
}

// wireToKO converts a wire object back to the contract shape: bookkeeping
// keys are stripped, external_id moves into the field, _provenance into the
// Provenance struct.
func wireToKO(w *koWire) (KnowledgeObject, error) {
	ko := KnowledgeObject{
		Koid:       w.KOID,
		TypeName:   w.TypeName,
		Version:    int64(w.Version),
		Properties: w.Properties,
	}
	if ext, ok := w.Properties[externalIDKey].(string); ok {
		ko.ExternalID = ext
	}
	if pv, ok := w.Properties[provenanceKey].(map[string]any); ok {
		pb, err := json.Marshal(pv)
		if err != nil {
			return KnowledgeObject{}, fmt.Errorf("knowledge: marshal stored provenance: %w", err)
		}
		if err := json.Unmarshal(pb, &ko.Provenance); err != nil {
			return KnowledgeObject{}, fmt.Errorf("knowledge: unmarshal stored provenance: %w", err)
		}
	}
	delete(ko.Properties, externalIDKey)
	delete(ko.Properties, provenanceKey)
	return ko, nil
}

// sameProps compares payloads; empty maps of either side are equivalent to
// nil (the wire round-trip can normalize one to the other).
func sameProps(a, b map[string]any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	return len(a) == 0 && len(b) == 0
}

// mapErr translates server error codes into contract errors. NOT_FOUND and
// VALIDATION_ERROR (malformed koid) both mean "no such object"; everything
// else stays a wrapped transport/tool error.
func mapErr(name string, err error) error {
	if err == nil {
		return nil
	}
	var me *aikoql.McpError
	if errors.As(err, &me) && (me.Code == "NOT_FOUND" || me.Code == "VALIDATION_ERROR") {
		return fmt.Errorf("%w: %s: %s", ErrNotFound, name, me.Message)
	}
	return fmt.Errorf("aikoql %s: %w", name, err)
}

func idxKey(externalID string) string { return "idx:" + externalID }

// escapeKoqlLiteral quotes an external ID into a KOQL string literal.
// ponytail: assumes KOQL escapes are JSON-like (\" and \\); verify against
// the grammar if external IDs ever contain other control characters.
func escapeKoqlLiteral(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
