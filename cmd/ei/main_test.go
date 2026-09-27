// CLI wiring tests (§38 runnable MVP): flag validation fails loudly before
// any store spawns, the store opener requires AIKOQL_MCP_BIN, and the live
// wiring is proven when the env is set.
package main

import (
	"bytes"
	"context"
	"encoding/json"
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

// TestRunLogShape (§33): one JSON line per ingestion run carrying the
// structured fields, and — by construction — no sensitive key can ever be
// logged: runLog has no token/secret/PII field, and the test would fail if
// one were added.
func TestRunLogShape(t *testing.T) {
	var buf bytes.Buffer
	if err := logRun(&buf, runLog{
		RunID: "r1", Source: "github", TenantID: "default",
		StartedAt:            time.Now().Add(-time.Second),
		EndedAt:              time.Now(),
		ObjectsSeen:          5,
		ObjectsCreated:       2,
		ObjectsUpdated:       1,
		ObjectsSkipped:       2,
		RelationshipsWritten: 3,
		Checkpoint:           ".ei/checkpoint.json",
	}); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not one JSON object: %v (%s)", err, buf.String())
	}
	for _, k := range []string{"run_id", "source", "tenant_id", "started_at", "ended_at",
		"objects_seen", "objects_created", "objects_updated", "objects_skipped",
		"relationships_written", "checkpoint"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %s", k, buf.String())
		}
	}
	for k := range m {
		for _, bad := range []string{"token", "secret", "password", "email", "prompt"} {
			if strings.Contains(strings.ToLower(k), bad) {
				t.Errorf("sensitive key %q present in %s", k, buf.String())
			}
		}
	}
	if _, ok := m["errors"]; ok {
		t.Error("errors key present on a clean run")
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
