package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client is a minimal Jira Cloud REST v3 client (§14): just what the
// connector needs — issue search with basic auth. stdlib http keeps the
// httptest seam trivial (the fake world serves a real server URL).
type Client struct {
	base  *url.URL
	http  *http.Client
	email string
	token string
}

// NewClient validates the base URL and builds the client. httpClient is the
// test seam; nil = http.DefaultClient.
func NewClient(baseURL, email, token string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return nil, fmt.Errorf("jira: invalid base url %q", baseURL)
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{base: u, http: httpClient, email: email, token: token}, nil
}

// Site returns the identity scope: the site host. The port is deliberately
// excluded — test servers bind a new port every run, and external IDs must
// not vary with it.
func (c *Client) Site() string { return c.base.Hostname() }

// wireIssue/searchPage mirror the REST v3 search response shape (§14: the
// Jira schema stays wire-level; ontology owns the canonical mapping).
type searchPage struct {
	Total      int         `json:"total"`
	MaxResults int         `json:"maxResults"`
	StartAt    int         `json:"startAt"`
	Issues     []wireIssue `json:"issues"`
}

type wireIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Status  struct {
			Name string `json:"name"`
		} `json:"status"`
		IssueType struct {
			Name string `json:"name"`
		} `json:"issuetype"`
		Created string `json:"created"`
		Updated string `json:"updated"`
	} `json:"fields"`
}

// search fetches one page of the issue search. ponytail: no retry — Jira
// 429s are rare at this scale; add backoff when a real org hits them.
func (c *Client) search(ctx context.Context, jql string, startAt int) (*searchPage, error) {
	u := c.base.JoinPath("rest", "api", "3", "search")
	q := url.Values{}
	q.Set("jql", jql)
	q.Set("maxResults", "100")
	q.Set("startAt", strconv.Itoa(startAt))
	q.Set("fields", "key,summary,status,issuetype,created,updated")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("jira: build search request: %w", err)
	}
	req.SetBasicAuth(c.email, c.token)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira: search: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jira: search: status %s", res.Status)
	}
	var p searchPage
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("jira: decode search page: %w", err)
	}
	return &p, nil
}

// parseTime accepts the Jira classic format and RFC3339 (the fake world
// serves the latter).
func parseTime(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02T15:04:05.000-0700", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("jira: unparseable time %q", s)
}
