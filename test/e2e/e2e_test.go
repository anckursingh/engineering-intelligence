// Package e2e runs the live GitHub end-to-end test: connector, identity,
// ontology, store, checkpoint persistence across two real syncs.
package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anckursingh/engineering-intelligence/internal/checkpoint"
	"github.com/anckursingh/engineering-intelligence/internal/github"
)

// TestLiveE2E runs a real synchronization against live GitHub, end to end:
// connector, identity, ontology, store, checkpoint persistence across two
// runs. Off by default — set EI_E2E_OWNER and EI_E2E_REPO (and optionally
// GITHUB_TOKEN; unauthenticated gets 60 req/hr).
//
// Pick a small, dormant repo (e.g. go-playground/colors) so the second run's
// "head unchanged → zero commits" assertion cannot flake.
func TestLiveE2E(t *testing.T) {
	owner, repo := os.Getenv("EI_E2E_OWNER"), os.Getenv("EI_E2E_REPO")
	if owner == "" || repo == "" {
		t.Skip("live E2E: set EI_E2E_OWNER and EI_E2E_REPO (e.g. go-playground / colors); GITHUB_TOKEN optional")
	}

	dir := t.TempDir()
	cfg := github.Config{
		Owner:          owner,
		Repos:          []string{repo},
		CheckpointPath: filepath.Join(dir, "checkpoint.json"),
		Token:          os.Getenv("GITHUB_TOKEN"),
	}

	res1, err := github.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if res1.Counts["Commit"].New == 0 {
		t.Errorf("run 1 ingested no commits for %s/%s", owner, repo)
	}

	res2, err := github.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if c := res2.Counts["Commit"]; c != (github.Count{}) {
		t.Errorf("run 2 Commit = %+v, want zero (head unchanged)", c)
	}

	ck, err := checkpoint.Load(cfg.CheckpointPath)
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if len(ck.Runs) != 2 {
		t.Errorf("run records = %d, want 2", len(ck.Runs))
	}
	if ck.Github.UpdatedSince.IsZero() {
		t.Error("checkpoint watermark zero after successful runs")
	}
	// Atomic write: no temp files may survive.
	tmp, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil || len(tmp) != 0 {
		t.Errorf("leftover tmp files after save: %v (err %v)", tmp, err)
	}
}
