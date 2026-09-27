package knowledge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func obj(extID string, props map[string]any) KnowledgeObject {
	return KnowledgeObject{
		TypeName:   "Issue",
		ExternalID: extID,
		Properties: props,
		Provenance: Provenance{Source: "test"},
	}
}

func TestUpsertIdempotent(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()

	a, err := m.Upsert(ctx, obj("github.com:issue:o/r#1", map[string]any{"n": float64(1)}))
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Upsert(ctx, obj("github.com:issue:o/r#1", map[string]any{"n": float64(1)}))
	if err != nil {
		t.Fatal(err)
	}
	if a.Koid != b.Koid || a.Version != 1 || b.Version != 1 {
		t.Errorf("identical upsert changed identity: %+v then %+v", a, b)
	}

	// changed properties bump the version
	c, err := m.Upsert(ctx, obj("github.com:issue:o/r#1", map[string]any{"n": float64(2)}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Koid != a.Koid || c.Version != 2 {
		t.Errorf("changed upsert = %+v, want same koid version 2", c)
	}

	// provenance-only change does not bump (ObservedAt changes every run)
	d := obj("github.com:issue:o/r#1", map[string]any{"n": float64(2)})
	d.Provenance.SourceURL = "https://new"
	e, err := m.Upsert(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if e.Version != 2 {
		t.Errorf("provenance-only change bumped version to %d", e.Version)
	}
}

func TestUpsertTypeConflict(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	if _, err := m.Upsert(ctx, obj("x:1", map[string]any{"a": "1"})); err != nil {
		t.Fatal(err)
	}
	other := obj("x:1", nil)
	other.TypeName = "Commit"
	if _, err := m.Upsert(ctx, other); !errors.Is(err, ErrTypeConflict) {
		t.Errorf("err = %v, want ErrTypeConflict", err)
	}
}

func TestGetAndGetByExternalID(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	ko, err := m.Upsert(ctx, obj("ext-1", nil))
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, ko.Koid)
	if err != nil || got.Koid != ko.Koid {
		t.Errorf("Get = %+v, %v", got, err)
	}
	got, err = m.GetByExternalID(ctx, "ext-1")
	if err != nil || got.Koid != ko.Koid {
		t.Errorf("GetByExternalID = %+v, %v", got, err)
	}
	if _, err := m.Get(ctx, "ko-999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get missing: %v, want ErrNotFound", err)
	}
	if _, err := m.GetByExternalID(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByExternalID missing: %v, want ErrNotFound", err)
	}
}

func TestRelateAndTraverse(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	a, _ := m.Upsert(ctx, obj("e-a", nil))
	b, _ := m.Upsert(ctx, obj("e-b", nil))

	rel := Relationship{Type: "PART_OF", From: b.Koid, To: a.Koid}
	if err := m.Relate(ctx, rel); err != nil {
		t.Fatal(err)
	}
	if err := m.Relate(ctx, rel); err != nil { // set semantics: duplicates collapse
		t.Fatalf("duplicate relate should be a no-op: %v", err)
	}

	out, err := m.Traverse(ctx, b.Koid, "PART_OF", Outbound, 1)
	if err != nil || len(out) != 1 || out[0].Koid != a.Koid {
		t.Errorf("outbound = %+v, %v", out, err)
	}
	in, err := m.Traverse(ctx, a.Koid, "PART_OF", Inbound, 1)
	if err != nil || len(in) != 1 || in[0].Koid != b.Koid {
		t.Errorf("inbound = %+v, %v", in, err)
	}

	if err := m.Relate(ctx, Relationship{Type: "X", From: "ko-999", To: a.Koid}); !errors.Is(err, ErrNotFound) {
		t.Errorf("relate unknown node: %v, want ErrNotFound", err)
	}
	if err := m.Relate(ctx, Relationship{Type: "X", From: a.Koid, To: "ko-999"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("relate unknown node: %v, want ErrNotFound", err)
	}
}

func TestTraverseDepthAndCycles(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	var koids []string
	for i := 0; i < 4; i++ {
		ko, err := m.Upsert(ctx, obj(fmt.Sprintf("n-%d", i), nil))
		if err != nil {
			t.Fatal(err)
		}
		koids = append(koids, ko.Koid)
	}
	for i := 0; i < 3; i++ {
		if err := m.Relate(ctx, Relationship{Type: "NEXT", From: koids[i], To: koids[i+1]}); err != nil {
			t.Fatal(err)
		}
	}
	// cycle n-2 -> n-0 must not loop forever
	if err := m.Relate(ctx, Relationship{Type: "NEXT", From: koids[2], To: koids[0]}); err != nil {
		t.Fatal(err)
	}

	out, err := m.Traverse(ctx, koids[0], "NEXT", Outbound, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Koid != koids[1] || out[1].Koid != koids[2] {
		t.Errorf("depth 2 outbound = %+v", out)
	}

	if _, err := m.Traverse(ctx, koids[0], "NEXT", Outbound, 0); err == nil {
		t.Error("depth 0 should error")
	}
	if _, err := m.Traverse(ctx, "ko-nope", "NEXT", Outbound, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("traverse missing node: %v, want ErrNotFound", err)
	}
}

func TestConcurrentUpserts(t *testing.T) {
	m := NewMemory()
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := m.Upsert(ctx, obj(fmt.Sprintf("c-%d", i), nil)); err != nil {
				t.Errorf("upsert %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.byKoid) != 20 {
		t.Errorf("want 20 objects, got %d", len(m.byKoid))
	}
}
