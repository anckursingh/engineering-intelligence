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
	githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelMergedAs), knowledge.Outbound, 1, "github.com:commit:acme/widgets@"+githubtest.ShaB)
	githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelContainsReview), knowledge.Outbound, 1, "github.com:review:acme/widgets#3@1001")

	repo, err := store.GetByExternalID(context.Background(), "github.com:repo:acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	githubtest.AssertReaches(t, store, repo.Koid, string(ontology.RelBelongsTo), knowledge.Outbound, 1, "github.com:org:acme")

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

// §8: identity resolution is tenant-scoped — the same source identity
// observed under two tenants resolves to two distinct engineers, and each
// tenant's graph never exposes the other's objects.
func TestTenantScopedIdentityResolution(t *testing.T) {
	w := githubtest.NewWorld(t)
	base := knowledge.NewMemory()
	cfgA := w.SyncConfig(t.TempDir(), base)
	cfgA.Tenant = "tenant-a"
	cfgB := w.SyncConfig(t.TempDir(), base)
	cfgB.Tenant = "tenant-b"
	if _, err := github.Sync(context.Background(), cfgA); err != nil {
		t.Fatal(err)
	}
	if _, err := github.Sync(context.Background(), cfgB); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ta := knowledge.WithTenant(base, "tenant-a")
	tb := knowledge.WithTenant(base, "tenant-b")
	ea, err := ta.GetByExternalID(ctx, "github.com:email:bob@corp.example")
	if err != nil {
		t.Fatalf("tenant-a engineer: %v", err)
	}
	eb, err := tb.GetByExternalID(ctx, "github.com:email:bob@corp.example")
	if err != nil {
		t.Fatalf("tenant-b engineer: %v", err)
	}
	if ea.Koid == eb.Koid {
		t.Error("same identity under two tenants must resolve to two engineers")
	}
	for _, e := range []knowledge.KnowledgeObject{ea, eb} {
		if e.Properties["identity_key"] != "email:bob@corp.example" {
			t.Errorf("identity_key = %v, want email:bob@corp.example", e.Properties["identity_key"])
		}
	}

	// Each tenant's PR graph reaches its own reviewer only: same rel type,
	// same login, different scopes.
	pa, err := ta.GetByExternalID(ctx, "github.com:pr:acme/widgets#3")
	if err != nil {
		t.Fatal(err)
	}
	githubtest.AssertReaches(t, ta, pa.Koid, string(ontology.RelReviewedBy), knowledge.Outbound, 1, "github.com:user:ann")
	got, err := ta.Traverse(ctx, pa.Koid, string(ontology.RelReviewedBy), knowledge.Outbound, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("tenant-a reviewers = %d objects, want 1 (cross-tenant leak)", len(got))
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
