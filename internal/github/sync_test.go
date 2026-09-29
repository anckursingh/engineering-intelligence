// External test package: black-box over the connector's exported API, sharing
// the fake world with test/acceptance. (Package-internal tests cannot import
// githubtest — it imports github — so the unit tests stay external too.)
package github_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

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

// TestSyncUserOwner: when the owner is a personal account, the org endpoint
// 404s and the sync falls back to the user endpoint — the owner object is a
// User (never an Organization), repos still hang off it via BELONGS_TO.
func TestSyncUserOwner(t *testing.T) {
	w := githubtest.NewUserWorld(t)
	store := knowledge.NewMemory()
	cfg := w.SyncConfig(t.TempDir(), store)
	res, err := github.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Counts["Organization"]; ok {
		t.Errorf("user owner produced an Organization: %+v", res.Counts)
	}
	if got := res.Counts["User"]; got != (github.Count{New: 1}) {
		t.Errorf("User count = %+v, want 1 new", got)
	}
	if got := w.OrgCalls(); got != 1 {
		t.Errorf("org endpoint calls = %d, want 1 (the 404 that triggers the fallback)", got)
	}

	if _, err := store.GetByExternalID(context.Background(), "github.com:account:acme"); err != nil {
		t.Errorf("user account object missing: %v", err)
	}
	repo, err := store.GetByExternalID(context.Background(), "github.com:repo:acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	githubtest.AssertReaches(t, store, repo.Koid, string(ontology.RelBelongsTo), knowledge.Outbound, 1, "github.com:account:acme")
}

// TestSyncSkipsEmptyRepo: an empty repository makes GitHub's commits
// endpoint answer 409 "Git Repository is empty" instead of an empty list;
// the sync treats it as zero commits and keeps going.
func TestSyncSkipsEmptyRepo(t *testing.T) {
	w := githubtest.NewUserWorld(t)
	w.AddEmptyRepo("vacant")
	store := knowledge.NewMemory()
	res, err := github.Sync(context.Background(), w.SyncConfig(t.TempDir(), store))
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Counts["Repository"]; got != (github.Count{New: 2}) {
		t.Errorf("Repository count = %+v, want 2 (the empty repo itself is synced)", got)
	}
	if got := res.Counts["Commit"]; got != (github.Count{New: 2}) {
		t.Errorf("Commit count = %+v, want 2 (the empty repo contributes none)", got)
	}
}

// Roadmap Milestone B: the PR list endpoint leaves merge_method and
// requested_reviewers unpopulated, so the sync fetches the single-PR GET for
// each changed PR and stores the richer record — labels, draft, merge
// method, requested reviewers — alongside the list's fields.
func TestSyncPRRichMetadata(t *testing.T) {
	w := githubtest.NewWorld(t)
	store := knowledge.NewMemory()
	if _, err := github.Sync(context.Background(), w.SyncConfig(t.TempDir(), store)); err != nil {
		t.Fatal(err)
	}
	pr, err := store.GetByExternalID(context.Background(), "github.com:pr:acme/widgets#3")
	if err != nil {
		t.Fatal(err)
	}
	if got := pr.Properties["merge_method"]; got != "squash" {
		t.Errorf("merge_method = %v, want squash (single-PR GET field)", got)
	}
	if got := pr.Properties["draft"]; got != false {
		t.Errorf("draft = %v, want false", got)
	}
	if got := pr.Properties["labels"]; !reflect.DeepEqual(got, []any{"enhancement"}) {
		t.Errorf("labels = %v, want [enhancement]", got)
	}
	if got := pr.Properties["requested_reviewers"]; !reflect.DeepEqual(got, []any{"bob"}) {
		t.Errorf("requested_reviewers = %v, want [bob]", got)
	}
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

// §13.1: retry backoff must be context-interruptible — a cancelled sync
// returns promptly instead of sleeping out the backoff window.
func TestSyncContextCancelDuringRetry(t *testing.T) {
	w := githubtest.NewWorld(t)
	w.FailOrgAlways()
	cfg := w.SyncConfig(t.TempDir(), knowledge.NewMemory())
	cfg.Sleep = nil // real sleep: the interruptible path under test

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := github.Sync(ctx, cfg)
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for w.OrgCalls() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("sync did not return promptly after cancel (backoff not interruptible)")
	}
}

// §13.3: the merge-commit model must not assume one merge kind. Squash and
// rebase merges put the resulting commit on the default branch, so the same
// history lookup finds them; an unavailable merge SHA must leave no edge,
// not fail the run.
func TestMergeCommitModel(t *testing.T) {
	w := githubtest.NewWorld(t)
	w.AddMergeVariants()
	store := knowledge.NewMemory()
	if _, err := github.Sync(context.Background(), w.SyncConfig(t.TempDir(), store)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ pr, sha string }{
		{"4", githubtest.SquashSha},
		{"5", githubtest.RebaseSha},
	} {
		pr, err := store.GetByExternalID(context.Background(), "github.com:pr:acme/widgets#"+c.pr)
		if err != nil {
			t.Fatal(err)
		}
		githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelMergedAs), knowledge.Outbound, 1,
			"github.com:commit:acme/widgets@"+c.sha)
	}
	pr6, err := store.GetByExternalID(context.Background(), "github.com:pr:acme/widgets#6")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Traverse(context.Background(), pr6.Koid, string(ontology.RelMergedAs), knowledge.Outbound, 1)
	if err != nil || len(got) != 0 {
		t.Errorf("unavailable merge commit must leave no edge: %+v, %v", got, err)
	}
}

