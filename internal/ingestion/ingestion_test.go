// External test package: graded on Memory and, live-server-gated, on
// AikoqlStore — atomicity semantics must hold on the real store (no
// transactions there), not just the in-memory double.
package ingestion_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/ingestion"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge/aikoqltest"
)

// batchStore embeds the in-memory store and records BatchUpsert calls, so
// Apply's batch fast path can be graded without a server. It never writes
// through to Memory — the tests assert that fact to prove the batch path ran.
type batchStore struct {
	*knowledge.Memory
	got []knowledge.KnowledgeObject
	err error
}

func (b *batchStore) BatchUpsert(_ context.Context, objs []knowledge.KnowledgeObject) ([]knowledge.KnowledgeObject, error) {
	b.got = objs
	if b.err != nil {
		return nil, b.err
	}
	out := make([]knowledge.KnowledgeObject, len(objs))
	copy(out, objs)
	for i := range out {
		out[i].Koid = fmt.Sprintf("k%d", i)
		out[i].Version = 1
	}
	return out, nil
}

func newStore(t *testing.T) knowledge.KnowledgeStore {
	t.Helper()
	return knowledge.NewMemory()
}

func TestIngestion(t *testing.T) {
	runIngestion(t, newStore)
}

func TestIngestionOnAikoql(t *testing.T) {
	runIngestion(t, func(t *testing.T) knowledge.KnowledgeStore {
		t.Helper()
		return aikoqltest.Live(t)
	})
}

func obj(extID, typeName string) knowledge.KnowledgeObject {
	return knowledge.KnowledgeObject{
		TypeName:   typeName,
		ExternalID: extID,
		Properties: map[string]any{"n": float64(1)},
	}
}

