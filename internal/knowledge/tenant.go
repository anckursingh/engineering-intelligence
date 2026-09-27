package knowledge

import (
	"context"
	"strings"
)

// WithTenant scopes a store to one tenant (TDD plan §8): external IDs and
// relationship types are namespaced so (tenant, source, external_id) tuples
// never collide across tenants and traversal cannot cross a tenant boundary.
// The empty tenant returns the store unchanged. Read-back strips the prefix,
// so the scope is invisible to callers holding the scoped handle.
//
// ponytail: Get/Traverse take store koids as-is — a caller mixing scoped
// handles can still hand a foreign koid. The ingestion layer keeps one scoped
// handle per tenant, which is the only supported shape.
func WithTenant(s KnowledgeStore, tenant string) KnowledgeStore {
	if tenant == "" {
		return s
	}
	return &tenantStore{base: s, prefix: tenant + "|"}
}

type tenantStore struct {
	base   KnowledgeStore
	prefix string
}

func (t *tenantStore) scoped(id string) string { return t.prefix + id }

func (t *tenantStore) unscoped(objs []KnowledgeObject) []KnowledgeObject {
	for i := range objs {
		objs[i].ExternalID = strings.TrimPrefix(objs[i].ExternalID, t.prefix)
	}
	return objs
}

func (t *tenantStore) Upsert(ctx context.Context, obj KnowledgeObject) (KnowledgeObject, error) {
	obj.ExternalID = t.scoped(obj.ExternalID)
	res, err := t.base.Upsert(ctx, obj)
	res.ExternalID = strings.TrimPrefix(res.ExternalID, t.prefix)
	return res, err
}

func (t *tenantStore) Get(ctx context.Context, koid string) (KnowledgeObject, error) {
	res, err := t.base.Get(ctx, koid)
	res.ExternalID = strings.TrimPrefix(res.ExternalID, t.prefix)
	return res, err
}

func (t *tenantStore) GetByExternalID(ctx context.Context, externalID string) (KnowledgeObject, error) {
	res, err := t.base.GetByExternalID(ctx, t.scoped(externalID))
	res.ExternalID = strings.TrimPrefix(res.ExternalID, t.prefix)
	return res, err
}

func (t *tenantStore) Relate(ctx context.Context, rel Relationship) error {
	rel.Type = t.scoped(rel.Type)
	return t.base.Relate(ctx, rel)
}

func (t *tenantStore) Traverse(ctx context.Context, from, relType string, dir Direction, depth int) ([]KnowledgeObject, error) {
	objs, err := t.base.Traverse(ctx, from, t.scoped(relType), dir, depth)
	return t.unscoped(objs), err
}
