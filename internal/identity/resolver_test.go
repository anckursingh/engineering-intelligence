package identity

import (
	"context"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/ingestion"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// claimEngineer reads a claim's denormalized canonical_engineer koid — the
// reuse path the resolver itself uses.
func claimEngineer(t *testing.T, store knowledge.KnowledgeStore, claimID string) string {
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

// claimTargetViaEdge follows the RESOLVES_TO graph edge — the graph truth the
// denormalized property must agree with. Kept as one pin so the edge itself
// never rots silently.
func claimTargetViaEdge(t *testing.T, store knowledge.KnowledgeStore, claimID string) string {
	t.Helper()
	claim, err := store.GetByExternalID(context.Background(), claimID)
	if err != nil {
		t.Fatal(err)
	}
	engs, err := store.Traverse(context.Background(), claim.Koid, string(ontology.RelResolvesTo), knowledge.Outbound, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(engs) != 1 {
		t.Fatalf("claim %s resolves to %d engineers, want 1", claimID, len(engs))
	}
	return engs[0].Koid
}

func newResolver(store knowledge.KnowledgeStore, source, site string) *Resolver {
	return NewResolver(store, ingestion.NewRun(), source, site)
}

func TestResolveGithubLogin(t *testing.T) {
	store := knowledge.NewMemory()
	r := newResolver(store, "github", "")
	koid, created, err := r.Resolve(context.Background(), Resolve("Ann", "ann@corp.example", "ann"), ontology.NewProvenance("https://github.com/ann", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	if koid == "" || !created {
		t.Fatalf("koid=%q created=%v, want a new engineer", koid, created)
	}
	if got := claimTargetViaEdge(t, store, ClaimExternalID("github", "login:ann")); got != koid {
		t.Errorf("claim edge resolves to %s, want %s", got, koid)
	}
	ko, err := store.GetByExternalID(context.Background(), ClaimExternalID("github", "login:ann"))
	if err != nil {
		t.Fatal(err)
	}
	if ko.Properties["matching_rule"] != "login" || ko.Properties["confidence"] != ConfLogin {
		t.Errorf("claim = %v, want rule login, confidence %v", ko.Properties, ConfLogin)
	}
}

func TestResolveGithubNoreply(t *testing.T) {
	store := knowledge.NewMemory()
	r := newResolver(store, "github", "")
	p := Resolve("Ann", "1234+ann@users.noreply.github.com", "")
	if p.Rule != "github_noreply" || p.Key != "login:ann" {
		t.Fatalf("noreply resolution = %+v, want key login:ann", p)
	}
	koid, _, err := r.Resolve(context.Background(), p, ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	if got := claimEngineer(t, store, ClaimExternalID("github", "login:ann")); got != koid {
		t.Errorf("noreply claim resolves to %s, want %s", got, koid)
	}
}

func TestResolveEmail(t *testing.T) {
	store := knowledge.NewMemory()
	r := newResolver(store, "github", "")
	koid, _, err := r.Resolve(context.Background(), Resolve("Bob", "bob@corp.example", ""), ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	if got := claimEngineer(t, store, ClaimExternalID("github", "email:bob@corp.example")); got != koid {
		t.Errorf("email claim resolves to %s, want %s", got, koid)
	}
}

func TestResolveUnknownIdentity(t *testing.T) {
	store := knowledge.NewMemory()
	r := newResolver(store, "github", "")
	koid, created, err := r.Resolve(context.Background(), Resolve("Ghost", "", ""), ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	if koid != "" || created {
		t.Errorf("ghost identity resolved to %q (created=%v), want unresolved", koid, created)
	}
}

// TestResolveSameIdentityAcrossRuns pins §9: two runs (fresh Runs = fresh
// provenance) must land on the same canonical engineer.
func TestResolveSameIdentityAcrossRuns(t *testing.T) {
	store := knowledge.NewMemory()
	koid1, _, err := newResolver(store, "github", "").Resolve(context.Background(), Resolve("Bob", "bob@corp.example", ""), ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	koid2, created2, err := newResolver(store, "github", "").Resolve(context.Background(), Resolve("Bob", "bob@corp.example", ""), ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	if created2 {
		t.Error("second run created a duplicate engineer")
	}
	if koid2 != koid1 {
		t.Errorf("same identity across runs = %s then %s, want same koid", koid1, koid2)
	}
}

// TestResolveCrossSourceSameEmail pins AC-ID-001: GitHub and Jira with the
// same email map to ONE canonical engineer.
func TestResolveCrossSourceSameEmail(t *testing.T) {
	store := knowledge.NewMemory()
	gkoid, _, err := newResolver(store, "github", "").Resolve(context.Background(), Resolve("Bob", "bob@corp.example", ""), ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	jkoid, _, err := newResolver(store, "jira", "corp.atlassian.net").Resolve(context.Background(), Resolve("Bob", "bob@corp.example", ""), ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	if jkoid != gkoid {
		t.Errorf("cross-source same email = %s (github) vs %s (jira), want one engineer", gkoid, jkoid)
	}
	if got := claimEngineer(t, store, ClaimExternalID("jira", "email:bob@corp.example")); got != gkoid {
		t.Errorf("jira claim resolves to %s, want %s", got, gkoid)
	}
}

// TestResolveConflictingNeverMerges pins §15 + AC-ID-003: different emails
// are different identities — no silent merge, and each decision is recorded
// with its rule, confidence and timestamps so it can be reviewed.
func TestResolveConflictingNeverMerges(t *testing.T) {
	store := knowledge.NewMemory()
	gkoid, _, err := newResolver(store, "github", "").Resolve(context.Background(), Resolve("John Doe", "john@company.com", ""), ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	jkoid, _, err := newResolver(store, "jira", "corp.atlassian.net").Resolve(context.Background(), Resolve("John Doe", "john.doe@company.com", ""), ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	if jkoid == gkoid {
		t.Errorf("ambiguous identity silently merged: %s == %s", gkoid, jkoid)
	}
	// AC-ID-003: the ambiguous mapping is reviewable — explainable fields.
	ko, err := store.GetByExternalID(context.Background(), ClaimExternalID("jira", "email:john.doe@company.com"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"matching_rule", "confidence", "created_at", "resolved_at", "source", "source_identity"} {
		if _, ok := ko.Properties[f]; !ok {
			t.Errorf("claim missing reviewable field %q: %v", f, ko.Properties)
		}
	}
}

// TestResolveTenantIsolation pins §15: the same source identity in two
// tenants is two engineers.
func TestResolveTenantIsolation(t *testing.T) {
	base := knowledge.NewMemory()
	koid1, _, err := newResolver(knowledge.WithTenant(base, "t1"), "github", "").Resolve(context.Background(), Resolve("Bob", "bob@corp.example", ""), ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	koid2, _, err := newResolver(knowledge.WithTenant(base, "t2"), "github", "").Resolve(context.Background(), Resolve("Bob", "bob@corp.example", ""), ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	if koid1 == koid2 {
		t.Errorf("tenants share an engineer: %s", koid1)
	}
}

// countingStore counts GetByExternalID calls. The live acceptance leg's
// aikoql call budget (120/min per server) depends on the resolver's run-local
// cache and email-only probing, so both get pinned here.
type countingStore struct {
	knowledge.KnowledgeStore
	byExtID int
}

func (c *countingStore) GetByExternalID(ctx context.Context, externalID string) (knowledge.KnowledgeObject, error) {
	c.byExtID++
	return c.KnowledgeStore.GetByExternalID(ctx, externalID)
}

// TestResolveWithinRunUsesCache: a second resolution of the same key in one
// run costs zero store lookups (the claim is determined by the key exactly).
// A login key also proves no cross-source probe fired (probing is email-only).
func TestResolveWithinRunUsesCache(t *testing.T) {
	store := &countingStore{KnowledgeStore: knowledge.NewMemory()}
	r := newResolver(store, "github", "")
	p := Resolve("Ann", "", "ann")
	koid1, _, err := r.Resolve(context.Background(), p, ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	koid2, created2, err := r.Resolve(context.Background(), p, ontology.NewProvenance("u", zeroTime()))
	if err != nil {
		t.Fatal(err)
	}
	if koid2 != koid1 || created2 {
		t.Errorf("cached resolution = %s (created=%v), want %s (false)", koid2, created2, koid1)
	}
	if store.byExtID != 1 {
		t.Errorf("GetByExternalID = %d calls, want 1 (cache + login rule skip the probe)", store.byExtID)
	}
}

func zeroTime() (t time.Time) { return t }
