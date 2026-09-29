package intelligence

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// boardCache memoizes board responses keyed by scope+window. The board is
// deterministic per key, and while `ei serve` holds the db no other process
// can write it (one server per db dir), so cached bytes cannot go stale —
// ponytail: single mutex, unbounded entries; per-key locking if concurrent
// scopes ever contend. Errors are never cached (put is only called on
// success), so a transient store failure always retries the walk.
type boardCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]boardEntry
}

type boardEntry struct {
	body    []byte
	etag    string
	expires time.Time
}

func newBoardCache(ttl time.Duration) *boardCache {
	return &boardCache{ttl: ttl, now: time.Now, entries: map[string]boardEntry{}}
}

func (c *boardCache) get(key string) ([]byte, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || !c.now().Before(e.expires) {
		delete(c.entries, key)
		return nil, "", false
	}
	return e.body, e.etag, true
}

func (c *boardCache) put(key string, body []byte) string {
	sum := sha256.Sum256(body)
	etag := fmt.Sprintf(`"%x"`, sum[:8])
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = boardEntry{body: body, etag: etag, expires: c.now().Add(c.ttl)}
	return etag
}
