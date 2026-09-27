package knowledgetest

import (
	"context"
	"errors"
	"testing"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

// RunTenantContract grades a store's tenant scoping (TDD plan §8): the
// logical identity (tenant, source, external_id) must never collide across
// tenants, same-tenant upserts stay idempotent, and traversal cannot expose
// another tenant's objects.
func RunTenantContract(t *testing.T, factory StoreFactory) {
	t.Helper()
	t.Run("cross-tenant-identity", func(t *testing.T) {
		base := factory(t)
		ta := knowledge.WithTenant(base, "tenant-a")
		tb := knowledge.WithTenant(base, "tenant-b")
		// Same external ID, different tenants: two objects.
		a := mustUpsert(t, ta, newObj("github.com:user:john", "User", nil))
		b := mustUpsert(t, tb, newObj("github.com:user:john", "User", nil))
		if a.Koid == b.Koid {
			t.Errorf("same identity in different tenants must create two objects, got one koid %s", a.Koid)
		}
		ga, err := ta.GetByExternalID(context.Background(), "github.com:user:john")
		if err != nil || ga.Koid != a.Koid {
			t.Errorf("tenant-a lookup = %+v, %v; want koid %s", ga, err, a.Koid)
		}
		gb, err := tb.GetByExternalID(context.Background(), "github.com:user:john")
		if err != nil || gb.Koid != b.Koid {
			t.Errorf("tenant-b lookup = %+v, %v; want koid %s", gb, err, b.Koid)
		}
		// Scoped handles return the caller's bare ID and never the prefixed one.
		if ga.ExternalID != "github.com:user:john" {
			t.Errorf("tenant-a external ID = %q, want unscoped", ga.ExternalID)
		}
		// The unscoped base store sees neither tenant's object.
		if _, err := base.GetByExternalID(context.Background(), "github.com:user:john"); !errors.Is(err, knowledge.ErrNotFound) {
			t.Errorf("bare store lookup = %v, want ErrNotFound (tenant data must not leak)", err)
		}
	})

	t.Run("same-tenant-idempotent", func(t *testing.T) {
		s := knowledge.WithTenant(factory(t), "tenant-a")
		a := mustUpsert(t, s, newObj("tenant:same", "Issue", map[string]any{"n": float64(1)}))
		b := mustUpsert(t, s, newObj("tenant:same", "Issue", map[string]any{"n": float64(1)}))
		if a.Koid != b.Koid || b.Version != 1 {
			t.Errorf("identical same-tenant upsert changed identity: %+v then %+v", a, b)
		}
	})

	t.Run("cross-tenant-traversal-isolation", func(t *testing.T) {
		base := factory(t)
		ta := knowledge.WithTenant(base, "tenant-a")
		tb := knowledge.WithTenant(base, "tenant-b")
		// Identical external IDs and relationship type in both tenants.
		a1 := mustUpsert(t, ta, newObj("tenant:chain-0", "Node", nil))
		a2 := mustUpsert(t, ta, newObj("tenant:chain-1", "Node", nil))
		b1 := mustUpsert(t, tb, newObj("tenant:chain-0", "Node", nil))
		b2 := mustUpsert(t, tb, newObj("tenant:chain-1", "Node", nil))
		if err := ta.Relate(context.Background(), knowledge.Relationship{Type: "NEXT", From: a1.Koid, To: a2.Koid}); err != nil {
			t.Fatal(err)
		}
		if err := tb.Relate(context.Background(), knowledge.Relationship{Type: "NEXT", From: b1.Koid, To: b2.Koid}); err != nil {
			t.Fatal(err)
		}
		got, err := ta.Traverse(context.Background(), a1.Koid, "NEXT", knowledge.Outbound, 1)
		if err != nil {
			t.Fatal(err)
		}
		assertKoids(t, got, a2.Koid) // exact set: b2 leaking in fails the count
		got, err = tb.Traverse(context.Background(), b1.Koid, "NEXT", knowledge.Outbound, 1)
		if err != nil {
			t.Fatal(err)
		}
		assertKoids(t, got, b2.Koid)
	})
}
