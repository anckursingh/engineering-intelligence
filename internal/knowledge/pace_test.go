package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	aikoql "github.com/ancku/aikoql-sdk"
)

// rateLimitErr mimics the SDK's wrap of the server's dispatcher rejection:
// JSON-RPC -32000, "rate limit exceeded (max N calls/min)" (dispatcher.rs).
func rateLimitErr() error {
	return fmt.Errorf("aikoql: get: %w", &aikoql.McpError{Code: "-32000", Message: "rate limit exceeded (max 120 calls/min)"})
}

// scriptDb returns one scripted (raw, err) per call, in order.
type scriptRes struct {
	raw json.RawMessage
	err error
}

type scriptDb struct {
	calls int
	res   []scriptRes
}

type alwaysRateLimitedDB struct {
	calls int
}

func (s *alwaysRateLimitedDB) CallTool(context.Context, string, any) (json.RawMessage, error) {
	s.calls++
	return nil, rateLimitErr()
}

func (s *scriptDb) CallTool(_ context.Context, _ string, _ any) (json.RawMessage, error) {
	r := s.res[s.calls]
	s.calls++
	return r.raw, r.err
}

// sleepRec records requested sleeps; err, when set, is returned instead.
type sleepRec struct {
	durs     []time.Duration
	err      error
	errAfter int
}

func (s *sleepRec) sleep(_ context.Context, d time.Duration) error {
	s.durs = append(s.durs, d)
	if s.err != nil && (s.errAfter == 0 || len(s.durs) >= s.errAfter) {
		return s.err
	}
	return nil
}

func retrying(db aikoqlDB, sleep func(context.Context, time.Duration) error) *rateLimitClient {
	return &rateLimitClient{db: db, sleep: sleep}
}

func TestRateLimitRetriesUntilAccepted(t *testing.T) {
	db := &scriptDb{res: []scriptRes{
		{err: rateLimitErr()},
		{raw: json.RawMessage(`{"koid":"k"}`)},
	}}
	sl := &sleepRec{}
	out, err := retrying(db, sl.sleep).CallTool(context.Background(), "get", map[string]any{"koid": "k"})
	if err != nil || string(out) != `{"koid":"k"}` {
		t.Fatalf("CallTool = %s, %v; want payload, nil", out, err)
	}
	if db.calls != 2 {
		t.Errorf("inner calls = %d, want 2", db.calls)
	}
	if len(sl.durs) != 1 || sl.durs[0] != time.Second {
		t.Errorf("sleeps = %v, want one of 1s", sl.durs)
	}
}

func TestRateLimitBackoffGrows(t *testing.T) {
	db := &scriptDb{res: []scriptRes{
		{err: rateLimitErr()},
		{err: rateLimitErr()},
		{err: rateLimitErr()},
		{raw: json.RawMessage(`{"ok":true}`)},
	}}
	sl := &sleepRec{}
	if _, err := retrying(db, sl.sleep).CallTool(context.Background(), "get", nil); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	if len(sl.durs) != len(want) {
		t.Fatalf("sleeps = %v, want %v", sl.durs, want)
	}
	for i := range want {
		if sl.durs[i] != want[i] {
			t.Errorf("sleep %d = %v, want %v", i, sl.durs[i], want[i])
		}
	}
}

func TestRateLimitPassesOtherErrorsThrough(t *testing.T) {
	boom := errors.New("aikoql: get: boom")
	db := &scriptDb{res: []scriptRes{{err: boom}}}
	sl := &sleepRec{}
	if _, err := retrying(db, sl.sleep).CallTool(context.Background(), "get", nil); err != boom {
		t.Errorf("err = %v, want the original, unretried", err)
	}
	if db.calls != 1 {
		t.Errorf("inner calls = %d, want 1 (no retry)", db.calls)
	}
	if len(sl.durs) != 0 {
		t.Errorf("slept %v on a non-rate-limit error", sl.durs)
	}
}

func TestRateLimitPassesSuccessThrough(t *testing.T) {
	db := &scriptDb{res: []scriptRes{{raw: json.RawMessage(`{"ok":true}`)}}}
	sl := &sleepRec{}
	out, err := retrying(db, sl.sleep).CallTool(context.Background(), "get", nil)
	if err != nil || string(out) != `{"ok":true}` {
		t.Fatalf("CallTool = %s, %v", out, err)
	}
	if db.calls != 1 || len(sl.durs) != 0 {
		t.Errorf("calls = %d, sleeps = %v; want 1 and none", db.calls, sl.durs)
	}
}

func TestRateLimitSleepHonorsContext(t *testing.T) {
	db := &scriptDb{res: []scriptRes{{err: rateLimitErr()}}}
	sl := &sleepRec{err: context.Canceled}
	_, err := retrying(db, sl.sleep).CallTool(context.Background(), "get", nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if len(sl.durs) != 1 {
		t.Errorf("sleeps = %v, want one attempt", sl.durs)
	}
}

func TestRateLimitRetryExhaustion(t *testing.T) {
	db := &alwaysRateLimitedDB{}
	const wantRetries = 5
	sl := &sleepRec{err: context.Canceled, errAfter: wantRetries + 1}
	_, err := retrying(db, sl.sleep).CallTool(context.Background(), "get", nil)
	if !isRateLimit(err) {
		t.Fatalf("CallTool error = %v, want the final rate-limit error", err)
	}
	if want := wantRetries + 1; db.calls != want {
		t.Errorf("inner calls = %d, want %d (initial call plus retry budget)", db.calls, want)
	}
	if want := wantRetries; len(sl.durs) != want {
		t.Errorf("sleeps = %d, want %d (no sleep after exhaustion)", len(sl.durs), want)
	}
}
