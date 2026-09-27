package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	gh "github.com/google/go-github/v92/github"
)

// Client wraps the go-github REST client with retry and pagination.
type Client struct {
	gh    *gh.Client
	sleep func(time.Duration)
}

// NewClient builds a client for the given token (empty = unauthenticated,
// 60 req/hr on public repos). httpClient is the test seam: tests point it
// at an httptest server via a host-rewriting transport (go-github v92 has
// no exported BaseURL).
func NewClient(token string, httpClient *http.Client) (*Client, error) {
	var opts []gh.ClientOptionsFunc
	if httpClient != nil {
		opts = append(opts, gh.WithHTTPClient(httpClient))
	}
	if token != "" {
		opts = append(opts, gh.WithAuthToken(token))
	}
	c, err := gh.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("github: new client: %w", err)
	}
	return &Client{gh: c, sleep: time.Sleep}, nil
}

const maxRetries = 3

// retry runs fn, retrying rate-limit errors (until the reset window) and
// 5xx (1s/2s/4s backoff). 4xx errors return immediately. Honors ctx.
func retry[T any](ctx context.Context, c *Client, name string, fn func() (T, *gh.Response, error)) (T, *gh.Response, error) {
	var zero T
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, nil, fmt.Errorf("github: %s aborted: %w", name, err)
		}
		v, resp, err := fn()
		if err == nil {
			return v, resp, nil
		}
		if resp == nil {
			return zero, nil, fmt.Errorf("github: %s: %w", name, err)
		}
		var rl *gh.RateLimitError
		var ab *gh.AbuseRateLimitError
		switch {
		case errors.As(err, &rl):
			if err := c.sleepCtx(ctx, time.Until(rl.Rate.Reset.Time)+time.Second); err != nil {
				return zero, nil, fmt.Errorf("github: %s aborted: %w", name, err)
			}
			continue
		case errors.As(err, &ab):
			if err := c.sleepCtx(ctx, ab.GetRetryAfter()); err != nil {
				return zero, nil, fmt.Errorf("github: %s aborted: %w", name, err)
			}
			continue
		case resp.StatusCode >= 500:
			if attempt >= maxRetries {
				return zero, nil, fmt.Errorf("github: %s: gave up after %d retries: %w", name, maxRetries, err)
			}
			if err := c.sleepCtx(ctx, time.Duration(1<<attempt)*time.Second); err != nil {
				return zero, nil, fmt.Errorf("github: %s aborted: %w", name, err)
			}
			continue
		default:
			return zero, nil, fmt.Errorf("github: %s: %w", name, err)
		}
	}
}

func (c *Client) sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		d = time.Second
	}
	if c.sleep == nil {
		c.sleep = time.Sleep
	}
	// ponytail: c.sleep (test seam) is the delay itself; a canceled ctx is
	// noticed only after the sleep — the CLI exits a moment late, not wrong.
	c.sleep(d)
	return ctx.Err()
}

// paginate pages through any list call with page size 100.
func paginate[T any](ctx context.Context, c *Client, name string, fetch func(page int) ([]T, *gh.Response, error)) ([]T, error) {
	var out []T
	for page := 1; ; {
		items, resp, err := retry(ctx, c, name, func() ([]T, *gh.Response, error) {
			return fetch(page)
		})
		if err != nil {
			return nil, fmt.Errorf("github: %s page %d: %w", name, page, err)
		}
		out = append(out, items...)
		if resp.NextPage == 0 {
			return out, nil
		}
		page = resp.NextPage
	}
}
