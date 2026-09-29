package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// fakeClock is a hand-advanced time source; the limiter's sleep is injected
// too, so spacing tests are deterministic (no real time, no flakes).
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }
func (c *fakeClock) sleep(_ context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	return nil
}

func paced(now func() time.Time, sleep func(context.Context, time.Duration) error, cap float64, rate float64) *rateLimiter {
	return &rateLimiter{now: now, sleep: sleep, cap: cap, rate: rate, avail: cap, last: now()}
}

func TestRateLimiterSpacing(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	l := paced(clk.Now, clk.sleep, 2, 4) // burst 2, refill 4/s
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := l.Wait(ctx); err != nil {
			t.Fatalf("burst Wait %d: %v", i, err)
		}
	}
	if got := clk.now.UnixNano(); got != 0 {
		t.Fatalf("burst Waits advanced the clock to %d, want no sleep", got)
	}
	// Third and fourth calls consume refilled tokens: 250ms each at 4/s.
	if err := l.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if got := clk.now; !got.Equal(time.Unix(0, 250*int64(time.Millisecond))) {
		t.Errorf("third Wait clock = %v, want 250ms", got)
	}
	if err := l.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if got := clk.now; !got.Equal(time.Unix(0, 500*int64(time.Millisecond))) {
		t.Errorf("fourth Wait clock = %v, want 500ms", got)
	}
}

func TestRateLimiterContextCancel(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	// Exhaust the burst, then cancel the waiting call: Wait must return the
	// context error, not sleep forever.
	l := paced(clk.Now, clk.sleep, 1, 1)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait = %v, want context.Canceled", err)
	}
}

// callFunc adapts a plain function to aikoqlDB.
type callFunc func(ctx context.Context, name string, args any) (json.RawMessage, error)

func (f callFunc) CallTool(ctx context.Context, name string, args any) (json.RawMessage, error) {
	return f(ctx, name, args)
}

func TestPacedClientForwardsAndSpaces(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	calls := 0
	inner := callFunc(func(_ context.Context, name string, args any) (json.RawMessage, error) {
		calls++
		if name != "get" {
			t.Errorf("tool = %q, want get", name)
		}
		return json.RawMessage(`{"koid":"k"}`), nil
	})
	p := &pacedClient{db: inner, l: paced(clk.Now, clk.sleep, 2, 4)}
	for i := 0; i < 3; i++ {
		out, err := p.CallTool(context.Background(), "get", map[string]any{"koid": "k"})
		if err != nil || string(out) != `{"koid":"k"}` {
			t.Fatalf("CallTool %d = %s, %v", i, out, err)
		}
	}
	if calls != 3 {
		t.Errorf("inner calls = %d, want 3", calls)
	}
	if got := clk.now; !got.Equal(time.Unix(0, 250*int64(time.Millisecond))) {
		t.Errorf("third call not spaced: clock = %v, want 250ms", got)
	}
}
