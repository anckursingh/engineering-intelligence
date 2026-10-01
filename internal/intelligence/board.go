// board.go is the first product board (§28): one response with the doc's
// five sections answering "how is engineering operating", "how is AI
// changing the workflow", and "how strong is the evidence". Empty sections
// are honest notes, never fabricated numbers, and the board carries no
// per-person data — individual ranking is impossible by construction.
// "What changed / why might it have changed" stays the investigations
// endpoint's job (§22-24); the board shows the current window.
package intelligence

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// boardItem is one computed value with its epistemic state and evidence.
type boardItem struct {
	Metric         string              `json:"metric"`
	Label          string              `json:"label"`
	Value          float64             `json:"value"`
	Unit           string              `json:"unit"`
	EpistemicState string              `json:"epistemic_state"`
	Evidence       []evidence.Evidence `json:"evidence"`
}

// boardSection is one §28 section; Note explains an honest absence.
type boardSection struct {
	ID    string      `json:"id"`
	Title string      `json:"title"`
	Items []boardItem `json:"items"`
	Note  string      `json:"note,omitempty"`
}

// board is the §28 response: the five sections in doc order.
type board struct {
	Sections []boardSection `json:"sections"`
}

// aiBoardMetrics is the AI Development section's metric set (§27, Milestone
// E): the workflow dimension — volume, task outcomes, cost — alongside the
// assisted-PR share. Each is computed when the population carries the
// telemetry; absence stays an honest gap, never a zero.
var aiBoardMetrics = []struct {
	name, label, unit string
	compute           func(metrics.Population, metrics.Window) []metrics.Observation
}{
	{"ai_assisted_pr_pct", "AI-assisted PR percentage", "%", metrics.AIAssistedPRPct},
	{"ai_interaction_volume", "AI interaction volume", "interactions", metrics.AIInteractionVolume},
	{"ai_run_volume", "AI run volume", "runs", metrics.AIRunVolume},
	{"ai_task_completion", "AI task completion", "%", metrics.AgentTaskCompletion},
	{"human_intervention_rate", "Human intervention rate", "%", metrics.HumanInterventionRate},
	{"retry_rate", "Retry rate", "%", metrics.RetryRate},
	{"ai_cost", "AI cost", "USD", metrics.AICost},
	{"cost_per_completed_task", "Cost per completed task", "USD", metrics.CostPerCompletedTask},
}

// buildBoard computes the board for one window over one Population. The
// candidates table is the metric registry: 0-2 are the flow set, 4 the
// quality set; the AI Development section runs its own §27 metric set.
func buildBoard(pop metrics.Population, win metrics.Window) board {
	var flow, ai, quality []boardItem
	var aiRunsPresent, aiCostAvailable bool
	for _, c := range []candidate{candidates[0], candidates[1], candidates[2], candidates[5], candidates[6]} {
		if v, ok, ev := summarize(c.compute(pop, win)); ok {
			flow = append(flow, boardItem{Metric: c.name, Label: c.label, Value: v, Unit: c.unit,
				EpistemicState: evidence.StateCalculated.String(), Evidence: ev})
		}
	}
	for _, m := range aiBoardMetrics {
		if v, ok, ev := summarize(m.compute(pop, win)); ok {
			ai = append(ai, boardItem{Metric: m.name, Label: m.label, Value: v, Unit: m.unit,
				EpistemicState: evidence.StateCalculated.String(), Evidence: ev})
			if m.name == "ai_run_volume" {
				aiRunsPresent = true
			}
			if m.name == "ai_cost" {
				aiCostAvailable = true
			}
		}
	}
	if v, ok, ev := summarize(candidates[4].compute(pop, win)); ok {
		quality = append(quality, boardItem{Metric: candidates[4].name, Label: candidates[4].label, Value: v, Unit: candidates[4].unit,
			EpistemicState: evidence.StateCalculated.String(), Evidence: ev})
	}
	sections := []boardSection{
		{ID: "engineering_flow", Title: "Engineering Flow", Items: flow},
		{ID: "quality", Title: "Quality", Items: quality},
		{ID: "reliability", Title: "Reliability", Note: "no deployment or incident data — no sources ingested yet"},
		{ID: "ai_development", Title: "AI Development", Items: ai},
	}
	if len(flow) == 0 {
		sections[0].Note = "no flow data in this window"
	}
	if len(quality) == 0 {
		sections[1].Note = "no CI data — no workflow runs ingested yet"
	}
	sections[2] = reliabilitySection(pop, win)
	if len(ai) == 0 {
		sections[3].Note = "no AI telemetry linked to this scope"
	} else if aiRunsPresent && !aiCostAvailable {
		sections[3].Note = "AI cost is not reported for all runs in this window — cost metrics omitted"
	}
	work := jiraWorkSection(pop)
	coverageItems := append(append(append([]boardItem{}, flow...), ai...), quality...)
	coverageItems = append(coverageItems, sections[2].Items...)
	coverageItems = append(coverageItems, work.Items...)
	return board{Sections: append(sections, evidenceCoverage(coverageItems), work)}
}

