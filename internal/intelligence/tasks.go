// tasks.go is the "Where are agent tasks failing?" report (Milestone F,
// item 46): a single-window location report, not a two-window comparison, so
// it has its own engine beside MetricChange. Failed finished tasks group by
// their containing session — the finest location the telemetry records,
// since tasks carry no repo identity.
package intelligence

import (
	"fmt"
	"sort"
	"strings"

	"github.com/anckursingh/engineering-intelligence/internal/metrics"
)

// QuestionTaskFailures is the fifth Milestone F exit question — the only one
// that asks "where", not "what changed", hence its own engine.
const QuestionTaskFailures = "Where are agent tasks failing?"

// TaskFailureRow is one session's failed finished tasks in the window.
type TaskFailureRow struct {
	Session string   `json:"session"`
	Failed  int      `json:"failed"`
	Tasks   []string `json:"tasks"`
}

// TaskFailures groups the window's failed finished tasks by session, rows
// sorted by session external ID. Unfinished tasks (no CompletedAt) and tasks
// outside the window are silence, never failures — a task that has not ended
// or failed in another period is not this period's failure.
func TaskFailures(pop metrics.Population, win metrics.Window) []TaskFailureRow {
	m := map[string][]string{}
	for _, t := range pop.AgentTasks {
		if t.Value.Status != "failed" || t.Value.CompletedAt.IsZero() ||
			t.Value.CompletedAt.Before(win.Start) || !t.Value.CompletedAt.Before(win.End) {
			continue
		}
		s := pop.TaskSessions[t.ExternalID]
		m[s] = append(m[s], t.ExternalID)
	}
	// ponytail: map iteration + two small sorts — deterministic for any
	// realistic session count; a single pre-sorted pass if that grows.
	sessions := make([]string, 0, len(m))
	for s := range m {
		sessions = append(sessions, s)
	}
	sort.Strings(sessions)
	rows := make([]TaskFailureRow, 0, len(sessions))
	for _, s := range sessions {
		ids := m[s]
		sort.Strings(ids)
		rows = append(rows, TaskFailureRow{Session: s, Failed: len(ids), Tasks: ids})
	}
	return rows
}

// taskFailuresStatement renders the report: honest absence, or the
// per-session grouping with cited task ids.
func taskFailuresStatement(rows []TaskFailureRow) string {
	if len(rows) == 0 {
		return "No agent tasks failed in the period."
	}
	total := 0
	for _, r := range rows {
		total += r.Failed
	}
	var bld strings.Builder
	fmt.Fprintf(&bld, "%d agent %s failed in the period across %d %s: ",
		total, plural(total, "task", "tasks"), len(rows), plural(len(rows), "session", "sessions"))
	for i, r := range rows {
		if i > 0 {
			bld.WriteString("; ")
		}
		fmt.Fprintf(&bld, "%s: %d (%s)", r.Session, r.Failed, strings.Join(r.Tasks, ", "))
	}
	bld.WriteString(".")
	return bld.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// taskLimitations state what the report can and cannot see.
var taskLimitations = []string{
	"task location is the coding session — the finest identity the telemetry records; tasks carry no repository identity",
	"only sessions reachable through a code contribution's producing task appear in the report",
}
