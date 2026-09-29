// Package ingestion is the application-level ingestion boundary (§10-11):
// connectors produce normalized records as Mutations; a Run applies them to
// a KnowledgeStore in order, with explicit, tested failure semantics.
//
// Atomicity is defined, never assumed (§11): stores have no transactions
// (AIKOQL's MCP surface has none), so a failed Apply rolls nothing back.
// Elements applied before the failure persist; the compensation contract is
// re-apply — Upsert idempotency and relate set semantics make re-applying a
// mutation safe once the failure cause is gone.
package ingestion

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

// Mutation is one batch of knowledge writes (§11): objects first, then
// relationships. Generic by contract — no domain-specific fields.
type Mutation struct {
	Objects       []knowledge.KnowledgeObject
	Relationships []knowledge.Relationship
}

// Result reports what one Apply did before success or the first failure:
// the applied objects as stored (koids assigned) and the relationship count.
type Result struct {
	Objects       []knowledge.KnowledgeObject
	Relationships int
}

// PartialFailure reports the first failed element of an Apply. Elements
// applied before it persist (no rollback).
type PartialFailure struct {
	Element string // "object N" or "relationship N", zero-indexed
	Err     error
}

func (p *PartialFailure) Error() string { return fmt.Sprintf("ingestion: %s: %v", p.Element, p.Err) }
func (p *PartialFailure) Unwrap() error { return p.Err }

// Run identifies one ingestion run and applies its mutations. It owns run
// identity, mutation ordering and run-scoped provenance stamping;
// identity-resolution orchestration and checkpoint coordination stay with
// the connectors until a second source makes them shared.
type Run struct {
	ID        string
	StartedAt time.Time
	EndedAt   time.Time
}

// NewRun starts a run; ID matches the connector's existing run-ID format.
func NewRun() *Run {
	start := time.Now().UTC()
	return &Run{ID: start.Format("20060102T150405Z"), StartedAt: start}
}

// Apply writes one mutation in order: all objects, then all relationships.
// Relationships may reference objects from this mutation (koids come back in
// Result.Objects of the mutation that carried them) or earlier ones, never
// later. The first failing element stops the mutation and is reported as
// *PartialFailure.
func (r *Run) Apply(ctx context.Context, store knowledge.KnowledgeStore, m Mutation) (Result, error) {
	defer func() { r.EndedAt = time.Now().UTC() }()
	objs := make([]knowledge.KnowledgeObject, len(m.Objects))
	copy(objs, m.Objects)
	for i := range objs {
		// §10: the layer owns run-scoped provenance — fill, never clobber.
		if objs[i].Provenance.ObservedAt.IsZero() {
			objs[i].Provenance.ObservedAt = r.StartedAt
		}
		if objs[i].Provenance.IngestionRun == "" {
			objs[i].Provenance.IngestionRun = r.ID
		}
	}
	applied, err := r.applyObjects(ctx, store, objs)
	if err != nil {
		return Result{Objects: applied}, err
	}
	for i, rel := range m.Relationships {
		if err := store.Relate(ctx, rel); err != nil {
			return Result{Objects: applied, Relationships: i}, &PartialFailure{
				Element: fmt.Sprintf("relationship %d", i),
				Err:     fmt.Errorf("ingestion: relate: %w", err),
			}
		}
	}
	return Result{Objects: applied, Relationships: len(m.Relationships)}, nil
}

// applyObjects writes the mutation's objects. Stores with a batch capability
// get one call for the whole set; the per-object loop is the fallback. Both
// paths share order, idempotency and first-failure semantics, so the
// compensation contract (re-apply) is unchanged.
func (r *Run) applyObjects(ctx context.Context, store knowledge.KnowledgeStore, objs []knowledge.KnowledgeObject) ([]knowledge.KnowledgeObject, error) {
	if bs, ok := store.(knowledge.BatchUpserter); ok {
		applied, err := bs.BatchUpsert(ctx, objs)
		if err == nil {
			return applied, nil
		}
		var be *knowledge.BatchError
		if errors.As(err, &be) {
			return applied, &PartialFailure{
				Element: fmt.Sprintf("object %d", be.Index),
				Err:     fmt.Errorf("ingestion: upsert: %w", be.Err),
			}
		}
		return applied, &PartialFailure{
			Element: "objects",
			Err:     fmt.Errorf("ingestion: upsert: %w", err),
		}
	}
	applied := make([]knowledge.KnowledgeObject, 0, len(objs))
	for i, obj := range objs {
		ko, err := store.Upsert(ctx, obj)
		if err != nil {
			return applied, &PartialFailure{
				Element: fmt.Sprintf("object %d", i),
				Err:     fmt.Errorf("ingestion: upsert: %w", err),
			}
		}
		applied = append(applied, ko)
	}
	return applied, nil
}
