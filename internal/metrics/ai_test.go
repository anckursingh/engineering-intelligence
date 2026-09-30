// AI development metrics tests (§27): deterministic over a Population's AI
// telemetry — exact values, event-time anchoring (§19), evidence-based
// attribution counting (§26), and honest absence for undefined ratios.
package metrics

import (
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

var aiWin = Window{Start: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}

// aiPr is a merged PR created two days before it merged.
func aiPr(extID string, num int, merged time.Time) Entity[ontology.PullRequest] {
	return pr(extID, num, merged.Add(-2*24*time.Hour), merged, true)
}

func contrib(extID string, prNum int, level ontology.AttributionLevel, source, ev string) Entity[ontology.CodeContribution] {
	return Entity[ontology.CodeContribution]{
		ExternalID: extID,
		Value: ontology.CodeContribution{
			Repository:  "acme/widgets",
			PRNumber:    prNum,
			Attribution: ontology.Attribution{Level: level, Source: source, Evidence: ev},
		},
	}
}

func run(extID string, started time.Time, cost float64) Entity[ontology.AgentRun] {
	return Entity[ontology.AgentRun]{
		ExternalID: extID,
		Value:      ontology.AgentRun{Source: "claude-code", ID: extID, StartedAt: started, CostUSD: cost, CostReported: true},
	}
}

func task(extID string, completedAt time.Time, status string, retries int, intervened bool) Entity[ontology.AgentTask] {
	return Entity[ontology.AgentTask]{
		ExternalID: extID,
		Value: ontology.AgentTask{Source: "claude-code", ID: extID, Status: status, Retries: retries,
			HumanIntervention: intervened, CompletedAt: completedAt},
	}
}

func interaction(extID string, started time.Time) Entity[ontology.Interaction] {
	return Entity[ontology.Interaction]{
		ExternalID: extID,
		Value:      ontology.Interaction{Source: "claude-code", ID: extID, StartedAt: started},
	}
}

func oneValue(t *testing.T, obs []Observation, want float64) {
	t.Helper()
	if len(obs) != 1 {
		t.Fatalf("observations = %d, want 1", len(obs))
	}
	if obs[0].Value != want {
		t.Errorf("value = %v, want %v", obs[0].Value, want)
	}
}

func none(t *testing.T, obs []Observation) {
	t.Helper()
	if len(obs) != 0 {
		t.Fatalf("observations = %d, want none (undefined is absence, not zero)", len(obs))
	}
}

func TestAIAssistedPRPctExact(t *testing.T) {
	pop := Population{
		PullRequests: []Entity[ontology.PullRequest]{
			aiPr("p1", 1, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)),
			aiPr("p2", 2, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)),
			aiPr("p3", 3, time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)),
			aiPr("p4", 4, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)),
		},
		CodeContributions: []Entity[ontology.CodeContribution]{
			contrib("c2", 2, ontology.AttributionDirect, "claude-code", "session:s1"),
			contrib("c4", 4, ontology.AttributionStrong, "claude-code", "session:s2"),
		},
	}
	got := AIAssistedPRPct(pop, aiWin)
	oneValue(t, got, 50.0)
	if len(got[0].Evidence) != 2 || len(got[0].Evidence[0].ObjectIDs) != 4 || len(got[0].Evidence[1].ObjectIDs) != 2 {
		t.Errorf("evidence = %v, want the 4 PRs + the 2 qualifying contributions", got[0].Evidence)
	}
}

