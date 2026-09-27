// Package jiratest exports the fake Jira world shared by the connector's
// unit tests (internal/jira) and the black-box acceptance suite
// (test/acceptance) — one fixture, not two copies.
package jiratest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/jira"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

// World is the mutable fake Jira world: one project, two issues (one epic),
// one delta issue. It serves its own httptest server and honors the
// updated>="..." JQL boundary like the real search endpoint does.
type World struct {
	mu        sync.Mutex
	issues    []wireIssue
	jqls      []string // every jql seen, for assertions
	serverURL string
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
		Created  string   `json:"created"`
		Updated  string   `json:"updated"`
		Reporter wireUser `json:"reporter"`
		Assignee wireUser `json:"assignee"`
	} `json:"fields"`
}

// wireUser is the identity-bearing slice of a Jira user.
type wireUser struct {
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

func wire(key, summary, status, issueType string, updated time.Time) *wireIssue {
	w := &wireIssue{Key: key}
	w.Fields.Summary = summary
	w.Fields.Status.Name = status
	w.Fields.IssueType.Name = issueType
	w.Fields.Created = updated.Add(-24 * time.Hour).Format(time.RFC3339)
	w.Fields.Updated = updated.Format(time.RFC3339)
	return w
}

func (w *wireIssue) withReporter(name, email string) *wireIssue {
	w.Fields.Reporter = wireUser{DisplayName: name, EmailAddress: email}
	return w
}

// NewWorld builds the fixture world and starts its HTTP server (closed by
// t.Cleanup).
func NewWorld(t *testing.T) *World {
	t.Helper()
	t0 := time.Now().UTC().Add(-10 * 24 * time.Hour)
	w := &World{issues: []wireIssue{
		*wire("PLAY-1", "Wobbling widget", "Open", "Bug", t0.Add(24*time.Hour)).
			withReporter("Bob", "bob@corp.example"),
		*wire("PLAY-2", "Widget platform", "In Progress", "Epic", t0.Add(48*time.Hour)).
			withReporter("John Doe", "john.doe@company.com"),
	}}
	server := httptest.NewServer(w)
	t.Cleanup(server.Close)
	w.serverURL = server.URL
	return w
}

// AddDeltaActivity mutates the world between runs: PLAY-3 created. Its
// updated timestamp is the mutation moment itself, so it always lands after
// the previous run's watermark (a delta older than the watermark is by
// definition not a delta).
func (w *World) AddDeltaActivity() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.issues = append(w.issues, *wire("PLAY-3", "New wobble", "Open", "Bug", time.Now().UTC()).
		withReporter("Bob", "bob@corp.example"))
}

// JQLs returns every search JQL the world has seen.
func (w *World) JQLs() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.jqls...)
}

// SyncConfig returns the jira.Config that talks to this world's server.
func (w *World) SyncConfig(dir string, store knowledge.KnowledgeStore) jira.Config {
	u, err := url.Parse(w.serverURL)
	if err != nil {
		panic(err) // test helper: httptest URLs always parse
	}
	return jira.Config{
		BaseURL:        u.Scheme + "://" + u.Host,
		Email:          "ci@corp.example",
		Token:          "fake-token",
		Project:        "PLAY",
		CheckpointPath: filepath.Join(dir, "checkpoint.json"),
		Store:          store,
	}
}

var updatedRe = regexp.MustCompile(`updated\s*>=\s*"([^"]+)"`)

// parseWatermark accepts both formats the connector emits: the Jira classic
// minute format and RFC3339.
func parseWatermark(s string) (time.Time, error) {
	for _, layout := range []string{"2006/01/02 15:04", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("jiratest: unparseable watermark %q", s)
}

func (w *World) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if r.URL.Path != "/rest/api/3/search" {
		http.NotFound(rw, r)
		return
	}
	jql := r.URL.Query().Get("jql")
	w.jqls = append(w.jqls, jql)
	issues := w.issues
	if m := updatedRe.FindStringSubmatch(jql); m != nil {
		if since, err := parseWatermark(m[1]); err == nil {
			filtered := issues[:0]
			for _, i := range issues {
				upd, err := time.Parse(time.RFC3339, i.Fields.Updated)
				if err == nil && !upd.Before(since) {
					filtered = append(filtered, i)
				}
			}
			issues = filtered
		}
	}
	startAt := 0
	rw.Header().Set("Content-Type", "application/json")
	json.NewEncoder(rw).Encode(map[string]any{
		"total":      len(issues),
		"maxResults": 100,
		"startAt":    startAt,
		"issues":     issues,
	})
}
