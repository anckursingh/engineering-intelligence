// Product dashboard + board cache tests (item 31): GET / serves the embedded
// dashboard; repeated board requests within the TTL never re-walk the store
// and revalidate via ETag; window/scope changes and expiry recompute; errors
// are never cached.
package intelligence

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

// countingStore counts read calls (GetByExternalID + Traverse) through the
// Population walk, so tests can assert the cache skipped the store.
type countingStore struct {
	knowledge.KnowledgeStore
	reads int
	fail  bool
}

func (c *countingStore) GetByExternalID(ctx context.Context, externalID string) (knowledge.KnowledgeObject, error) {
	c.reads++
	if c.fail {
		return knowledge.KnowledgeObject{}, errors.New("store unavailable")
	}
	return c.KnowledgeStore.GetByExternalID(ctx, externalID)
}

func (c *countingStore) Traverse(ctx context.Context, from, relType string, dir knowledge.Direction, depth int) ([]knowledge.KnowledgeObject, error) {
	c.reads++
	return c.KnowledgeStore.Traverse(ctx, from, relType, dir, depth)
}

func TestDashboardServed(t *testing.T) {
	api := NewAPI(knowledge.NewMemory())

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "Engineering Intelligence") {
		t.Fatalf("dashboard body does not carry the product name")
	}

	// The exact-root route must not swallow API paths: unknown ones stay 404.
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /nope = %d, want 404 (dashboard route must not catch all)", rec.Code)
	}
}

func TestBoardCacheSecondRequestSkipsStore(t *testing.T) {
	store := &countingStore{KnowledgeStore: knowledge.NewMemory()}
	seedInvestigationWorld(t, store)
	api := NewAPI(store)

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("first GET /board = %d, body %s", rec.Code, rec.Body.String())
	}
	reads := store.reads
	if reads == 0 {
		t.Fatal("first request never touched the store")
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first response carries no ETag")
	}
	body := rec.Body.String()

	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("second GET /board = %d", rec.Code)
	}
	if store.reads != reads {
		t.Fatalf("second request walked the store again: reads %d -> %d (TTL cache must serve it)", reads, store.reads)
	}
	if rec.Body.String() != body || rec.Header().Get("ETag") != etag {
		t.Fatal("cached response differs from the first")
	}
}

func TestBoardCacheETagRevalidates(t *testing.T) {
	store := &countingStore{KnowledgeStore: knowledge.NewMemory()}
	seedInvestigationWorld(t, store)
	api := NewAPI(store)

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	etag := rec.Header().Get("ETag")

	req := httptest.NewRequest(http.MethodGet, boardURL, nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("304 carries a body (%d bytes)", rec.Body.Len())
	}
}

func TestBoardCacheKeyedByWindowAndScope(t *testing.T) {
	store := &countingStore{KnowledgeStore: knowledge.NewMemory()}
	seedInvestigationWorld(t, store)
	api := NewAPI(store)

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	firstETag := rec.Header().Get("ETag")
	reads := store.reads

	other := strings.Replace(boardURL, "2026-09-01", "2026-08-01", 1)
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, other, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("different window = %d", rec.Code)
	}
	if store.reads <= reads {
		t.Fatalf("different window served from cache: reads %d, want > %d", store.reads, reads)
	}
	if rec.Header().Get("ETag") == firstETag {
		t.Fatal("different window returned the same ETag")
	}
}

func TestBoardCacheExpires(t *testing.T) {
	store := &countingStore{KnowledgeStore: knowledge.NewMemory()}
	seedInvestigationWorld(t, store)
	api := NewAPI(store)
	t0 := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	api.board.now = func() time.Time { return t0 }

	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	reads := store.reads

	api.board.now = func() time.Time { return t0.Add(2 * time.Minute) } // TTL is 1m
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("post-expiry = %d", rec.Code)
	}
	if store.reads <= reads {
		t.Fatalf("expired entry served stale data: reads %d, want > %d", store.reads, reads)
	}
}

func TestBoardCacheNeverCachesErrors(t *testing.T) {
	store := &countingStore{KnowledgeStore: knowledge.NewMemory()}
	seedInvestigationWorld(t, store)
	api := NewAPI(store)

	store.fail = true
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failing store = %d, want 500", rec.Code)
	}
	reads := store.reads

	store.fail = false
	rec = httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, boardURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("retry after failure = %d, body %s", rec.Code, rec.Body.String())
	}
	if store.reads <= reads {
		t.Fatalf("error response was cached: reads %d, want > %d", store.reads, reads)
	}
}