// §26 counting: only valid DIRECT/STRONG telemetry claims count. INFERRED,
// UNKNOWN, a bare DIRECT (no source), and a contribution for a PR that does
// not exist — none count.
func TestAIAssistedPRPctAttributionFiltering(t *testing.T) {
	pop := Population{
		PullRequests: []Entity[ontology.PullRequest]{
			aiPr("p1", 1, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)),
			aiPr("p2", 2, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)),
		},
		CodeContributions: []Entity[ontology.CodeContribution]{
			contrib("c1", 1, ontology.AttributionInferred, "", ""),
			contrib("c2", 2, ontology.AttributionUnknown, "", ""),
			contrib("c3", 1, ontology.AttributionDirect, "", "session:s3"), // invalid: bare DIRECT
			{ExternalID: "c4", Value: ontology.CodeContribution{ // valid claim, wrong repo
				Repository:  "other/repo",
				PRNumber:    1,
				Attribution: ontology.Attribution{Level: ontology.AttributionDirect, Source: "claude-code", Evidence: "session:s4"},
			}},
		},
	}

	got := AIAssistedPRPct(pop, aiWin)
	oneValue(t, got, 0.0)
	if len(got[0].Evidence) != 1 {
		t.Errorf("evidence = %v, want the PRs only — no qualifying contributions", got[0].Evidence)
	}
}

func TestAIAssistedPRPctAbsence(t *testing.T) {
	none(t, AIAssistedPRPct(Population{}, aiWin)) // empty population
	none(t, AIAssistedPRPct(Population{
		PullRequests: []Entity[ontology.PullRequest]{
			aiPr("p1", 1, time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)), // merged outside window
		},
	}, aiWin))
}

func TestAIInteractionVolume(t *testing.T) {
	pop := Population{Interactions: []Entity[ontology.Interaction]{
		interaction("i1", time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)),
		interaction("i2", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)),
		interaction("i2", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)), // duplicate collapses
		interaction("i3", time.Time{}),                                 // missing event time
		interaction("i4", time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)),
	}}
	oneValue(t, AIInteractionVolume(pop, aiWin), 2.0)
	none(t, AIInteractionVolume(Population{}, aiWin))
}

func TestAIRunVolume(t *testing.T) {
	pop := Population{AgentRuns: []Entity[ontology.AgentRun]{
		run("r1", time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), 1.0),
		run("r2", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), 2.0),
		run("r3", time.Time{}, 3.0),
		run("r4", time.Date(2026, 9, 30, 23, 30, 0, 0, time.UTC), 4.0),
	}}
	oneValue(t, AIRunVolume(pop, aiWin), 3.0)
	none(t, AIRunVolume(Population{}, aiWin))
}

func TestAgentTaskCompletion(t *testing.T) {
	pop := Population{AgentTasks: []Entity[ontology.AgentTask]{
		task("t1", time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), "completed", 0, false),
		task("t2", time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), "completed", 1, true),
		task("t3", time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), "failed", 2, false),
		task("t4", time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), "failed", 0, true),
		task("t5", time.Time{}, "completed", 0, false),                               // no event time — pending, excluded
		task("t6", time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC), "pending", 0, false), // unfinished status
	}}
	oneValue(t, AgentTaskCompletion(pop, aiWin), 50.0)
	none(t, AgentTaskCompletion(Population{}, aiWin))
}

func TestHumanInterventionRate(t *testing.T) {
	pop := Population{AgentTasks: []Entity[ontology.AgentTask]{
		task("t1", time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), "completed", 0, true),
		task("t2", time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), "completed", 0, false),
		task("t3", time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), "failed", 0, true),
		task("t4", time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), "failed", 0, false),
	}}
	oneValue(t, HumanInterventionRate(pop, aiWin), 50.0)
	none(t, HumanInterventionRate(Population{}, aiWin))
}

func TestRetryRate(t *testing.T) {
	pop := Population{AgentTasks: []Entity[ontology.AgentTask]{
		task("t1", time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), "completed", 1, false),
		task("t2", time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), "completed", 0, false),
		task("t3", time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), "failed", 2, false),
		task("t4", time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), "failed", 0, false),
	}}
	oneValue(t, RetryRate(pop, aiWin), 50.0)
	none(t, RetryRate(Population{}, aiWin))
}