// §13.4: PR-body regex is the fallback linking path — duplicate references
// collapse to one edge, and a reference to a nonexistent issue skips the
// link without failing the run. (Authoritative linking data — GitHub
// GraphQL ClosingIssuesReferences — joins when metrics need it.)
func TestPRToIssueLinks(t *testing.T) {
	w := githubtest.NewWorld(t)
	w.AddLinkVariants()
	store := knowledge.NewMemory()
	if _, err := github.Sync(context.Background(), w.SyncConfig(t.TempDir(), store)); err != nil {
		t.Fatal(err)
	}
	dup, err := store.GetByExternalID(context.Background(), "github.com:pr:acme/widgets#7")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Traverse(context.Background(), dup.Koid, string(ontology.RelImplements), knowledge.Outbound, 1)
	if err != nil || len(got) != 1 {
		t.Errorf("duplicate references must collapse to one edge: %+v, %v", got, err)
	}
	missing, err := store.GetByExternalID(context.Background(), "github.com:pr:acme/widgets#8")
	if err != nil {
		t.Fatal(err)
	}
	got, err = store.Traverse(context.Background(), missing.Koid, string(ontology.RelImplements), knowledge.Outbound, 1)
	if err != nil || len(got) != 0 {
		t.Errorf("nonexistent referenced issue must leave no edge: %+v, %v", got, err)
	}
}

// TestSyncCI: workflow runs normalize to Build objects hanging off the repo
// (CONTAINS_BUILD), runs the API links to PRs get the HAS_BUILD edge, and the
// second run gates on the watermark — completed earlier runs skip, the
// finished in-progress run updates. The edge never claims an outcome: a
// failed run linked to a PR is still HAS_BUILD (the conclusion lives on the
// Build).
func TestSyncCI(t *testing.T) {
	w := githubtest.NewWorld(t)
	w.AddBuilds()
	store := knowledge.NewMemory()
	cfg := w.SyncConfig(t.TempDir(), store)

	res, err := github.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Counts["Build"]; got != (github.Count{New: 4}) {
		t.Errorf("Build count = %+v, want 4 new", got)
	}
	// base 9 relationships + 4 CONTAINS_BUILD + 1 HAS_BUILD (run 200 links PR #3)
	if res.Relationships != 14 {
		t.Errorf("relationships = %d, want 14", res.Relationships)
	}

	build, err := store.GetByExternalID(context.Background(), "github.com:build:acme/widgets:200")
	if err != nil {
		t.Fatal(err)
	}
	if build.Properties["conclusion"] != "success" || build.Properties["head_sha"] != githubtest.ShaB {
		t.Errorf("build 200 = %+v, want conclusion success + head sha", build.Properties)
	}
	repo, err := store.GetByExternalID(context.Background(), "github.com:repo:acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	githubtest.AssertReaches(t, store, repo.Koid, string(ontology.RelContainsBuild), knowledge.Outbound, 1,
		"github.com:build:acme/widgets:200", "github.com:build:acme/widgets:201", "github.com:build:acme/widgets:202", "github.com:build:acme/widgets:203")
	pr, err := store.GetByExternalID(context.Background(), "github.com:pr:acme/widgets#3")
	if err != nil {
		t.Fatal(err)
	}
	githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelHasBuild), knowledge.Outbound, 1, "github.com:build:acme/widgets:200")

	// between runs: run 202 completes; runs 200/201/203 predate the watermark
	w.FinishInProgressBuild()
	res2, err := github.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c := res2.Counts["Build"]; c != (github.Count{Updated: 1, Skipped: 3}) {
		t.Errorf("run2 Build = %+v, want updated 1 skipped 3", c)
	}
	build202, err := store.GetByExternalID(context.Background(), "github.com:build:acme/widgets:202")
	if err != nil {
		t.Fatal(err)
	}
	if build202.Properties["status"] != "completed" || build202.Properties["conclusion"] != "failure" {
		t.Errorf("build 202 = %+v, want completed failure", build202.Properties)
	}
}
