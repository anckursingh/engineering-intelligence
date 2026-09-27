// External test package: the contract suite (knowledgetest) imports
// knowledge, so Memory's tests stay external too (see sync_test.go).
package knowledge_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge/knowledgetest"
)

func obj(extID string, props map[string]any) knowledge.KnowledgeObject {
	return knowledge.KnowledgeObject{
		TypeName:   "Issue",
		ExternalID: extID,
		Properties: props,
		Provenance: knowledge.Provenance{Source: "test"},
	}
}

// Memory is graded against the frozen KnowledgeStore contract (Phase 1B);
// AikoqlStore must pass the same suite.
func TestMemoryContract(t *testing.T) {
	knowledgetest.RunContract(t, func(t *testing.T) knowledge.KnowledgeStore {
		t.Helper()
		return knowledge.NewMemory()
	})
}

// Store-specific guarantee beyond the portable contract: provenance-only
// changes never bump the version (ObservedAt changes every run) — content
// identity is the property payload.
func TestProvenanceOnlyChangeKeepsVersion(t *testing.T) {
	m := knowledge.NewMemory()
	ctx := context.Background()
	if _, err := m.Upsert(ctx, obj("github.com:issue:o/r#1", map[string]any{"n": float64(2)})); err != nil {
		t.Fatal(err)
	}
	changed := obj("github.com:issue:o/r#1", map[string]any{"n": float64(2)})
	changed.Provenance.SourceURL = "https://new"
	got, err := m.Upsert(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 {
		t.Errorf("provenance-only change bumped version to %d", got.Version)
	}
}

func TestConcurrentUpserts(t *testing.T) {
	m := knowledge.NewMemory()
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
	for i := 0; i < 20; i++ {
		if _, err := m.GetByExternalID(ctx, fmt.Sprintf("c-%d", i)); err != nil {
			t.Errorf("concurrent upsert %d lost: %v", i, err)
		}
	}
}
