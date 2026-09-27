// Live performance baselines (§32): measure, don't claim. Run:
//
//	AIKOQL_MCP_BIN=.../aikoql-mcp.exe go test ./internal/knowledge -run '^$' -bench BenchmarkAikoql -benchtime=1x
//
// Results are recorded in TESTING.md §Performance baselines.
package knowledge_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ancku/aikoql-sdk"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

func benchStore(b *testing.B) (*aikoql.Client, knowledge.KnowledgeStore) {
	b.Helper()
	bin := os.Getenv("AIKOQL_MCP_BIN")
	if bin == "" {
		b.Skip("AIKOQL_MCP_BIN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	b.Cleanup(cancel)
	c, err := aikoql.DialStdio(ctx, bin, "serve", filepath.Join(b.TempDir(), "kb"))
	if err != nil {
		b.Fatalf("DialStdio: %v", err)
	}
	b.Cleanup(func() { c.Close() })
	if err := c.Initialize(ctx); err != nil {
		b.Fatalf("Initialize: %v", err)
	}
	return c, knowledge.NewAikoql(c)
}

func benchObj(i int) knowledge.KnowledgeObject {
	return knowledge.KnowledgeObject{
		TypeName:   "Bench",
		ExternalID: fmt.Sprintf("bench:obj:%d", i),
		Properties: map[string]any{"n": float64(i), "label": "object"},
		Provenance: knowledge.Provenance{Source: "bench", ObservedAt: time.Now()},
	}
}

// Upsert cost includes the index lookup + defensive get + index write:
// 4 server round trips per create, 2-3 per no-op rerun.
func BenchmarkAikoqlUpsert(b *testing.B) {
	_, s := benchStore(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Upsert(ctx, benchObj(i)); err != nil {
			b.Fatal(err)
		}
	}
}

// Setup sizes stay under the server's fixed 120 calls/min stdio rate limit
// (create = 4 calls, relate = 1): 20 objects ≈ 80 calls + 19 relates.
const benchSetup = 20

func BenchmarkAikoqlGetByExternalID(b *testing.B) {
	_, s := benchStore(b)
	ctx := context.Background()
	for i := 0; i < benchSetup; i++ {
		if _, err := s.Upsert(ctx, benchObj(i)); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.GetByExternalID(ctx, fmt.Sprintf("bench:obj:%d", i%benchSetup)); err != nil {
			b.Fatal(err)
		}
	}
}

// Directed BFS over a 20-node chain: ~4 depth-1 traverses + 3 gets.
func BenchmarkAikoqlTraverseDepth3(b *testing.B) {
	_, s := benchStore(b)
	ctx := context.Background()
	var koids []string
	for i := 0; i < benchSetup; i++ {
		ko, err := s.Upsert(ctx, benchObj(i))
		if err != nil {
			b.Fatal(err)
		}
		koids = append(koids, ko.Koid)
	}
	for i := 0; i < benchSetup-1; i++ {
		if err := s.Relate(ctx, knowledge.Relationship{Type: "NEXT", From: koids[i], To: koids[i+1]}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Traverse(ctx, koids[0], "NEXT", knowledge.Outbound, 3); err != nil {
			b.Fatal(err)
		}
	}
}