func runIngestion(t *testing.T, newStore func(t *testing.T) knowledge.KnowledgeStore) {
	t.Run("apply-order-and-success", func(t *testing.T) {
		s := newStore(t)
		run := ingestion.NewRun()
		if run.ID == "" || run.StartedAt.IsZero() {
			t.Fatalf("run = %+v, want ID and StartedAt set", run)
		}
		res, err := run.Apply(context.Background(), s, ingestion.Mutation{
			Objects: []knowledge.KnowledgeObject{obj("ing:a", "Issue"), obj("ing:b", "Issue")},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Objects) != 2 || res.Objects[0].Koid == "" || res.Objects[1].Koid == "" {
			t.Fatalf("res.Objects = %+v, want 2 stored objects with koids", res.Objects)
		}
		if res.Objects[0].Koid == res.Objects[1].Koid {
			t.Error("two objects share one koid")
		}
		// The stored objects carry the koids relationships reference.
		rel := knowledge.Relationship{Type: "PART_OF", From: res.Objects[1].Koid, To: res.Objects[0].Koid}
		res2, err := run.Apply(context.Background(), s, ingestion.Mutation{Relationships: []knowledge.Relationship{rel}})
		if err != nil {
			t.Fatal(err)
		}
		if res2.Relationships != 1 {
			t.Errorf("relationships = %d, want 1", res2.Relationships)
		}
		got, err := s.Traverse(context.Background(), res.Objects[1].Koid, "PART_OF", knowledge.Outbound, 1)
		if err != nil || len(got) != 1 || got[0].Koid != res.Objects[0].Koid {
			t.Errorf("traverse = %+v, %v", got, err)
		}
		if run.EndedAt.Before(run.StartedAt) {
			t.Errorf("EndedAt %v before StartedAt %v", run.EndedAt, run.StartedAt)
		}
	})

	t.Run("object-fails-relationships-unapplied", func(t *testing.T) {
		s := newStore(t)
		run := ingestion.NewRun()
		res, err := run.Apply(context.Background(), s, ingestion.Mutation{
			Objects: []knowledge.KnowledgeObject{
				obj("ing:tc", "Issue"),
				obj("ing:tc", "Commit"), // same external ID, different type
			},
			Relationships: []knowledge.Relationship{{Type: "X", From: "k1", To: "k2"}},
		})
		var pf *ingestion.PartialFailure
		if !errors.As(err, &pf) {
			t.Fatalf("err = %v, want *PartialFailure", err)
		}
		if pf.Element != "object 1" || !errors.Is(err, knowledge.ErrTypeConflict) {
			t.Errorf("failure = %+v, want element object 1 wrapping ErrTypeConflict", pf)
		}
		if len(res.Objects) != 1 || res.Relationships != 0 {
			t.Errorf("res = %+v, want 1 applied object, 0 relationships", res)
		}
		if _, err := s.GetByExternalID(context.Background(), "ing:tc"); err != nil {
			t.Errorf("first object must persist across the failure: %v", err)
		}
	})

	// §11's explicit case: object succeeds, relationship fails. Prior writes
	// persist (no transactions, no rollback) and the failure names the element.
	t.Run("relationship-fails-objects-persist", func(t *testing.T) {
		s := newStore(t)
		run := ingestion.NewRun()
		res1, err := run.Apply(context.Background(), s, ingestion.Mutation{
			Objects: []knowledge.KnowledgeObject{obj("ing:ra", "Issue"), obj("ing:rb", "Issue")},
		})
		if err != nil {
			t.Fatal(err)
		}
		res2, err := run.Apply(context.Background(), s, ingestion.Mutation{Relationships: []knowledge.Relationship{
			{Type: "PART_OF", From: res1.Objects[1].Koid, To: res1.Objects[0].Koid},
			{Type: "PART_OF", From: res1.Objects[0].Koid, To: "ko-missing"}, // fails
		}})
		var pf *ingestion.PartialFailure
		if !errors.As(err, &pf) {
			t.Fatalf("err = %v, want *PartialFailure", err)
		}
		if pf.Element != "relationship 1" || !errors.Is(err, knowledge.ErrNotFound) {
			t.Errorf("failure = %+v, want element relationship 1 wrapping ErrNotFound", pf)
		}
		if res2.Relationships != 1 {
			t.Errorf("res2.Relationships = %d, want 1 (first edge applied)", res2.Relationships)
		}
		got, err := s.Traverse(context.Background(), res1.Objects[1].Koid, "PART_OF", knowledge.Outbound, 1)
		if err != nil || len(got) != 1 || got[0].Koid != res1.Objects[0].Koid {
			t.Errorf("first edge must persist across the failure: %+v, %v", got, err)
		}
	})

	// Retry/compensation contract: after the failure cause is removed,
	// re-applying the mutation completes with earlier elements as no-ops
	// (idempotent upsert, set-semantics relate).
	t.Run("reapply-after-failure", func(t *testing.T) {
		s := newStore(t)
		run := ingestion.NewRun()
		ctx := context.Background()
		res1, err := run.Apply(ctx, s, ingestion.Mutation{
			Objects: []knowledge.KnowledgeObject{obj("ing:rr-a", "Issue"), obj("ing:rr-b", "Issue")},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = run.Apply(ctx, s, ingestion.Mutation{
			Objects:       []knowledge.KnowledgeObject{obj("ing:rr-c", "Issue")},
			Relationships: []knowledge.Relationship{{Type: "NEXT", From: res1.Objects[0].Koid, To: "ko-missing"}},
		})
		if err == nil {
			t.Fatal("apply with missing target must fail")
		}
		// Fix the cause: the missing target exists now.
		res3, err := run.Apply(ctx, s, ingestion.Mutation{Objects: []knowledge.KnowledgeObject{obj("ing:rr-target", "Issue")}})
		if err != nil {
			t.Fatal(err)
		}
		full := ingestion.Mutation{
			Objects:       []knowledge.KnowledgeObject{obj("ing:rr-a", "Issue"), obj("ing:rr-b", "Issue"), obj("ing:rr-c", "Issue")},
			Relationships: []knowledge.Relationship{{Type: "NEXT", From: res1.Objects[0].Koid, To: res3.Objects[0].Koid}},
		}
		if _, err := run.Apply(ctx, s, full); err != nil {
			t.Fatalf("re-apply after fix: %v", err)
		}
		after, err := s.GetByExternalID(ctx, "ing:rr-a")
		if err != nil {
			t.Fatal(err)
		}
		// Re-applying the same mutation again is a no-op: no version bump
		// (identical upsert) and no duplicate edge (relate collapses).
		// (A first relate may legitimately bump the FROM version server-side,
		// so the check is between identical applies.)
		if _, err := run.Apply(ctx, s, full); err != nil {
			t.Fatalf("second re-apply: %v", err)
		}
		again, err := s.GetByExternalID(ctx, "ing:rr-a")
		if err != nil {
			t.Fatal(err)
		}
		if again.Version != after.Version {
			t.Errorf("identical re-apply bumped version %d -> %d", after.Version, again.Version)
		}
		got, err := s.Traverse(ctx, res1.Objects[0].Koid, "NEXT", knowledge.Outbound, 1)
		if err != nil || len(got) != 1 {
			t.Errorf("edge after re-apply = %+v, %v; want exactly 1 (no duplicate)", got, err)
		}
	})

	t.Run("context-cancelled", func(t *testing.T) {
		s := newStore(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := ingestion.NewRun().Apply(ctx, s, ingestion.Mutation{
			Objects: []knowledge.KnowledgeObject{obj("ing:ctx", "Issue")},
		})
		var pf *ingestion.PartialFailure
		if !errors.As(err, &pf) || pf.Element != "object 0" || !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want PartialFailure object 0 wrapping context.Canceled", err)
		}
	})

	// §10: the layer owns run-scoped provenance — ObservedAt and IngestionRun
	// are stamped per object; connector-supplied values are never clobbered.
	t.Run("stamps-provenance", func(t *testing.T) {
		s := newStore(t)
		run := ingestion.NewRun()
		res, err := run.Apply(context.Background(), s, ingestion.Mutation{Objects: []knowledge.KnowledgeObject{
			{TypeName: "Issue", ExternalID: "ing:pv-a", Properties: map[string]any{"n": float64(1)},
				Provenance: knowledge.Provenance{Source: "src", SourceURL: "https://x"}},
			{TypeName: "Issue", ExternalID: "ing:pv-b", Properties: map[string]any{"n": float64(1)},
				Provenance: knowledge.Provenance{Source: "src", ObservedAt: time.Now().Add(-time.Hour), IngestionRun: "pre-stamped"}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		stamped := res.Objects[0].Provenance
		if stamped.IngestionRun != run.ID || !stamped.ObservedAt.Equal(run.StartedAt) {
			t.Errorf("stamped provenance = %+v, want run ID %s and ObservedAt %v", stamped, run.ID, run.StartedAt)
		}
		if stamped.Source != "src" || stamped.SourceURL != "https://x" {
			t.Errorf("source fields lost: %+v", stamped)
		}
		pre := res.Objects[1].Provenance
		if pre.IngestionRun != "pre-stamped" || pre.ObservedAt.IsZero() {
			t.Errorf("connector-supplied provenance was clobbered: %+v", pre)
		}
	})
}

// §32: Apply must prefer a store's batch capability — one BatchUpsert call
// for the whole mutation — and stamp run provenance before the batch write.
func TestApplyPrefersBatchUpsert(t *testing.T) {
	s := &batchStore{Memory: knowledge.NewMemory()}
	run := ingestion.NewRun()
	res, err := run.Apply(context.Background(), s, ingestion.Mutation{
		Objects: []knowledge.KnowledgeObject{obj("ing:a", "Issue"), obj("ing:b", "Issue")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.got == nil {
		t.Fatal("BatchUpsert not called; want Apply to prefer it over per-object Upsert")
	}
	if len(s.got) != 2 {
		t.Fatalf("BatchUpsert got %d objects, want 2", len(s.got))
	}
	if s.got[0].Provenance.ObservedAt.IsZero() || s.got[0].Provenance.IngestionRun != run.ID {
		t.Errorf("provenance not stamped before batch: %+v", s.got[0].Provenance)
	}
	if len(res.Objects) != 2 || res.Objects[0].Koid != "k0" || res.Objects[1].Koid != "k1" {
		t.Errorf("res.Objects = %+v, want batch koids", res.Objects)
	}
	// The per-object fallback must not have run: the fake never writes to
	// Memory, so a populated Memory would prove Upsert was called instead.
	if _, err := s.Memory.GetByExternalID(context.Background(), "ing:a"); !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("Memory was written, want batch path only: %v", err)
	}
}

func TestApplyBatchFailureKeepsElementSemantics(t *testing.T) {
	s := &batchStore{Memory: knowledge.NewMemory(),
		err: &knowledge.BatchError{Index: 1, Err: knowledge.ErrNotFound}}
	_, err := ingestion.NewRun().Apply(context.Background(), s, ingestion.Mutation{
		Objects: []knowledge.KnowledgeObject{obj("ing:a", "Issue"), obj("ing:b", "Issue")},
	})
	var pf *ingestion.PartialFailure
	if !errors.As(err, &pf) {
		t.Fatalf("err = %v, want PartialFailure", err)
	}
	if pf.Element != "object 1" {
		t.Errorf("element = %q, want %q", pf.Element, "object 1")
	}
	if !errors.Is(err, knowledge.ErrNotFound) {
		t.Errorf("err chain loses cause: %v", err)
	}
}
