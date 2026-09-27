// Package acceptance is the runnable gate for the MVP acceptance criteria
// (docs/MVP-ACCEPTANCE.md) that slice 1 implements. Subtests are named by
// their AC id so a failure points straight at the criterion. Black-box: only
// the exported API (github.Config/Sync/Count, knowledge, checkpoint) plus the
// shared fake world.
//
// The suite is store-agnostic: it runs against Memory (always) and against
// AikoqlStore (live server, gated on AIKOQL_MCP_BIN) to prove the storage
// contract is not a Memory convenience.
//
// Not automatable here (later slices or manual): AC-KG-002/003, all MET/INT/UI,
// AC-AQ-004 (proven by the aikoql container smoke; becomes a CI job with the
// SDK adapter).
package acceptance

import (
	"context"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/checkpoint"
	"github.com/anckursingh/engineering-intelligence/internal/github"
	"github.com/anckursingh/engineering-intelligence/internal/github/githubtest"
	"github.com/anckursingh/engineering-intelligence/internal/jira"
	"github.com/anckursingh/engineering-intelligence/internal/jira/jiratest"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge/aikoqltest"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

func TestAcceptanceSlice1(t *testing.T) {
	runAcceptance(t, func(t *testing.T) knowledge.KnowledgeStore {
		t.Helper()
		return knowledge.NewMemory()
	})
}

// TestAcceptanceSlice1Aikoql runs the same ACs against a live aikoql server
// (fresh per subtest, over stdio) â€” every acceptance criterion must hold on
// the real persistence layer, not just the in-memory double.
func TestAcceptanceSlice1Aikoql(t *testing.T) {
	runAcceptance(t, func(t *testing.T) knowledge.KnowledgeStore {
		t.Helper()
		return aikoqltest.Live(t)
	})
}

func runAcceptance(t *testing.T, newStore func(t *testing.T) knowledge.KnowledgeStore) {
	t.Run("AC-ING-001 incremental sync", func(t *testing.T) {
		w := githubtest.NewWorld(t)
		store := newStore(t)
		cfg := w.SyncConfig(t.TempDir(), store)
		if _, err := github.Sync(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		w.AddDeltaActivity()
		res, err := github.Sync(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		// The delta run creates only the new issue and updates only the edited
		// one; everything unchanged counts nothing.
		if c := res.Counts["Issue"]; c != (github.Count{New: 1, Updated: 1, Skipped: 1}) {
			t.Errorf("Issue = %+v, want new 1 updated 1 skipped 1", c)
		}
		for _, typ := range []string{"Commit", "PullRequest", "Organization", "Repository"} {
			if c := res.Counts[typ]; c.New != 0 || c.Updated != 0 {
				t.Errorf("%s = %+v, want no new/updated (incremental)", typ, c)
			}
		}
	})

	t.Run("AC-ING-002 jira incremental sync", func(t *testing.T) {
		w := jiratest.NewWorld(t)
		store := newStore(t)
		cfg := w.SyncConfig(t.TempDir(), store)
		res, err := jira.Sync(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if c := res.Counts["Issue"]; c != (jira.Count{New: 1}) {
			t.Errorf("run1 Issue = %+v, want new 1", c)
		}
		w.AddDeltaActivity()
		res2, err := jira.Sync(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if c := res2.Counts["Issue"]; c != (jira.Count{New: 1}) {
			t.Errorf("run2 Issue = %+v, want new 1 (delta only)", c)
		}
		res3, err := jira.Sync(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		for typ, c := range res3.Counts {
			if c.New != 0 || c.Updated != 0 {
				t.Errorf("%s = %+v on unchanged rerun, want zero (AC-ING-005 for jira)", typ, c)
			}
		}
	})

	t.Run("AC-ING-004 AC-REL-001 checkpoint survives failure", func(t *testing.T) {
		w := githubtest.NewWorld(t)
		store := newStore(t)
		cfg := w.SyncConfig(t.TempDir(), store)
		if _, err := github.Sync(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		before, err := checkpoint.Load(cfg.CheckpointPath)
		if err != nil {
			t.Fatal(err)
		}
		w.FailOrgAlways()
		if _, err := github.Sync(context.Background(), cfg); err == nil {
			t.Fatal("sync with a failing upstream must error")
		}
		// Connector failure must not corrupt the checkpoint or the knowledge:
		// the watermark and run history are unchanged, objects still resolve.
		after, err := checkpoint.Load(cfg.CheckpointPath)
		if err != nil {
			t.Fatal(err)
		}
		if !after.Github.UpdatedSince.Equal(before.Github.UpdatedSince) {
			t.Errorf("watermark advanced across a failed run: %v -> %v", before.Github.UpdatedSince, after.Github.UpdatedSince)
		}
		if len(after.Runs) != len(before.Runs) {
			t.Errorf("run records = %d, want %d (failed run not recorded)", len(after.Runs), len(before.Runs))
		}
		if _, err := store.GetByExternalID(context.Background(), "github.com:commit:acme/widgets@"+githubtest.ShaA); err != nil {
			t.Errorf("knowledge lost after connector failure: %v", err)
		}
	})

	t.Run("AC-ING-005 AC-REL-002 rerun is idempotent", func(t *testing.T) {
		w := githubtest.NewWorld(t)
		cfg := w.SyncConfig(t.TempDir(), newStore(t))
		if _, err := github.Sync(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		res, err := github.Sync(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		for typ, c := range res.Counts {
			if c.New != 0 || c.Updated != 0 {
				t.Errorf("%s = %+v on unchanged rerun, want zero new/updated (duplicates)", typ, c)
			}
		}
	})

	t.Run("AC-ID-001 two sources one engineer", func(t *testing.T) {
		// The identity world carries just the identity-relevant objects: the
		// live aikoql leg budgets ~120 server calls per subtest, and issues/
		// PRs/reviews add nothing to what this AC pins.
		gw := githubtest.NewIdentityWorld(t)
		store := newStore(t)
		if _, err := github.Sync(context.Background(), gw.SyncConfig(t.TempDir(), store)); err != nil {
			t.Fatal(err)
		}
		jw := jiratest.NewWorld(t)
		if _, err := jira.Sync(context.Background(), jw.SyncConfig(t.TempDir(), store)); err != nil {
			t.Fatal(err)
		}
		// bob@corp.example appears in both sources; both claims must resolve
		// to ONE canonical engineer (denormalized on the claim; the edge is
		// pinned by the identity unit tests).
		resolveTo := func(claimID string) string {
			t.Helper()
			claim, err := store.GetByExternalID(context.Background(), claimID)
			if err != nil {
				t.Fatal(err)
			}
			koid, ok := claim.Properties["canonical_engineer"].(string)
			if !ok || koid == "" {
				t.Fatalf("claim %s missing canonical_engineer: %v", claimID, claim.Properties)
			}
			return koid
		}
		gkoid := resolveTo("ei.com:identity:github:email:bob@corp.example")
		jkoid := resolveTo("ei.com:identity:jira:email:bob@corp.example")
		if gkoid != jkoid {
			t.Errorf("github claim -> %s, jira claim -> %s, want one engineer", gkoid, jkoid)
		}
	})

	t.Run("AC-ID-003 ambiguous mappings reviewable, never merged", func(t *testing.T) {
		gw := githubtest.NewIdentityWorld(t)
		store := newStore(t)
		if _, err := github.Sync(context.Background(), gw.SyncConfig(t.TempDir(), store)); err != nil {
			t.Fatal(err)
		}
		jw := jiratest.NewWorld(t)
		if _, err := jira.Sync(context.Background(), jw.SyncConfig(t.TempDir(), store)); err != nil {
			t.Fatal(err)
		}
		// john.doe@company.com exists only in Jira: a distinct engineer, never
		// silently merged with github's email-only bob.
		engineerOf := func(claimID string) string {
			t.Helper()
			claim, err := store.GetByExternalID(context.Background(), claimID)
			if err != nil {
				t.Fatal(err)
			}
			koid, ok := claim.Properties["canonical_engineer"].(string)
			if !ok || koid == "" {
				t.Fatalf("claim %s missing canonical_engineer: %v", claimID, claim.Properties)
			}
			return koid
		}
		bob := engineerOf("ei.com:identity:github:email:bob@corp.example")
		john := engineerOf("ei.com:identity:jira:email:john.doe@company.com")
		if bob == john {
			t.Errorf("ambiguous identity silently merged into %s", bob)
		}
		// AC-ID-003: the decision is reviewable — rule, confidence, timestamps.
		claim, err := store.GetByExternalID(context.Background(), "ei.com:identity:jira:email:john.doe@company.com")
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"matching_rule", "confidence", "created_at", "resolved_at", "source", "source_identity", "canonical_engineer"} {
			if _, ok := claim.Properties[f]; !ok {
				t.Errorf("claim missing reviewable field %q: %v", f, claim.Properties)
			}
		}
	})

	t.Run("AC-ID-002 identity decisions are explainable", func(t *testing.T) {
		w := githubtest.NewWorld(t)
		store := newStore(t)
		cfg := w.SyncConfig(t.TempDir(), store)
		if _, err := github.Sync(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		cases := map[string]struct{ key, rule string }{
			"github.com:user:ann":               {"login:ann", "login"},
			"github.com:email:bob@corp.example": {"email:bob@corp.example", "email"},
		}
		for extID, want := range cases {
			ko, err := store.GetByExternalID(context.Background(), extID)
			if err != nil {
				t.Fatalf("%s: %v", extID, err)
			}
			if got := ko.Properties["identity_key"]; got != want.key {
				t.Errorf("%s identity_key = %v, want %s", extID, got, want.key)
			}
			if got := ko.Properties["identity_rule"]; got != want.rule {
				t.Errorf("%s identity_rule = %v, want %s", extID, got, want.rule)
			}
		}
	})

	t.Run("AC-KG-001 PR traverses to author repo issue review", func(t *testing.T) {
		w := githubtest.NewWorld(t)
		store := newStore(t)
		cfg := w.SyncConfig(t.TempDir(), store)
		if _, err := github.Sync(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		pr, err := store.GetByExternalID(context.Background(), "github.com:pr:acme/widgets#3")
		if err != nil {
			t.Fatal(err)
		}
		githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelTargets), knowledge.Outbound, 1, "github.com:repo:acme/widgets")
		githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelImplements), knowledge.Outbound, 1, "github.com:issue:acme/widgets#1")
		githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelReviewedBy), knowledge.Outbound, 1, "github.com:user:ann")
		githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelAuthored), knowledge.Inbound, 1, "github.com:user:ann")
		githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelMergedAs), knowledge.Outbound, 1, "github.com:commit:acme/widgets@"+githubtest.ShaB)
		githubtest.AssertReaches(t, store, pr.Koid, string(ontology.RelContainsReview), knowledge.Outbound, 1, "github.com:review:acme/widgets#3@1001")
	})

	t.Run("AC-KG-004 provenance retained on every object", func(t *testing.T) {
		w := githubtest.NewWorld(t)
		store := newStore(t)
		cfg := w.SyncConfig(t.TempDir(), store)
		res, err := github.Sync(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		extIDs := []string{
			"github.com:org:acme",
			"github.com:repo:acme/widgets",
			"github.com:commit:acme/widgets@" + githubtest.ShaA,
			"github.com:commit:acme/widgets@" + githubtest.ShaB,
			"github.com:issue:acme/widgets#1",
			"github.com:issue:acme/widgets#2",
			"github.com:pr:acme/widgets#3",
			"github.com:review:acme/widgets#3@1001",
			"github.com:user:ann",
			"github.com:email:bob@corp.example",
		}
		for _, extID := range extIDs {
			ko, err := store.GetByExternalID(context.Background(), extID)
			if err != nil {
				t.Fatalf("%s: %v", extID, err)
			}
			if ko.Provenance.Source != "github" {
				t.Errorf("%s source = %q, want github", extID, ko.Provenance.Source)
			}
			if ko.Provenance.SourceURL == "" {
				t.Errorf("%s source_url empty", extID)
			}
			if ko.Provenance.IngestionRun != res.RunID {
				t.Errorf("%s ingestion_run = %q, want %q", extID, ko.Provenance.IngestionRun, res.RunID)
			}
			if ko.Provenance.ObservedAt.After(time.Now().UTC().Add(time.Minute)) {
				t.Errorf("%s observed_at in the future: %v", extID, ko.Provenance.ObservedAt)
			}
		}
	})
}