func TestAICost(t *testing.T) {
	pop := Population{AgentRuns: []Entity[ontology.AgentRun]{
		run("r1", time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), 1.5),
		run("r2", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), 2.25),
		run("r2", time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), 2.25), // duplicate collapses
		run("r3", time.Time{}, 9.0),                                  // missing event time
	}}
	oneValue(t, AICost(pop, aiWin), 3.75)
	none(t, AICost(Population{}, aiWin))
}

func TestAICostUnreportedIsAbsent(t *testing.T) {
	pop := Population{AgentRuns: []Entity[ontology.AgentRun]{
		{ExternalID: "r1", Value: ontology.AgentRun{
			Source: "claude-code", ID: "r1", StartedAt: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
		}},
	}}
	none(t, AICost(pop, aiWin))
}

func TestAICostReportedZeroRemainsAnObservation(t *testing.T) {
	zero := run("r1", time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), 0)
	oneValue(t, AICost(Population{AgentRuns: []Entity[ontology.AgentRun]{zero}}, aiWin), 0)
}

func TestAICostNonZeroLegacyValueIsMeasured(t *testing.T) {
	legacy := Entity[ontology.AgentRun]{ExternalID: "r1", Value: ontology.AgentRun{
		Source: "legacy-source", ID: "r1", StartedAt: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), CostUSD: 1.25,
	}}
	oneValue(t, AICost(Population{AgentRuns: []Entity[ontology.AgentRun]{legacy}}, aiWin), 1.25)
}

func TestAICostPartialCoverageIsAbsent(t *testing.T) {
	unknown := Entity[ontology.AgentRun]{ExternalID: "r2", Value: ontology.AgentRun{
		Source: "claude-code", ID: "r2", StartedAt: time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC),
	}}
	pop := Population{AgentRuns: []Entity[ontology.AgentRun]{
		run("r1", time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), 1.5),
		unknown,
	}}
	none(t, AICost(pop, aiWin))
}

func TestCostPerCompletedTask(t *testing.T) {
	pop := Population{
		AgentRuns: []Entity[ontology.AgentRun]{
			run("r1", time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), 3.75),
		},
		AgentTasks: []Entity[ontology.AgentTask]{
			task("t1", time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), "completed", 0, false),
			task("t2", time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC), "completed", 0, false),
			task("t3", time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), "completed", 0, false),
			task("t4", time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), "failed", 0, false),
		},
	}
	oneValue(t, CostPerCompletedTask(pop, aiWin), 1.25)
	// Undefined ratio is absence, not zero: no completed tasks, and no runs.
	none(t, CostPerCompletedTask(Population{
		AgentRuns: pop.AgentRuns,
	}, aiWin))
	none(t, CostPerCompletedTask(Population{
		AgentTasks: pop.AgentTasks,
	}, aiWin))
}

func TestCostPerCompletedTaskUnknownCostIsAbsent(t *testing.T) {
	pop := Population{
		AgentRuns: []Entity[ontology.AgentRun]{{ExternalID: "r1", Value: ontology.AgentRun{
			Source: "claude-code", ID: "r1", StartedAt: time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
		}}},
		AgentTasks: []Entity[ontology.AgentTask]{task("t1", time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), "completed", 0, false)},
	}
	none(t, CostPerCompletedTask(pop, aiWin))
}

// §18 timezone boundary: events are classified by instant, not wall clock.
// An interaction started 2026-09-30 23:30 UTC is September 30 in New York but
// October 1 in Tokyo — it stays inside the window either way.
func TestAIMetricsTimezoneBoundary(t *testing.T) {
	instant := time.Date(2026, 9, 30, 23, 30, 0, 0, time.UTC)
	pop := Population{Interactions: []Entity[ontology.Interaction]{
		interaction("i1", inZone(t, instant, "America/New_York")),
		interaction("i2", inZone(t, instant, "Asia/Tokyo")),
	}}
	oneValue(t, AIInteractionVolume(pop, aiWin), 2.0)
}
