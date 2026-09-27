// AI development metrics (§27): deterministic functions of the AI telemetry
// in a Population. Event times only (§19) — interactions and runs anchor at
// started_at, tasks at completed_at. Attribution counting is evidence-based
// (§26): only valid DIRECT/STRONG telemetry claims count as AI-assisted —
// never code style, never lines of AI code as a productivity metric.
//
// AI-related rework has no rework signal anywhere in the ontology — it joins
// when a rework source exists (§17's "the set that has data").
package metrics

import (
	"sort"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// Definitions of the AI metric set (§27). All read telemetry event times and
// cite the objects they were computed from.
var (
	DefAIAssistedPRPct = Definition{
		Name:        "ai_assisted_pr_pct",
		Formula:     "count(merged PRs with >=1 valid DIRECT/STRONG CodeContribution) / count(merged PRs) * 100",
		Population:  "merged PullRequests + CodeContributions",
		Window:      "merged_at in window (event time)",
		Sources:     []string{"github", "ai-telemetry"},
		Filters:     []string{"merged", "valid timestamps", "contribution matches PR repository+number", "attribution valid (§26) with level DIRECT or STRONG"},
		Aggregation: "one observation per window",
		Limitations: []string{"INFERRED/UNKNOWN attribution excluded — only source-telemetry-backed claims count"},
	}
	DefAIInteractionVolume = Definition{
		Name:        "ai_interaction_volume",
		Formula:     "count(Interactions)",
		Population:  "Interactions",
		Window:      "started_at in window (event time)",
		Sources:     []string{"ai-telemetry"},
		Filters:     []string{"started_at set"},
		Aggregation: "count — one observation per window",
		Limitations: []string{"counts only what the telemetry source records"},
	}
	DefAIRunVolume = Definition{
		Name:        "ai_run_volume",
		Formula:     "count(AgentRuns)",
		Population:  "AgentRuns",
		Window:      "started_at in window (event time)",
		Sources:     []string{"ai-telemetry"},
		Filters:     []string{"started_at set"},
		Aggregation: "count — one observation per window",
		Limitations: []string{"counts only what the telemetry source records"},
	}
	DefAgentTaskCompletion = Definition{
		Name:        "ai_task_completion",
		Formula:     "count(finished tasks with status completed) / count(finished tasks) * 100",
		Population:  "AgentTasks",
		Window:      "completed_at in window (event time)",
		Sources:     []string{"ai-telemetry"},
		Filters:     []string{"completed_at set", "status completed or failed"},
		Aggregation: "one observation per window",
		Limitations: []string{"pending tasks have no event time and are excluded"},
	}
	DefHumanInterventionRate = Definition{
		Name:        "human_intervention_rate",
		Formula:     "count(finished tasks with human_intervention) / count(finished tasks) * 100",
		Population:  "AgentTasks",
		Window:      "completed_at in window (event time)",
		Sources:     []string{"ai-telemetry"},
		Filters:     []string{"completed_at set", "status completed or failed"},
		Aggregation: "one observation per window",
		Limitations: []string{"intervention is a telemetry fact, not a judgment"},
	}
	DefRetryRate = Definition{
		Name:        "retry_rate",
		Formula:     "count(finished tasks with retries > 0) / count(finished tasks) * 100",
		Population:  "AgentTasks",
		Window:      "completed_at in window (event time)",
		Sources:     []string{"ai-telemetry"},
		Filters:     []string{"completed_at set", "status completed or failed"},
		Aggregation: "one observation per window",
		Limitations: []string{"retry counting is source-defined"},
	}
	DefAICost = Definition{
		Name:        "ai_cost",
		Formula:     "sum(cost_usd)",
		Population:  "AgentRuns",
		Window:      "started_at in window (event time)",
		Sources:     []string{"ai-telemetry"},
		Filters:     []string{"started_at set"},
		Aggregation: "sum — one observation per window",
		Limitations: []string{"currency and accounting are source-defined"},
	}
	DefCostPerCompletedTask = Definition{
		Name:        "cost_per_completed_task",
		Formula:     "ai_cost / count(completed tasks)",
		Population:  "AgentRuns + AgentTasks",
		Window:      "runs started_at and tasks completed_at in window (event time)",
		Sources:     []string{"ai-telemetry"},
		Filters:     []string{"runs exist", ">=1 task completed in window"},
		Aggregation: "one observation per window",
		Limitations: []string{"undefined (no runs or no completed tasks) is absence, not zero"},
	}
)

// AIAssistedPRPct computes the share of merged PRs (merged_at in window)
// carrying at least one valid DIRECT/STRONG AI contribution (§26-27). A PR
// with only INFERRED or UNKNOWN attribution does not count as AI-assisted —
// no telemetry-backed claim, no count.
func AIAssistedPRPct(pop Population, w Window) []Observation {
	if windowEmpty(w) {
		return nil
	}
	type key struct {
		repo string
		num  int
	}
	prs := map[key]string{}
	for _, e := range pop.PullRequests {
		if !valid(e.Value) || e.Value.MergedAt.Before(w.Start) || !e.Value.MergedAt.Before(w.End) {
			continue
		}
		prs[key{e.Value.Repository, e.Value.Number}] = e.ExternalID // duplicates collapse
	}
	if len(prs) == 0 {
		return nil
	}
	assisted := map[key]bool{}
	var cIDs []string
	for _, c := range pop.CodeContributions {
		if c.Value.Attribution.Validate() != nil {
			continue // invalid claim — §26: bare DIRECT/STRONG carries no evidence
		}
		if c.Value.Attribution.Level != ontology.AttributionDirect && c.Value.Attribution.Level != ontology.AttributionStrong {
			continue
		}
		k := key{c.Value.Repository, c.Value.PRNumber}
		if _, ok := prs[k]; !ok {
			continue
		}
		assisted[k] = true
		cIDs = append(cIDs, c.ExternalID)
	}
	prIDs := make([]string, 0, len(prs))
	for _, id := range prs {
		prIDs = append(prIDs, id)
	}
	sort.Strings(prIDs)
	sort.Strings(cIDs)
	ev := []evidence.Evidence{
		{Type: "PullRequest", ObjectIDs: prIDs, State: evidence.StateCalculated},
	}
	if len(cIDs) > 0 {
		ev = append(ev, evidence.Evidence{Type: "CodeContribution", ObjectIDs: cIDs, State: evidence.StateCalculated})
	}
	return []Observation{{
		Metric:   DefAIAssistedPRPct.Name,
		Value:    float64(len(assisted)) / float64(len(prs)) * 100,
		Window:   w,
		Evidence: ev,
	}}
}

// AIInteractionVolume counts interactions whose started_at falls in the
// window (event time, §19).
func AIInteractionVolume(pop Population, w Window) []Observation {
	if windowEmpty(w) {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, e := range pop.Interactions {
		if seen[e.ExternalID] || e.Value.StartedAt.IsZero() || e.Value.StartedAt.Before(w.Start) || !e.Value.StartedAt.Before(w.End) {
			continue
		}
		seen[e.ExternalID] = true
		ids = append(ids, e.ExternalID)
	}
	return countObs(DefAIInteractionVolume.Name, "Interaction", ids, w)
}

// AIRunVolume counts agent runs whose started_at falls in the window.
func AIRunVolume(pop Population, w Window) []Observation {
	if windowEmpty(w) {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, e := range pop.AgentRuns {
		if seen[e.ExternalID] || e.Value.StartedAt.IsZero() || e.Value.StartedAt.Before(w.Start) || !e.Value.StartedAt.Before(w.End) {
			continue
		}
		seen[e.ExternalID] = true
		ids = append(ids, e.ExternalID)
	}
	return countObs(DefAIRunVolume.Name, "AgentRun", ids, w)
}

// countObs renders a window count; an empty count is honest absence (no
// objects, not zero objects — the telemetry source's silence is unknown).
func countObs(metric, typ string, ids []string, w Window) []Observation {
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return []Observation{{
		Metric:   metric,
		Value:    float64(len(ids)),
		Window:   w,
		Evidence: []evidence.Evidence{{Type: typ, ObjectIDs: ids, State: evidence.StateCalculated}},
	}}
}

// finishedTask is a task's outcome facts, scanned once.
type finishedTask struct {
	id         string
	completed  bool
	intervened bool
	retried    bool
}

// finishedTasks returns tasks with a completed_at in the window and a
// finished status — the denominator for the task-rate metrics.
func finishedTasks(pop Population, w Window) []finishedTask {
	seen := map[string]bool{}
	var out []finishedTask
	for _, e := range pop.AgentTasks {
		if seen[e.ExternalID] || e.Value.CompletedAt.IsZero() || e.Value.CompletedAt.Before(w.Start) || !e.Value.CompletedAt.Before(w.End) {
			continue
		}
		if e.Value.Status != "completed" && e.Value.Status != "failed" {
			continue
		}
		seen[e.ExternalID] = true
		out = append(out, finishedTask{
			id:         e.ExternalID,
			completed:  e.Value.Status == "completed",
			intervened: e.Value.HumanIntervention,
			retried:    e.Value.Retries > 0,
		})
	}
	return out
}

// taskRate renders count/total*100 over finished tasks; none is absence.
func taskRate(metric string, tasks []finishedTask, count func(finishedTask) bool, w Window) []Observation {
	if len(tasks) == 0 {
		return nil
	}
	n := 0
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.id)
		if count(t) {
			n++
		}
	}
	sort.Strings(ids)
	return []Observation{{
		Metric:   metric,
		Value:    float64(n) / float64(len(tasks)) * 100,
		Window:   w,
		Evidence: []evidence.Evidence{{Type: "AgentTask", ObjectIDs: ids, State: evidence.StateCalculated}},
	}}
}

// AgentTaskCompletion is the share of finished tasks whose status is
// completed.
func AgentTaskCompletion(pop Population, w Window) []Observation {
	return taskRate(DefAgentTaskCompletion.Name, finishedTasks(pop, w), func(t finishedTask) bool { return t.completed }, w)
}

// HumanInterventionRate is the share of finished tasks a human stepped into.
func HumanInterventionRate(pop Population, w Window) []Observation {
	return taskRate(DefHumanInterventionRate.Name, finishedTasks(pop, w), func(t finishedTask) bool { return t.intervened }, w)
}

// RetryRate is the share of finished tasks that took at least one retry.
func RetryRate(pop Population, w Window) []Observation {
	return taskRate(DefRetryRate.Name, finishedTasks(pop, w), func(t finishedTask) bool { return t.retried }, w)
}

// AICost sums run cost_usd for runs whose started_at falls in the window.
func AICost(pop Population, w Window) []Observation {
	if windowEmpty(w) {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	var sum float64
	for _, e := range pop.AgentRuns {
		if seen[e.ExternalID] || e.Value.StartedAt.IsZero() || e.Value.StartedAt.Before(w.Start) || !e.Value.StartedAt.Before(w.End) {
			continue
		}
		seen[e.ExternalID] = true
		ids = append(ids, e.ExternalID)
		sum += e.Value.CostUSD
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	return []Observation{{
		Metric:   DefAICost.Name,
		Value:    sum,
		Window:   w,
		Evidence: []evidence.Evidence{{Type: "AgentRun", ObjectIDs: ids, State: evidence.StateCalculated}},
	}}
}

// CostPerCompletedTask divides the window's AI cost by the tasks completed
// in the window. Both sides must have data — an undefined ratio is absence,
// not zero.
func CostPerCompletedTask(pop Population, w Window) []Observation {
	cost := AICost(pop, w)
	if len(cost) == 0 {
		return nil
	}
	tasks := finishedTasks(pop, w)
	n := 0
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.id)
		if t.completed {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	sort.Strings(ids)
	ev := append([]evidence.Evidence{}, cost[0].Evidence...)
	ev = append(ev, evidence.Evidence{Type: "AgentTask", ObjectIDs: ids, State: evidence.StateCalculated})
	return []Observation{{
		Metric:   DefCostPerCompletedTask.Name,
		Value:    cost[0].Value / float64(n),
		Window:   w,
		Evidence: ev,
	}}
}
