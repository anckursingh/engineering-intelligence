// Pacing for the AIKOQL server's stdio rate policy (§32). The server
// rejects calls beyond max_calls_per_minute instead of delaying them, so
// the client spaces tool calls with a token bucket.
//
// ponytail: 120 is the server default and is not advertised on the wire;
// make it configurable when a server with a different limit appears.
package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

const defaultCallsPerMinute = 120

// rateLimiter is a token bucket: cap tokens burst, refilled at rate per
// second. Wait consumes one token, sleeping across the bucket's refill
// when it is empty.
type rateLimiter struct {
	mu    sync.Mutex
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error
	cap   float64
	rate  float64 // tokens per second
	avail float64
	last  time.Time
}

func (l *rateLimiter) Wait(ctx context.Context) error {
	for {
		wait, ok := l.take()
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := l.sleep(ctx, wait); err != nil {
			return err
		}
	}
}

// take consumes one token if available, otherwise returns how long to wait
// for the next one (0, true means "token taken, go").
func (l *rateLimiter) take() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.avail += now.Sub(l.last).Seconds() * l.rate
	if l.avail > l.cap {
		l.avail = l.cap
	}
	l.last = now
	if l.avail >= 1 {
		l.avail--
		return 0, true
	}
	return time.Duration((1 - l.avail) / l.rate * float64(time.Second)), false
}

func realSleep(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// pacedClient wraps aikoqlDB, consuming one token per tool call.
type pacedClient struct {
	db aikoqlDB
	l  *rateLimiter
}

func (p *pacedClient) CallTool(ctx context.Context, name string, args any) (json.RawMessage, error) {
	if err := p.l.Wait(ctx); err != nil {
		return nil, fmt.Errorf("aikoql rate limit: %w", err)
	}
	return p.db.CallTool(ctx, name, args)
}

// NewPacedClient wraps a tool-call surface so calls respect the server's
// calls-per-minute policy. The burst cap equals the limit: short bursts
// pass unthrottled while the sustained rate never trips it.
func NewPacedClient(db aikoqlDB, callsPerMin int) aikoqlDB {
	now := time.Now
	return &pacedClient{db: db, l: &rateLimiter{
		now: now, sleep: realSleep,
		cap: float64(callsPerMin), rate: float64(callsPerMin) / 60,
		avail: float64(callsPerMin), last: now(),
	}}
}
