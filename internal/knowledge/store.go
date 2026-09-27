// Package knowledge defines the storage contract between Engineering
// Intelligence and AIKOQL. It contains only generic database semantics —
// no engineering domain types — so the aikoql Go SDK adapter can implement
// it as a mechanical mapping to the AIKOQL tool surface:
//
//	Upsert  → remember (idempotency_key = ExternalID)
//	Get     → get
//	Relate  → relate
//	Traverse → traverse
package knowledge

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("knowledge: object not found")
	ErrTypeConflict = errors.New("knowledge: external_id already used by a different type_name")
)

// KnowledgeObject is a generic knowledge object as AIKOQL models it:
// a type name, an external identity, a flat property payload, and provenance.
type KnowledgeObject struct {
	Koid       string         `json:"koid"`        // store-assigned; empty for new objects
	TypeName   string         `json:"type_name"`   // "Organization", "Engineer", ...
	ExternalID string         `json:"external_id"` // collision-free source identity
	Properties map[string]any `json:"properties"`  // flat snake_case payload, includes "external_id"
	Provenance Provenance     `json:"provenance"`
	Version    int64          `json:"version"` // 0 for new objects
}

// Provenance records where a knowledge object came from.
type Provenance struct {
	Source           string    `json:"source"`
	SourceURL        string    `json:"source_url,omitempty"`
	ObservedAt       time.Time `json:"observed_at"`
	SourceUpdatedAt  time.Time `json:"source_updated_at"`
	IngestionRun     string    `json:"ingestion_run"`
	ConnectorVersion string    `json:"connector_version"`
	Lineage          []string  `json:"lineage,omitempty"`
}

// Relationship is a directed, typed edge between two knowledge objects.
// From and To are koids returned by Upsert.
type Relationship struct {
	Type string
	From string
	To   string
}

// Direction selects the traversal direction relative to the starting node.
type Direction int

const (
	Outbound Direction = iota // edges leaving From
	Inbound                   // edges pointing at From
)

// KnowledgeStore is the persistence contract EI codes against.
type KnowledgeStore interface {
	// Upsert is idempotent on ExternalID: it creates the object if absent
	// (Version 1) or replaces Properties/Provenance when content changed.
	Upsert(ctx context.Context, obj KnowledgeObject) (KnowledgeObject, error)
	Get(ctx context.Context, koid string) (KnowledgeObject, error)
	GetByExternalID(ctx context.Context, externalID string) (KnowledgeObject, error)
	Relate(ctx context.Context, rel Relationship) error
	// Traverse walks edges of relType up to depth hops from a starting koid.
	Traverse(ctx context.Context, from, relType string, dir Direction, depth int) ([]KnowledgeObject, error)
}
