// External test package: black-box over the connector's exported API, sharing
// the fake world with test/acceptance. (Package-internal tests cannot import
// githubtest — it imports github — so the unit tests stay external too.)
package github_test

import (
	"context"
	"testing"

	"github.com/anckursingh/engineering-intelligence/internal/checkpoint"
	"github.com/anckursingh/engineering-intelligence/internal/github"
	"github.com/anckursingh/engineering-intelligence/internal/github/githubtest"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

func TestSyncRun1Full(t *testing.T) {
	w := githubtest.NewWorld(t)
	store := knowledge.NewMemory()
	cfg := w.SyncConfig(t.TempDir(), store)
	res, err := github.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]github.Count{
		"Organization": {New: 1},
		"Repository":   {New: 1},
		"Issue":        {New: 2},
		"Commit":       {New: 2},
		"PullRequest":  {New: 1},
		"Review":       {New: 1},
		"Engineer":     {New: 2}, // ann (login, deduped across commit/PR/review) + bob (email)
	}
	for typ, wantC := range want {
		if got := res.Counts[typ]; got != wantC {
			t.Errorf("count %s = %+v, want %+v", typ, got, wantC)
		}
	}
	if res.Relationships != 9 {
		t.Errorf("relationships = %d, want 9", res.Relationships)
	}

	// AC-KG-001 mechanics: from the PR, reach repo, issue, author, review,
	// merged commit — and from the repo, the org.
	pr, err := store.GetByExternalID(context.Background(), "github.com:pr:acme/widgets#3")
	if err != nil {
		t.Fatal(err)
	}
	githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelTargets), knowledge.Outbound, 1, "github.com:repo:acme/widgets")
	githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelImplements), knowledge.Outbound, 1, "github.com:issue:acme/widgets#1")
	githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelReviewedBy), knowledge.Outbound, 1, "github.com:user:ann")
	githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelAuthored), knowledge.Inbound, 2, "github.com:user:ann")
	githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelPartOf), knowledge.Inbound, 2,
		"github.com:review:acme/widgets#3@1001", "github.com:commit:acme/widgets@"+githubtest.ShaB)

	repo, err := store.GetByExternalID(context.Background(), "github.com:repo:acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	githubtest.AssertReaches(t, store, repo.Koid, string(ontology.RelPartOf), knowledge.Outbound, 1, "github.com:org:acme")

	// commit → author linkage works for both login and email identities
	commitA, err := store.GetByExternalID(context.Background(), "github.com:commit:acme/widgets@"+githubtest.ShaA)
	if err != nil {
		t.Fatal(err)
	}
	githubtest.AssertReaches(t, store, commitA.Koid, string(ontology.RelAuthored), knowledge.Inbound, 1, "github.com:user:ann")
	commitB, err := store.GetByExternalID(context.Background(), "github.com:commit:acme/widgets@"+githubtest.ShaB)
	if err != nil {
		t.Fatal(err)
	}
	githubtest.AssertReaches(t, store, commitB.Koid, string(ontology.RelAuthored), knowledge.Inbound, 1, "github.com:email:bob@corp.example")
}

func TestSyncRun2Delta(t *testing.T) {
	w := githubtest.NewWorld(t)
	store := knowledge.NewMemory()
	cfg := w.SyncConfig(t.TempDir(), store)

	if _, err := github.Sync(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	// between runs: issue #1 edited, issue #4 created
	w.AddDeltaActivity()

	res2, err := github.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	if c := res2.Counts["Issue"]; c != (github.Count{New: 1, Updated: 1, Skipped: 1}) {
		t.Errorf("run2 Issue = %+v, want new 1 updated 1 skipped 1", c)
	}
	if c := res2.Counts["PullRequest"]; c != (github.Count{Skipped: 1}) {
		t.Errorf("run2 PullRequest = %+v, want skipped 1", c)
	}
	if c := res2.Counts["Commit"]; c != (github.Count{}) {
		t.Errorf("run2 Commit = %+v, want zero (head unchanged)", c)
	}
	if c := res2.Counts["Organization"]; c != (github.Count{}) {
		t.Errorf("run2 Organization = %+v, want zero (unchanged)", c)
	}
	// repo PART_OF org re-relate: set semantics collapse it, but the write
	// was issued, so the call counter sees 1.
	if res2.Relationships != 1 {
		t.Errorf("run2 relationships = %d, want 1", res2.Relationships)
	}
	if w.IssueListCalls() != 2 {
		t.Errorf("issue list calls = %d, want 2 (full list each run)", w.IssueListCalls())
	}

	ck, err := checkpoint.Load(cfg.CheckpointPath)
	if err != nil {
		t.Fatal(err)
	}
	if ck.Github.Repos["acme/widgets"] != githubtest.ShaA {
		t.Errorf("head sha = %q", ck.Github.Repos["acme/widgets"])
	}
	if len(ck.Runs) != 2 {
		t.Errorf("runs = %d, want 2", len(ck.Runs))
	}
	if _, err := store.GetByExternalID(context.Background(), "github.com:issue:acme/widgets#4"); err != nil {
		t.Errorf("delta issue #4 not in store: %v", err)
	}
}

func TestSyncRateLimitRetry(t *testing.T) {
	w := githubtest.NewWorld(t)
	w.FailFirstOrgCallOnce()

	cfg := w.SyncConfig(t.TempDir(), knowledge.NewMemory())
	res, err := github.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatalf("sync should retry past the 403 rate limit: %v", err)
	}
	if w.OrgCalls() != 2 {
		t.Errorf("org calls = %d, want 2 (403 then 200)", w.OrgCalls())
	}
	if res.Counts["Organization"].New != 1 {
		t.Errorf("org not synced after retry: %+v", res.Counts["Organization"])
	}
}