func reliabilitySection(pop metrics.Population, win metrics.Window) boardSection {
	section := boardSection{ID: "reliability", Title: "Reliability"}
	var deploymentIDs, releaseIDs []string
	for _, deployment := range pop.Deployments {
		if inWindow(deployment.Value.CreatedAt, win) {
			deploymentIDs = append(deploymentIDs, deployment.ExternalID)
		}
	}
	for _, release := range pop.Releases {
		if inWindow(release.Value.PublishedAt, win) {
			releaseIDs = append(releaseIDs, release.ExternalID)
		}
	}
	if len(deploymentIDs) == 0 && len(releaseIDs) == 0 {
		section.Note = "no deployment or release data in this window"
		return section
	}
	if len(deploymentIDs) > 0 {
		section.Items = append(section.Items, windowItem("deployment_count", "Deployments", len(deploymentIDs), "deployments", "Deployment", deploymentIDs))
	}
	if len(releaseIDs) > 0 {
		section.Items = append(section.Items, windowItem("release_count", "Published releases", len(releaseIDs), "releases", "Release", releaseIDs))
	}
	section.Note = "Deployment outcomes are not available from the ingested GitHub deployment records."
	return section
}

func inWindow(at time.Time, win metrics.Window) bool {
	return !at.IsZero() && !at.Before(win.Start) && at.Before(win.End)
}

func windowItem(metric, label string, value int, unit, kind string, ids []string) boardItem {
	return boardItem{
		Metric: metric, Label: label, Value: float64(value), Unit: unit, EpistemicState: evidence.StateCalculated.String(),
		Evidence: []evidence.Evidence{{Type: kind, Source: "github", ObjectIDs: ids, State: evidence.StateCalculated}},
	}
}

func jiraWorkSection(pop metrics.Population) boardSection {
	section := boardSection{ID: "work_management", Title: "Work Management"}
	if len(pop.JiraIssues) == 0 {
		section.Note = "no Jira work items in this scope"
		return section
	}
	issueIDs := make([]string, 0, len(pop.JiraIssues))
	for _, issue := range pop.JiraIssues {
		issueIDs = append(issueIDs, issue.ExternalID)
	}
	sprintIDs := make([]string, 0, len(pop.JiraSprints))
	for _, sprint := range pop.JiraSprints {
		sprintIDs = append(sprintIDs, sprint.ExternalID)
	}
	section.Items = []boardItem{
		jiraSnapshotItem("jira_issue_count", "Jira work items", len(issueIDs), "issues", "Issue", issueIDs),
		jiraSnapshotItem("jira_assigned_issue_count", "Work items assigned", len(pop.JiraAssignedIssueIDs), "issues", "Issue", pop.JiraAssignedIssueIDs),
		jiraSnapshotItem("jira_sprint_count", "Sprints represented", len(sprintIDs), "sprints", "Sprint", sprintIDs),
		jiraSnapshotItem("jira_sprint_memberships", "Sprint memberships", len(pop.JiraSprintMembershipIssueIDs), "issues", "Issue", pop.JiraSprintMembershipIssueIDs),
	}
	section.Items = append(section.Items, jiraBreakdownItems(pop.JiraIssues)...)
	section.Note = "Current Jira snapshot; counts are not limited to the selected time window."
	return section
}

