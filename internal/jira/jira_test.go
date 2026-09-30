// External test package: black-box over the connector's exported API, sharing
// the fake world with test/acceptance (jiratest imports jira, so the unit
// tests stay external too — same shape as the github package).
package jira_test

import (
	"context"
	"strings"
	"testing"

	"github.com/anckursingh/engineering-intelligence/internal/checkpoint"
	"github.com/anckursingh/engineering-intelligence/internal/jira"
	"github.com/anckursingh/engineering-intelligence/internal/jira/jiratest"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

func TestJiraSyncRun1Full(t *testing.T) {
	w := jiratest.NewWorld(t)
	store := knowledge.NewMemory()
	cfg := w.SyncConfig(t.TempDir(), store)
	res, err := jira.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]jira.Count{
		"Issue": {New: 1}, // PLAY-1
		"Epic":  {New: 1}, // PLAY-2: issuetype Epic maps to the canonical Epic
	}
	for typ, wantC := range want {
		if got := res.Counts[typ]; got != wantC {
			t.Errorf("count %s = %+v, want %+v", typ, got, wantC)
		}
	}
	if len(res.Counts) != 2 {
		t.Errorf("counts = %+v, want exactly Issue and Epic", res.Counts)
	}

	// AC-ING-002 + AC-KG-004 mechanics: canonical Issue carries provenance.
	ko, err := store.GetByExternalID(context.Background(), "jira.com:127.0.0.1:issue:PLAY-1")
	if err != nil {
		t.Fatal(err)
	}
	if ko.TypeName != "Issue" || ko.Properties["key"] != "PLAY-1" {
		t.Errorf("canonical issue = %s %v", ko.TypeName, ko.Properties)
	}
	if ko.Provenance.Source != "jira" || ko.Provenance.IngestionRun != res.RunID {
		t.Errorf("provenance = %+v, want source jira, run %s", ko.Provenance, res.RunID)
	}

	ck, err := checkpoint.Load(cfg.CheckpointPath)
	if err != nil {
		t.Fatal(err)
	}
	if ck.Jira.UpdatedSince.IsZero() {
		t.Error("jira watermark not advanced after a successful run")
	}
	if len(ck.Runs) != 1 {
		t.Errorf("run records = %d, want 1", len(ck.Runs))
	}
	tokens := w.PageTokens()
	if len(tokens) != 2 || tokens[0] != "" || tokens[1] != "next" {
		t.Errorf("search page tokens = %q, want empty token then next", tokens)
	}
}

func TestJiraSyncRun2Delta(t *testing.T) {
	w := jiratest.NewWorld(t)
	store := knowledge.NewMemory()
	cfg := w.SyncConfig(t.TempDir(), store)
	initial, err := jira.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if initial == nil || initial.RunID == "" {
		t.Fatal("initial sync returned no run identity")
	}
	w.AddDeltaActivity()

	res2, err := jira.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if c := res2.Counts["Issue"]; c != (jira.Count{New: 1}) {
		t.Errorf("run2 Issue = %+v, want new 1 (PLAY-3 only)", c)
	}
	if c := res2.Counts["Epic"]; c != (jira.Count{}) {
		t.Errorf("run2 Epic = %+v, want zero (unchanged)", c)
	}
	// The JQL sent to the server must carry the previous run's watermark —
	// incrementality is the server's filter, not a client-side guess.
	jqls := w.JQLs()
	if len(jqls) != 3 || !strings.Contains(jqls[2], "updated >=") {
		t.Errorf("jqls = %q, want run2's query to filter by updated >=", jqls)
	}
	tokens := w.PageTokens()
	if len(tokens) != 3 || tokens[0] != "" || tokens[1] != "next" || tokens[2] != "" {
		t.Errorf("search page tokens = %q, want a continuation only for the initial two-page sync", tokens)
	}

	// AC-ING-005: re-running the same page creates no duplicates.
	res3, err := jira.Sync(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	for typ, c := range res3.Counts {
		if c.New != 0 || c.Updated != 0 {
			t.Errorf("%s = %+v on unchanged rerun, want zero new/updated", typ, c)
		}
	}
	delta, err := store.GetByExternalID(context.Background(), "jira.com:127.0.0.1:issue:PLAY-3")
	if err != nil {
		t.Errorf("delta issue PLAY-3 not in store: %v", err)
	} else if delta.TypeName != "Issue" {
		t.Errorf("delta issue type = %q, want Issue", delta.TypeName)
	}
}
