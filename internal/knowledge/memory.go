package knowledge

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// Memory is an in-process KnowledgeStore used for development and tests
// until the aikoql Go SDK adapter lands. Nothing persists — it is a test
// double, not a second knowledge store.
//
// ponytail: one global mutex; per-key locks if throughput ever matters.
type Memory struct {
	mu      sync.Mutex
	seq     int64
	byKoid  map[string]KnowledgeObject
	byExtID map[string]string                         // externalID → koid
	adj     map[string]map[string]map[string]struct{} // from → relType → to
	rev     map[string]map[string]map[string]struct{} // to → relType → from
}

func NewMemory() *Memory {
	return &Memory{
		byKoid:  make(map[string]KnowledgeObject),
		byExtID: make(map[string]string),
		adj:     make(map[string]map[string]map[string]struct{}),
		rev:     make(map[string]map[string]map[string]struct{}),
	}
}

func (m *Memory) Upsert(ctx context.Context, obj KnowledgeObject) (KnowledgeObject, error) {
	if err := ctx.Err(); err != nil {
		return KnowledgeObject{}, fmt.Errorf("knowledge: upsert aborted: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if koid, ok := m.byExtID[obj.ExternalID]; ok {
		existing := m.byKoid[koid]
		if existing.TypeName != obj.TypeName {
			return KnowledgeObject{}, fmt.Errorf("%w: %q is %s", ErrTypeConflict, obj.ExternalID, existing.TypeName)
		}
		// Content identity is the property payload; provenance is metadata.
		// ObservedAt changes every run, so comparing it would bump versions forever.
		if reflect.DeepEqual(existing.Properties, obj.Properties) {
			return existing, nil
		}
		obj.Koid = existing.Koid
		obj.Version = existing.Version + 1
		m.byKoid[koid] = obj
		return obj, nil
	}

	m.seq++
	obj.Koid = fmt.Sprintf("ko-%d", m.seq)
	obj.Version = 1
	m.byKoid[obj.Koid] = obj
	m.byExtID[obj.ExternalID] = obj.Koid
	return obj, nil
}

func (m *Memory) Get(ctx context.Context, koid string) (KnowledgeObject, error) {
	if err := ctx.Err(); err != nil {
		return KnowledgeObject{}, fmt.Errorf("knowledge: get aborted: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	obj, ok := m.byKoid[koid]
	if !ok {
		return KnowledgeObject{}, fmt.Errorf("knowledge: get %s: %w", koid, ErrNotFound)
	}
	return obj, nil
}

func (m *Memory) GetByExternalID(ctx context.Context, externalID string) (KnowledgeObject, error) {
	if err := ctx.Err(); err != nil {
		return KnowledgeObject{}, fmt.Errorf("knowledge: get by external id aborted: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	koid, ok := m.byExtID[externalID]
	if !ok {
		return KnowledgeObject{}, fmt.Errorf("knowledge: external id %s: %w", externalID, ErrNotFound)
	}
	return m.byKoid[koid], nil
}

func (m *Memory) Relate(ctx context.Context, rel Relationship) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("knowledge: relate aborted: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, koid := range []string{rel.From, rel.To} {
		if _, ok := m.byKoid[koid]; !ok {
			return fmt.Errorf("knowledge: relate %s: %w", koid, ErrNotFound)
		}
	}
	add := func(idx map[string]map[string]map[string]struct{}, a, b string) {
		if idx[a] == nil {
			idx[a] = make(map[string]map[string]struct{})
		}
		if idx[a][rel.Type] == nil {
			idx[a][rel.Type] = make(map[string]struct{})
		}
		idx[a][rel.Type][b] = struct{}{}
	}
	add(m.adj, rel.From, rel.To)
	add(m.rev, rel.To, rel.From)
	return nil
}

func (m *Memory) Traverse(ctx context.Context, from, relType string, dir Direction, depth int) ([]KnowledgeObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: traverse aborted: %w", err)
	}
	if depth < 1 {
		return nil, fmt.Errorf("knowledge: traverse depth must be >= 1, got %d", depth)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byKoid[from]; !ok {
		return nil, fmt.Errorf("knowledge: traverse from %s: %w", from, ErrNotFound)
	}

	idx := m.adj
	if dir == Inbound {
		idx = m.rev
	}
	seen := map[string]struct{}{from: {}}
	var result []KnowledgeObject
	frontier := []string{from}
	for hop := 0; hop < depth; hop++ {
		var next []string
		for _, koid := range frontier {
			for to := range idx[koid][relType] {
				if _, ok := seen[to]; ok {
					continue
				}
				seen[to] = struct{}{}
				result = append(result, m.byKoid[to])
				next = append(next, to)
			}
		}
		frontier = next
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Koid < result[j].Koid })
	return result, nil
}
