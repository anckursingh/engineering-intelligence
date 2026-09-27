// Package knowledgetest holds the frozen KnowledgeStore behavioral contract
// (TDD plan §4 / Phase 1B). Every store — Memory today, AikoqlStore next —
// is graded against this suite; it is the specification, not a convenience.
package knowledgetest

import (
	"context"
	"errors"
	"testing"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

// StoreFactory builds one fresh store per subtest.
type StoreFactory func(t *testing.T) knowledge.KnowledgeStore

// RunContract runs the full behavioral contract. All subtests must pass for
// a store to be a valid KnowledgeStore.
func RunContract(t *testing.T, factory StoreFactory) {
	t.Helper()
	t.Run("create-and-get", func(t *testing.T) {
		s := factory(t)
		created := mustUpsert(t, s, newObj("contract:create", "Issue", map[string]any{"n": float64(1)}))
		if created.Koid == "" || created.Version != 1 {
			t.Fatalf("created = %+v, want non-empty koid, version 1", created)
		}
		got, err := s.Get(context.Background(), created.Koid)
		if err != nil || got.Koid != created.Koid || got.ExternalID != created.ExternalID {
			t.Errorf("Get = %+v, %v", got, err)
		}
		got, err = s.GetByExternalID(context.Background(), "contract:create")
		if err != nil || got.Koid != created.Koid {
			t.Errorf("GetByExternalID = %+v, %v", got, err)
		}
	})
	t.Run("idempotent-upsert", func(t *testing.T) {
		s := factory(t)
		a := mustUpsert(t, s, newObj("contract:same", "Issue", map[string]any{"n": float64(1)}))
		b := mustUpsert(t, s, newObj("contract:same", "Issue", map[string]any{"n": float64(1)}))
		if a.Koid != b.Koid || a.Version != 1 || b.Version != 1 {
			t.Errorf("identical upsert changed identity: %+v then %+v", a, b)
		}
	})
	t.Run("version-update", func(t *testing.T) {
		s := factory(t)
		a := mustUpsert(t, s, newObj("contract:ver", "Issue", map[string]any{"n": float64(1)}))
		b := mustUpsert(t, s, newObj("contract:ver", "Issue", map[string]any{"n": float64(2)}))
		if b.Koid != a.Koid || b.Version != 2 {
			t.Errorf("changed upsert = %+v, want same koid version 2", b)
		}
		c := mustUpsert(t, s, newObj("contract:ver", "Issue", map[string]any{"n": float64(2)}))
		if c.Version != 2 {
			t.Errorf("unchanged rerun bumped version to %d", c.Version)
		}
	})
	t.Run("type-conflict", func(t *testing.T) {
		s := factory(t)
		mustUpsert(t, s, newObj("contract:tc", "Issue", nil))
		if _, err := s.Upsert(context.Background(), newObj("contract:tc", "Commit", nil)); !errors.Is(err, knowledge.ErrTypeConflict) {
			t.Errorf("err = %v, want ErrTypeConflict", err)
		}
	})
	t.Run("get-missing", func(t *testing.T) {
		s := factory(t)
		if _, err := s.Get(context.Background(), "ko-does-not-exist"); !errors.Is(err, knowledge.ErrNotFound) {
			t.Errorf("Get missing: %v, want ErrNotFound", err)
		}
		if _, err := s.GetByExternalID(context.Background(), "ext-does-not-exist"); !errors.Is(err, knowledge.ErrNotFound) {
			t.Errorf("GetByExternalID missing: %v, want ErrNotFound", err)
		}
	})
	t.Run("relate-and-duplicate", func(t *testing.T) {
		s := factory(t)
		a := mustUpsert(t, s, newObj("contract:rel-a", "Org", nil))
		b := mustUpsert(t, s, newObj("contract:rel-b", "Repo", nil))
		rel := knowledge.Relationship{Type: "PART_OF", From: b.Koid, To: a.Koid}
		if err := s.Relate(context.Background(), rel); err != nil {
			t.Fatal(err)
		}
		if err := s.Relate(context.Background(), rel); err != nil { // set semantics: duplicates collapse
			t.Fatalf("duplicate relate should be a no-op: %v", err)
		}
	})
	t.Run("traverse-outbound", func(t *testing.T) {
		s := factory(t)
		a := mustUpsert(t, s, newObj("contract:out-a", "Org", nil))
		b := mustUpsert(t, s, newObj("contract:out-b", "Repo", nil))
		if err := s.Relate(context.Background(), knowledge.Relationship{Type: "PART_OF", From: b.Koid, To: a.Koid}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Traverse(context.Background(), b.Koid, "PART_OF", knowledge.Outbound, 1)
		if err != nil {
			t.Fatal(err)
		}
		assertKoids(t, got, a.Koid)
	})
	t.Run("traverse-inbound", func(t *testing.T) {
		s := factory(t)
		a := mustUpsert(t, s, newObj("contract:in-a", "Org", nil))
		b := mustUpsert(t, s, newObj("contract:in-b", "Repo", nil))
		if err := s.Relate(context.Background(), knowledge.Relationship{Type: "PART_OF", From: b.Koid, To: a.Koid}); err != nil {
			t.Fatal(err)
		}
		got, err := s.Traverse(context.Background(), a.Koid, "PART_OF", knowledge.Inbound, 1)
		if err != nil {
			t.Fatal(err)
		}
		assertKoids(t, got, b.Koid)
	})
	t.Run("traverse-depth-and-cycles", func(t *testing.T) {
		s := factory(t)
		var koids []string
		for i := 0; i < 4; i++ {
			ko := mustUpsert(t, s, newObj("contract:chain-"+string(rune('a'+i)), "Node", nil))
			koids = append(koids, ko.Koid)
		}
		for i := 0; i < 3; i++ {
			if err := s.Relate(context.Background(), knowledge.Relationship{Type: "NEXT", From: koids[i], To: koids[i+1]}); err != nil {
				t.Fatal(err)
			}
		}
		// cycle n2 -> n0 must not loop forever
		if err := s.Relate(context.Background(), knowledge.Relationship{Type: "NEXT", From: koids[2], To: koids[0]}); err != nil {
			t.Fatal(err)
		}

		got, err := s.Traverse(context.Background(), koids[0], "NEXT", knowledge.Outbound, 1)
		if err != nil {
			t.Fatal(err)
		}
		assertKoids(t, got, koids[1])
		got, err = s.Traverse(context.Background(), koids[0], "NEXT", knowledge.Outbound, 2)
		if err != nil {
			t.Fatal(err)
		}
		assertKoids(t, got, koids[1], koids[2])
		got, err = s.Traverse(context.Background(), koids[0], "NEXT", knowledge.Outbound, 3)
		if err != nil {
			t.Fatal(err)
		}
		assertKoids(t, got, koids[1], koids[2], koids[3])
		if _, err := s.Traverse(context.Background(), koids[0], "NEXT", knowledge.Outbound, 0); err == nil {
			t.Error("depth 0 should error")
		}
	})
	t.Run("missing-object-behavior", func(t *testing.T) {
		s := factory(t)
		a := mustUpsert(t, s, newObj("contract:miss", "Issue", nil))
		if err := s.Relate(context.Background(), knowledge.Relationship{Type: "X", From: "ko-999", To: a.Koid}); !errors.Is(err, knowledge.ErrNotFound) {
			t.Errorf("relate unknown from: %v, want ErrNotFound", err)
		}
		if err := s.Relate(context.Background(), knowledge.Relationship{Type: "X", From: a.Koid, To: "ko-999"}); !errors.Is(err, knowledge.ErrNotFound) {
			t.Errorf("relate unknown to: %v, want ErrNotFound", err)
		}
		if _, err := s.Traverse(context.Background(), "ko-999", "X", knowledge.Outbound, 1); !errors.Is(err, knowledge.ErrNotFound) {
			t.Errorf("traverse from missing: %v, want ErrNotFound", err)
		}
	})
	t.Run("context-cancellation", func(t *testing.T) {
		s := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := s.Upsert(ctx, newObj("contract:ctx", "Issue", nil)); !errors.Is(err, context.Canceled) {
			t.Errorf("Upsert with cancelled ctx: %v, want context.Canceled", err)
		}
		if _, err := s.Get(ctx, "ko-1"); !errors.Is(err, context.Canceled) {
			t.Errorf("Get with cancelled ctx: %v, want context.Canceled", err)
		}
		if _, err := s.GetByExternalID(ctx, "ext-1"); !errors.Is(err, context.Canceled) {
			t.Errorf("GetByExternalID with cancelled ctx: %v, want context.Canceled", err)
		}
		if err := s.Relate(ctx, knowledge.Relationship{Type: "X", From: "a", To: "b"}); !errors.Is(err, context.Canceled) {
			t.Errorf("Relate with cancelled ctx: %v, want context.Canceled", err)
		}
		if _, err := s.Traverse(ctx, "ko-1", "X", knowledge.Outbound, 1); !errors.Is(err, context.Canceled) {
			t.Errorf("Traverse with cancelled ctx: %v, want context.Canceled", err)
		}
	})
}

func newObj(extID, typeName string, props map[string]any) knowledge.KnowledgeObject {
	return knowledge.KnowledgeObject{
		TypeName:   typeName,
		ExternalID: extID,
		Properties: props,
		Provenance: knowledge.Provenance{Source: "contract"},
	}
}

func mustUpsert(t *testing.T, s knowledge.KnowledgeStore, obj knowledge.KnowledgeObject) knowledge.KnowledgeObject {
	t.Helper()
	ko, err := s.Upsert(context.Background(), obj)
	if err != nil {
		t.Fatalf("upsert %s: %v", obj.ExternalID, err)
	}
	return ko
}

// assertKoids checks the traversal result as a set: store ordering (e.g.
// server-side traversal order) must not fail the contract.
func assertKoids(t *testing.T, got []knowledge.KnowledgeObject, want ...string) {
	t.Helper()
	have := make(map[string]bool, len(got))
	for _, o := range got {
		have[o.Koid] = true
	}
	for _, koid := range want {
		if !have[koid] {
			t.Errorf("missing koid %s (got %v)", koid, have)
		}
	}
	if len(have) != len(want) {
		t.Errorf("got %d objects, want %d: %v", len(have), len(want), have)
	}
}
