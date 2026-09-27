// CLI wiring tests (§38 runnable MVP): flag validation fails loudly before
// any store spawns, the store opener requires AIKOQL_MCP_BIN, and the live
// wiring is proven when the env is set.
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

func TestRunBadCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"nope"}, {"sync"}, {"ingest"}} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("run(%v) = %v, want usage error", args, err)
		}
	}
}

// TestRunRequiredFlagsBeforeStore: every missing-flag case fails BEFORE the
// store opener runs — no AIKOQL_MCP_BIN is set here, so a store attempt
// would surface a different error and prove the validation order wrong.
func TestRunRequiredFlagsBeforeStore(t *testing.T) {
	t.Setenv("AIKOQL_MCP_BIN", "")
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"sync", "github"}, "--owner is required"},
		{[]string{"sync", "github", "--owner", "o"}, "--db is required"},
		{[]string{"sync", "jira"}, "--base-url, --email, --token and --project are required"},
		{[]string{"sync", "jira", "--base-url", "u", "--email", "e", "--token", "t", "--project", "p"}, "--db is required"},
		{[]string{"ingest", "claude"}, "--dir is required"},
		{[]string{"serve"}, "--db is required"},
	}
	for _, c := range cases {
		if err := run(c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("run(%v) = %v, want %q", c.args, err, c.want)
		}
	}
}

func TestOpenStoreMissingBin(t *testing.T) {
	t.Setenv("AIKOQL_MCP_BIN", "")
	if _, _, err := openStore(t.TempDir()); err == nil || !strings.Contains(err.Error(), "AIKOQL_MCP_BIN") {
		t.Errorf("openStore without env: %v, want AIKOQL_MCP_BIN error", err)
	}
}

// TestOpenStoreLive: the CLI's store opener against the real server binary.
func TestOpenStoreLive(t *testing.T) {
	if os.Getenv("AIKOQL_MCP_BIN") == "" {
		t.Skip("AIKOQL_MCP_BIN not set")
	}
	store, closeStore, err := openStore(filepath.Join(t.TempDir(), "kb"))
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	defer closeStore()

	prov := ontology.NewProvenance("test", time.Now())
	ko, err := ontology.Organization{Login: "acme"}.KnowledgeObject(prov)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Upsert(context.Background(), ko)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := store.GetByExternalID(context.Background(), ontology.OrgExternalID("acme"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Koid != stored.Koid {
		t.Errorf("round trip koid = %q, want %q", got.Koid, stored.Koid)
	}
}
