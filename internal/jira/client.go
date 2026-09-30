package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a minimal Jira Cloud REST v3 client (§14): just what the
// connector needs — issue search with basic auth. stdlib http keeps the
// httptest seam trivial (the fake world serves a real server URL).
type Client struct {
	base          *url.URL
	http          *http.Client
	email         string
	token         string
	sprintFieldID string
	sprintLoaded  bool
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
	Issues        []wireIssue `json:"issues"`
	NextPageToken string      `json:"nextPageToken"`
	IsLast        bool        `json:"isLast"`
}

type searchRequest struct {
	JQL           string   `json:"jql"`
	MaxResults    int      `json:"maxResults"`
	Fields        []string `json:"fields"`
	NextPageToken string   `json:"nextPageToken,omitempty"`
}

type wireIssue struct {
	Key    string          `json:"key"`
	Fields wireIssueFields `json:"fields"`
}

type wireIssueFields struct {
	Summary string `json:"summary"`
	Status  struct {
		Name string `json:"name"`
	} `json:"status"`
	IssueType struct {
		Name string `json:"name"`
	} `json:"issuetype"`
	Project struct {
		ID   string `json:"id"`
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"project"`
	Created  string   `json:"created"`
	Updated  string   `json:"updated"`
	Reporter wireUser `json:"reporter"`
	Assignee wireUser `json:"assignee"`
	Parent   *struct {
		Key string `json:"key"`
	} `json:"parent"`
	Custom map[string]json.RawMessage `json:"-"`
}

type wireSprint struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	BoardID   int64  `json:"boardId"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

func (f *wireIssueFields) UnmarshalJSON(data []byte) error {
	type plain wireIssueFields
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("jira: decode issue fields: %w", err)
	}
	var custom map[string]json.RawMessage
	if err := json.Unmarshal(data, &custom); err != nil {
		return fmt.Errorf("jira: decode custom issue fields: %w", err)
	}
	*f = wireIssueFields(decoded)
	f.Custom = custom
	return nil
}

func (c *Client) sprintField(ctx context.Context) (string, error) {
	if c.sprintLoaded {
		return c.sprintFieldID, nil
	}
	u := c.base.JoinPath("rest", "api", "3", "field")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("jira: build field request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.email, c.token)
	res, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("jira: list fields: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("jira: list fields: status %s", res.Status)
	}
	var fields []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(res.Body).Decode(&fields); err != nil {
		return "", fmt.Errorf("jira: decode field list: %w", err)
	}
	for _, field := range fields {
		if strings.EqualFold(field.Name, "Sprint") {
			c.sprintFieldID = field.ID
			break
		}
	}
	c.sprintLoaded = true
	return c.sprintFieldID, nil
}

// wireUser is the identity-bearing slice of a Jira user (§15).
type wireUser struct {
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	AccountID    string `json:"accountId"`
}

// search fetches one page from Jira Cloud's enhanced JQL endpoint. ponytail:
// no retry — Jira 429s are rare at this scale; add backoff when a real org hits them.
func (c *Client) search(ctx context.Context, jql, nextPageToken string) (*searchPage, error) {
	u := c.base.JoinPath("rest", "api", "3", "search", "jql")
	sprintFieldID, err := c.sprintField(ctx)
	if err != nil {
		return nil, err
	}
	fields := []string{"key", "summary", "status", "issuetype", "created", "updated", "reporter", "assignee", "parent", "project"}
	if sprintFieldID != "" {
		fields = append(fields, sprintFieldID)
	}
	payload, err := json.Marshal(searchRequest{
		JQL:           jql,
		MaxResults:    100,
		Fields:        fields,
		NextPageToken: nextPageToken,
	})
	if err != nil {
		return nil, fmt.Errorf("jira: encode search request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("jira: build search request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
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
