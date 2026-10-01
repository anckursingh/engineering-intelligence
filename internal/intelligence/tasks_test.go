// Package intelligence: "Where are agent tasks failing?" (Milestone F, item
// 46) — a single-window report, not a two-window comparison, so it has its
// own engine beside MetricChange. Failed finished tasks group by their
// containing session: the finest location the telemetry records, since
// tasks carry no repo identity. Unfinished tasks and tasks outside the
// window are silence, never failures.
package intelligence

import (
	"strings"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/metrics"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// TestTaskFailuresGroupsBySession: only failed tasks completed in the
// window count, grouped by session in deterministic (sorted) order.
func TestTaskFailuresGroupsBySession(t *testing.T) {
	sep := func(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC) }
	win := metrics.Window{Start: sep(1), End: sep(30)}
	pop := metrics.Population{
		AgentTasks: []metrics.Entity[ontology.AgentTask]{
			{ExternalID: "ei.com:agent-task:claude-code:t1", Value: ontology.AgentTask{Status: "failed", CompletedAt: sep(3)}},
			{ExternalID: "ei.com:agent-task:claude-code:t2", Value: ontology.AgentTask{Status: "completed", CompletedAt: sep(5)}},
			{ExternalID: "ei.com:agent-task:claude-code:t3", Value: ontology.AgentTask{Status: "failed", CompletedAt: sep(7)}},
			{ExternalID: "ei.com:agent-task:claude-code:t4", Value: ontology.AgentTask{Status: "failed", CompletedAt: sep(30).Add(-40 * 24 * time.Hour)}}, // before the window
			{ExternalID: "ei.com:agent-task:claude-code:t5", Value: ontology.AgentTask{Status: "failed"}},                                                 // never finished
		},
		TaskSessions: map[string]string{
			"ei.com:agent-task:claude-code:t1": "ei.com:coding-session:claude-code:s1",
			"ei.com:agent-task:claude-code:t2": "ei.com:coding-session:claude-code:s1",
			"ei.com:agent-task:claude-code:t3": "ei.com:coding-session:claude-code:s2",
			"ei.com:agent-task:claude-code:t4": "ei.com:coding-session:claude-code:s2",
			"ei.com:agent-task:claude-code:t5": "ei.com:coding-session:claude-code:s1",
		},
	}
	rows := TaskFailures(pop, win)

	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (t2 completed, t4 out of window, t5 unfinished are silence)", len(rows))
	}
	if rows[0].Session != "ei.com:coding-session:claude-code:s1" || rows[0].Failed != 1 ||
		len(rows[0].Tasks) != 1 || rows[0].Tasks[0] != "ei.com:agent-task:claude-code:t1" {
		t.Errorf("rows[0] = %+v, want s1 with [t1]", rows[0])
	}
	if rows[1].Session != "ei.com:coding-session:claude-code:s2" || rows[1].Failed != 1 ||
		len(rows[1].Tasks) != 1 || rows[1].Tasks[0] != "ei.com:agent-task:claude-code:t3" {
		t.Errorf("rows[1] = %+v, want s2 with [t3]", rows[1])
	}
}

// TestTaskFailuresStatement: the report renders honest absence, the
// per-session grouping with cited task ids, and singular/plural agreement.
func TestTaskFailuresStatement(t *testing.T) {
	if got := taskFailuresStatement(nil); got != "No agent tasks failed in the period." {
		t.Errorf("empty statement = %q", got)
	}
	rows := []TaskFailureRow{
		{Session: "ei.com:coding-session:claude-code:s1", Failed: 1, Tasks: []string{"ei.com:agent-task:claude-code:t1"}},
	}
	want := "1 agent task failed in the period across 1 session: " +
		"ei.com:coding-session:claude-code:s1: 1 (ei.com:agent-task:claude-code:t1)."
	if got := taskFailuresStatement(rows); got != want {
		t.Errorf("statement = %q\nwant       %q", got, want)
	}
	rows = append(rows, TaskFailureRow{Session: "ei.com:coding-session:claude-code:s2", Failed: 2, Tasks: []string{"t2", "t3"}})
	got := taskFailuresStatement(rows)
	if !strings.Contains(got, "3 agent tasks failed in the period across 2 sessions: ") ||
		!strings.Contains(got, "; ei.com:coding-session:claude-code:s2: 2 (t2, t3).") {
		t.Errorf("plural statement = %q", got)
	}
}