func jiraBreakdownItems(issues []metrics.Entity[ontology.JiraIssue]) []boardItem {
	statusIDs, typeIDs := map[string][]string{}, map[string][]string{}
	for _, issue := range issues {
		status := issue.Value.Status
		if status == "" {
			status = "Unknown"
		}
		issueType := issue.Value.IssueType
		if issueType == "" {
			issueType = "Unknown"
		}
		statusIDs[status] = append(statusIDs[status], issue.ExternalID)
		typeIDs[issueType] = append(typeIDs[issueType], issue.ExternalID)
	}
	var items []boardItem
	for _, group := range []struct {
		prefix, label string
		byValue       map[string][]string
	}{{"jira_status", "Work items by status", statusIDs}, {"jira_type", "Work items by type", typeIDs}} {
		values := make([]string, 0, len(group.byValue))
		for value := range group.byValue {
			values = append(values, value)
		}
		sort.Strings(values)
		for i, value := range values {
			items = append(items, jiraSnapshotItem(
				group.prefix+"_"+strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), " ", "_"))+"_"+strconv.Itoa(i),
				group.label+": "+value, len(group.byValue[value]), "issues", "Issue", group.byValue[value],
			))
		}
	}
	return items
}

func jiraSnapshotItem(metric, label string, value int, unit, kind string, ids []string) boardItem {
	return boardItem{
		Metric: metric, Label: label, Value: float64(value), Unit: unit, EpistemicState: evidence.StateCalculated.String(),
		Evidence: []evidence.Evidence{{Type: kind, Source: "jira", ObjectIDs: ids, State: evidence.StateCalculated}},
	}
}

// evidenceCoverage reports, per epistemic state present in the board's
// items, how many distinct objects back it — §28's "how strong is the
// evidence", in object counts, never a confidence score.
func evidenceCoverage(items []boardItem) boardSection {
	sec := boardSection{ID: "evidence_coverage", Title: "Evidence Coverage"}
	perState := map[string]map[string]bool{}
	for _, it := range items {
		for _, ev := range it.Evidence {
			m := perState[ev.State.String()]
			if m == nil {
				m = map[string]bool{}
				perState[ev.State.String()] = m
			}
			for _, id := range ev.ObjectIDs {
				m[id] = true
			}
		}
	}
	for _, s := range []evidence.State{evidence.StateObserved, evidence.StateCalculated, evidence.StateInferred, evidence.StateHypothesized, evidence.StateUnknown} {
		if ids, ok := perState[s.String()]; ok {
			objectIDs := make([]string, 0, len(ids))
			for id := range ids {
				objectIDs = append(objectIDs, id)
			}
			sort.Strings(objectIDs)
			sec.Items = append(sec.Items, boardItem{Metric: "evidence_coverage", Label: s.String(),
				Value: float64(len(ids)), Unit: "objects", EpistemicState: s.String(),
				Evidence: []evidence.Evidence{{Type: "Object", ObjectIDs: objectIDs, State: s}}})
		}
	}
	if len(sec.Items) == 0 {
		sec.Note = "no computed values to cover — nothing has evidence yet"
	}
	return sec
}

// handleBoard serves GET /board?scope=<extID>&start=<RFC3339>&end=<RFC3339>.
// Responses are memoized per scope+window (TTL cache + ETag revalidation):
// the board is deterministic per key and the store is immutable while a
// server holds the db.
func (a *API) handleBoard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	start, err := time.Parse(time.RFC3339, q.Get("start"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "start must be RFC3339")
		return
	}
	end, err := time.Parse(time.RFC3339, q.Get("end"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "end must be RFC3339")
		return
	}
	key := q.Get("scope") + "|" + q.Get("start") + "|" + q.Get("end")
	if body, etag, ok := a.board.get(key); ok {
		writeBoardBytes(w, r, body, etag)
		return
	}
	pop, err := Population(r.Context(), a.store, q.Get("scope"))
	if err != nil {
		// An unknown scope is the caller naming something wrong (a navigation
		// typo on the board), not a server fault.
		if errors.Is(err, knowledge.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "unknown scope")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	body, err := json.Marshal(buildBoard(pop, metrics.Window{Start: start, End: end}))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "marshal board: "+err.Error())
		return
	}
	writeBoardBytes(w, r, body, a.board.put(key, body))
}

func writeBoardBytes(w http.ResponseWriter, r *http.Request, body []byte, etag string) {
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache") // revalidate against the TTL cache
	// A write error here is unreportable — the client is gone and the
	// handler has no error channel left.
	_, _ = w.Write(body)
}
