// Rate limiting for the AIKOQL server's stdio policy (§32). The server
// REJECTS calls beyond its max_calls_per_minute (JSON-RPC -32000, message
// "rate limit exceeded (max N calls/min)" — dispatcher.rs) instead of
// delaying them, so the client retries rejected calls with backoff. Every
// EI tool call is idempotent (idempotency keys on remembers, version-keyed
// updates, pure reads), so a rejected call is safe to re-issue; the context
// bounds the retries. The limit itself lives server-side, so a raised or
// disabled limit (aikoql.toml) costs no sleeps at all.
package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	aikoql "github.com/ancku/aikoql-sdk"
)

const (
	rateLimitBackoffStart = time.Second
	rateLimitBackoffMax   = 8 * time.Second
)

// isRateLimit reports whether err is the server's rate-policy rejection,
// found through the SDK's %w wrapping of *aikoql.McpError.
func isRateLimit(err error) bool {
	var mcpErr *aikoql.McpError
	return errors.As(err, &mcpErr) && strings.Contains(mcpErr.Message, "rate limit exceeded")
}

// rateLimitClient wraps aikoqlDB, retrying calls the server rejects over its
// calls-per-minute policy with exponential backoff.
type rateLimitClient struct {
	db    aikoqlDB
	sleep func(ctx context.Context, d time.Duration) error
}

func (r *rateLimitClient) CallTool(ctx context.Context, name string, args any) (json.RawMessage, error) {
	backoff := rateLimitBackoffStart
	for {
		raw, err := r.db.CallTool(ctx, name, args)
		if !isRateLimit(err) {
			return raw, err
		}
		if err := r.sleep(ctx, backoff); err != nil {
			return nil, fmt.Errorf("aikoql rate limit: %w", err)
		}
		if backoff *= 2; backoff > rateLimitBackoffMax {
			backoff = rateLimitBackoffMax
		}
	}
}

func realSleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewRateLimitClient wraps a tool-call surface so calls rejected by the
// server's calls-per-minute policy are retried with backoff.
func NewRateLimitClient(db aikoqlDB) aikoqlDB {
	return &rateLimitClient{db: db, sleep: realSleep}
}
