package ontology

import (
	"reflect"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

func TestExternalIDScheme(t *testing.T) {
	ids := []string{
		OrgExternalID("acme"), UserExternalID("acme"), AccountExternalID("acme"), // same login, different type prefix
		IssueExternalID("o", "r", 7), PRExternalID("o", "r", 7), // same number, different prefix
		CommitExternalID("o", "r", "ABCDEF"), // lowercased
		ReviewExternalID("o", "r", 7, 1),
		RepoExternalID("o", "r"), BuildExternalID("o", "r", 1),
		UserEmailExternalID("a@b.c"),
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate external id %q", id)
		}
		seen[id] = true
	}
	if got := CommitExternalID("o", "r", "ABCDEF"); got != "github.com:commit:o/r@abcdef" {
		t.Errorf("CommitExternalID(%q) = %q, want lowercased sha", "ABCDEF", got)
	}
}

func TestEngineerExternalIDByRule(t *testing.T) {
	prov := NewProvenance("https://x", time.Now().UTC())

	byLogin := Engineer{Name: "A", Login: "octo", IdentityKey: "login:octo", IdentityRule: "login"}
	ko, err := byLogin.KnowledgeObject(prov)
	if err != nil {
		t.Fatal(err)
	}
	if ko.ExternalID != UserExternalID("octo") {
		t.Errorf("login engineer external id = %q", ko.ExternalID)
	}

	byEmail := Engineer{Name: "A", Email: "a@b.c", IdentityKey: "email:a@b.c", IdentityRule: "email"}
	ko2, err := byEmail.KnowledgeObject(prov)
	if err != nil {
		t.Fatal(err)
	}
	if ko2.ExternalID != UserEmailExternalID("a@b.c") {
		t.Errorf("email engineer external id = %q", ko2.ExternalID)
	}
}

// TestUserAccountKnowledgeObject: a GitHub user account (the sync owner) is
// its own type — never an Organization — with a prefix disjoint from the
// engineer-keyed github.com:user: scheme.
func TestUserAccountKnowledgeObject(t *testing.T) {
	prov := NewProvenance("https://x", time.Now().UTC())
	ko, err := User{Login: "alice", Name: "Alice", HTMLURL: "https://github.com/alice"}.KnowledgeObject(prov)
	if err != nil {
		t.Fatal(err)
	}
	if ko.TypeName != "User" {
		t.Errorf("type name = %q, want User", ko.TypeName)
	}
	if ko.ExternalID != "github.com:account:alice" {
		t.Errorf("external id = %q, want github.com:account:alice", ko.ExternalID)
	}

	engKO, err := Engineer{Login: "alice", IdentityKey: "login:alice", IdentityRule: "login"}.KnowledgeObject(prov)
	if err != nil {
		t.Fatal(err)
	}
	if ko.ExternalID == engKO.ExternalID {
		t.Errorf("user account %q collides with login-keyed engineer %q", ko.ExternalID, engKO.ExternalID)
	}
}

