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
// identity and mutation ordering; provenance stamping, identity-resolution
// orchestration and checkpoint coordination join it when connectors route
// their writes through Apply.
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
	applied := make([]knowledge.KnowledgeObject, 0, len(m.Objects))
	for i, obj := range m.Objects {
		ko, err := store.Upsert(ctx, obj)
		if err != nil {
			return Result{Objects: applied}, &PartialFailure{
				Element: fmt.Sprintf("object %d", i),
				Err:     fmt.Errorf("ingestion: upsert: %w", err),
			}
		}
		applied = append(applied, ko)
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
