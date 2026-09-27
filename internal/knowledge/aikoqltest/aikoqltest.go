// Package aikoqltest spawns live per-test aikoql servers over stdio for
// store-graded suites (contract, acceptance, ingestion). Gated on
// AIKOQL_MCP_BIN; every test skips offline (CI sets it; see TESTING.md).
package aikoqltest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ancku/aikoql-sdk"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

// Live returns a KnowledgeStore backed by a fresh aikoql server process on a
// temp database; the server closes when the test ends.
func Live(t testing.TB) knowledge.KnowledgeStore {
	t.Helper()
	bin := os.Getenv("AIKOQL_MCP_BIN")
	if bin == "" {
		t.Skip("AIKOQL_MCP_BIN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	dbDir := filepath.Join(t.TempDir(), "kb") // non-existent: auto-created by the server
	c, err := aikoql.DialStdio(ctx, bin, "serve", dbDir)
	if err != nil {
		t.Fatalf("DialStdio: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	return knowledge.NewAikoql(c)
}