func TestMappingProvenanceAndProperties(t *testing.T) {
	src := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("IST", 19800)) // 06:30 UTC
	prov := NewProvenance("https://example.com/x", src)
	if prov.Source != "github" {
		t.Errorf("source = %q, want github", prov.Source)
	}
	if !prov.SourceUpdatedAt.Equal(src) {
		t.Errorf("SourceUpdatedAt = %s, want instant-equal to %s", prov.SourceUpdatedAt, src)
	}
	if prov.SourceUpdatedAt.Location() != time.UTC {
		t.Errorf("SourceUpdatedAt not UTC")
	}
	// Run-scoped fields are the ingestion layer's job (§10), not the
	// connector's: the ontology constructor must leave them unset.
	if !prov.ObservedAt.IsZero() || prov.IngestionRun != "" || prov.ConnectorVersion == "" {
		t.Errorf("provenance mis-owned: %+v", prov)
	}

	o := Organization{Login: "acme", Name: "Acme Inc", CreatedAt: src, UpdatedAt: src}
	ko, err := o.KnowledgeObject(prov)
	if err != nil {
		t.Fatal(err)
	}
	if ko.TypeName != "Organization" || ko.ExternalID != OrgExternalID("acme") {
		t.Errorf("type/ext = %q/%q", ko.TypeName, ko.ExternalID)
	}
	if ko.Properties["external_id"] != ko.ExternalID {
		t.Errorf("properties[external_id] = %v, want %q", ko.Properties["external_id"], ko.ExternalID)
	}
	if ko.Properties["name"] != "Acme Inc" {
		t.Errorf("properties[name] = %v", ko.Properties["name"])
	}
	// timestamps marshal to RFC3339 strings (not time.Time)
	if _, ok := ko.Properties["created_at"].(string); !ok {
		t.Errorf("created_at = %T, want string", ko.Properties["created_at"])
	}
	if !reflect.DeepEqual(ko.Provenance, prov) {
		t.Errorf("provenance lost in mapping: %+v != %+v", ko.Provenance, prov)
	}
	if ko.Version != 0 || ko.Koid != "" {
		t.Errorf("new object must have zero Version and empty Koid: %+v", ko)
	}
}

// AC-KG-004: every mapped type carries provenance.
func TestKnowledgeObjectCarriesProvenanceAllTypes(t *testing.T) {
	prov := NewProvenance("https://x", time.Now().UTC())
	t0 := time.Now().UTC()
	kofn := func(f func(knowledge.Provenance) (knowledge.KnowledgeObject, error)) knowledge.KnowledgeObject {
		ko, err := f(prov)
		if err != nil {
			t.Fatal(err)
		}
		if ko.Provenance.Source != "github" || ko.Provenance.ConnectorVersion == "" {
			t.Errorf("provenance not carried: %+v", ko.Provenance)
		}
		return ko
	}
	kofn(func(p knowledge.Provenance) (knowledge.KnowledgeObject, error) {
		return (Organization{Login: "a", CreatedAt: t0, UpdatedAt: t0}).KnowledgeObject(p)
	})
	kofn(func(p knowledge.Provenance) (knowledge.KnowledgeObject, error) {
		return (Repository{Owner: "a", Name: "b", DefaultBranch: "main", CreatedAt: t0, UpdatedAt: t0, PushedAt: t0}).KnowledgeObject(p)
	})
	kofn(func(p knowledge.Provenance) (knowledge.KnowledgeObject, error) {
		return (Issue{Repository: "a/b", Number: 1, Title: "x", State: "open", CreatedAt: t0, UpdatedAt: t0}).KnowledgeObject(p)
	})
	kofn(func(p knowledge.Provenance) (knowledge.KnowledgeObject, error) {
		return (Commit{Repository: "a/b", SHA: "cafe", Message: "m", AuthorName: "n", CommittedAt: t0}).KnowledgeObject(p)
	})
	kofn(func(p knowledge.Provenance) (knowledge.KnowledgeObject, error) {
		return (PullRequest{Repository: "a/b", Number: 2, Title: "x", State: "open", BaseRef: "main", HeadRef: "f", CreatedAt: t0, UpdatedAt: t0}).KnowledgeObject(p)
	})
	kofn(func(p knowledge.Provenance) (knowledge.KnowledgeObject, error) {
		return (Review{Repository: "a/b", PRNumber: 2, ID: 9, ReviewerLogin: "r", State: "APPROVED", SubmittedAt: t0}).KnowledgeObject(p)
	})
	kofn(func(p knowledge.Provenance) (knowledge.KnowledgeObject, error) {
		return (Engineer{Name: "e", Login: "l", IdentityKey: "login:l", IdentityRule: "login"}).KnowledgeObject(p)
	})
	kofn(func(p knowledge.Provenance) (knowledge.KnowledgeObject, error) {
		return (Build{Repository: "a/b", ID: 3, Name: "CI", HeadSHA: "cafe", Conclusion: "success", Status: "completed", StartedAt: t0, CompletedAt: t0}).KnowledgeObject(p)
	})
}
