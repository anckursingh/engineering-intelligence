// population.go: the §22 flow's graph step — build a metric Population by
// walking the knowledge graph from an organization scope root: repos →
// PRs → reviews. The store stays the only source of objects; the metrics
// keep their own window filtering.
package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// Population walks one or more graph roots and combines their typed objects.
// Multiple roots are comma-separated to place source populations on one board.
func Population(ctx context.Context, store knowledge.KnowledgeStore, scope string) (metrics.Population, error) {
	var combined metrics.Population
	for _, part := range strings.Split(scope, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return combined, fmt.Errorf("intelligence: empty scope in composite scope %q", scope)
		}
		pop, err := populationForScope(ctx, store, part)
		if err != nil {
			return combined, err
		}
		combined = mergePopulations(combined, pop)
	}
	return combined, nil
}

func populationForScope(ctx context.Context, store knowledge.KnowledgeStore, scope string) (metrics.Population, error) {
	var pop metrics.Population
	seenSession := map[string]bool{}
	org, err := store.GetByExternalID(ctx, scope)
	if err != nil {
		return pop, fmt.Errorf("intelligence: resolve scope %s: %w", scope, err)
	}
	if org.TypeName == "JiraProject" {
		return jiraPopulation(ctx, store, org)
	}
	repos, err := store.Traverse(ctx, org.Koid, string(ontology.RelBelongsTo), knowledge.Inbound, 1)
	if err != nil {
		return pop, fmt.Errorf("intelligence: traverse repos of %s: %w", scope, err)
	}
	for _, repo := range repos {
		deployments, err := store.Traverse(ctx, repo.Koid, string(ontology.RelContainsDeployment), knowledge.Outbound, 1)
		if err != nil {
			return pop, fmt.Errorf("intelligence: traverse deployments of %s: %w", repo.ExternalID, err)
		}
		for _, ko := range deployments {
			deployment, err := convert[ontology.Deployment](ko)
			if err != nil {
				return pop, fmt.Errorf("intelligence: decode deployment %s: %w", ko.ExternalID, err)
			}
			pop.Deployments = append(pop.Deployments, metrics.Entity[ontology.Deployment]{ExternalID: ko.ExternalID, Value: deployment})
		}
		releases, err := store.Traverse(ctx, repo.Koid, string(ontology.RelContainsRelease), knowledge.Outbound, 1)
		if err != nil {
			return pop, fmt.Errorf("intelligence: traverse releases of %s: %w", repo.ExternalID, err)
		}
		for _, ko := range releases {
			release, err := convert[ontology.Release](ko)
			if err != nil {
				return pop, fmt.Errorf("intelligence: decode release %s: %w", ko.ExternalID, err)
			}
			pop.Releases = append(pop.Releases, metrics.Entity[ontology.Release]{ExternalID: ko.ExternalID, Value: release})
		}
		builds, err := store.Traverse(ctx, repo.Koid, string(ontology.RelContainsBuild), knowledge.Outbound, 1)
		if err != nil {
			return pop, fmt.Errorf("intelligence: traverse builds of %s: %w", repo.ExternalID, err)
		}
		for _, b := range builds {
			build, err := convert[ontology.Build](b)
			if err != nil {
				return pop, fmt.Errorf("intelligence: decode build %s: %w", b.ExternalID, err)
			}
			pop.Builds = append(pop.Builds, metrics.Entity[ontology.Build]{ExternalID: b.ExternalID, Value: build})
		}
		prs, err := store.Traverse(ctx, repo.Koid, string(ontology.RelTargets), knowledge.Inbound, 1)
		if err != nil {
			return pop, fmt.Errorf("intelligence: traverse PRs of %s: %w", repo.ExternalID, err)
		}
		for _, pr := range prs {
			p, err := convert[ontology.PullRequest](pr)
			if err != nil {
				return pop, fmt.Errorf("intelligence: decode PR %s: %w", pr.ExternalID, err)
			}
			pop.PullRequests = append(pop.PullRequests, metrics.Entity[ontology.PullRequest]{ExternalID: pr.ExternalID, Value: p})
			reviews, err := store.Traverse(ctx, pr.Koid, string(ontology.RelContainsReview), knowledge.Outbound, 1)
			if err != nil {
				return pop, fmt.Errorf("intelligence: traverse reviews of %s: %w", pr.ExternalID, err)
			}
			for _, rev := range reviews {
				r, err := convert[ontology.Review](rev)
				if err != nil {
					return pop, fmt.Errorf("intelligence: decode review %s: %w", rev.ExternalID, err)
				}
				pop.Reviews = append(pop.Reviews, metrics.Entity[ontology.Review]{ExternalID: rev.ExternalID, Value: r})
			}
			contribs, err := store.Traverse(ctx, pr.Koid, string(ontology.RelAIContributes), knowledge.Inbound, 1)
			if err != nil {
				return pop, fmt.Errorf("intelligence: traverse AI contributions of %s: %w", pr.ExternalID, err)
			}
			for _, co := range contribs {
				c, err := convert[ontology.CodeContribution](co)
				if err != nil {
					return pop, fmt.Errorf("intelligence: decode AI contribution %s: %w", co.ExternalID, err)
				}
				pop.CodeContributions = append(pop.CodeContributions, metrics.Entity[ontology.CodeContribution]{ExternalID: co.ExternalID, Value: c})
				// Telemetry reach (§27): the contribution records its session
				// first-hand at ingest, so the walk resolves it directly instead
				// of edge-hopping through task links that can be absent. A
				// contribution without a recorded session leaves that session's
				// telemetry out of this population (honest absence).
				if c.Session == "" || seenSession[c.Session] {
					continue
				}
				seenSession[c.Session] = true
				session, err := store.GetByExternalID(ctx, c.Session)
				if err != nil {
					if errors.Is(err, knowledge.ErrNotFound) {
						continue // the contribution names a session the store lacks
					}
					return pop, fmt.Errorf("intelligence: resolve session %s of AI contribution %s: %w", c.Session, co.ExternalID, err)
				}
				if err := collectTelemetry[ontology.Interaction](ctx, store, session.Koid, ontology.RelContainsInteraction, "Interaction", &pop.Interactions); err != nil {
					return pop, err
				}
				if err := collectTelemetry[ontology.AgentRun](ctx, store, session.Koid, ontology.RelContainsRun, "AgentRun", &pop.AgentRuns); err != nil {
					return pop, err
				}
				if err := collectSessionTasks(ctx, store, session, &pop); err != nil {
					return pop, err
				}
			}
		}
	}
	return pop, nil
}

func mergePopulations(dst, src metrics.Population) metrics.Population {
	dst.PullRequests = mergeEntities(dst.PullRequests, src.PullRequests)
	dst.Reviews = mergeEntities(dst.Reviews, src.Reviews)
	dst.Builds = mergeEntities(dst.Builds, src.Builds)
	dst.CodeContributions = mergeEntities(dst.CodeContributions, src.CodeContributions)
	dst.AgentRuns = mergeEntities(dst.AgentRuns, src.AgentRuns)
	dst.AgentTasks = mergeEntities(dst.AgentTasks, src.AgentTasks)
	dst.Interactions = mergeEntities(dst.Interactions, src.Interactions)
	dst.JiraIssues = mergeEntities(dst.JiraIssues, src.JiraIssues)
	dst.JiraSprints = mergeEntities(dst.JiraSprints, src.JiraSprints)
	dst.JiraAssignedIssueIDs = mergeStrings(dst.JiraAssignedIssueIDs, src.JiraAssignedIssueIDs)
	dst.JiraSprintMembershipIssueIDs = append(dst.JiraSprintMembershipIssueIDs, src.JiraSprintMembershipIssueIDs...)
	dst.Deployments = mergeEntities(dst.Deployments, src.Deployments)
	dst.Releases = mergeEntities(dst.Releases, src.Releases)
	if len(src.TaskSessions) > 0 {
		if dst.TaskSessions == nil {
			dst.TaskSessions = map[string]string{}
		}
		for task, session := range src.TaskSessions {
			dst.TaskSessions[task] = session
		}
	}
	return dst
}

func mergeEntities[T any](left, right []metrics.Entity[T]) []metrics.Entity[T] {
	seen := make(map[string]bool, len(left)+len(right))
	merged := make([]metrics.Entity[T], 0, len(left)+len(right))
	for _, entity := range append(left, right...) {
		if seen[entity.ExternalID] {
			continue
		}
		seen[entity.ExternalID] = true
		merged = append(merged, entity)
	}
	return merged
}

func mergeStrings(left, right []string) []string {
	seen := make(map[string]bool, len(left)+len(right))
	merged := make([]string, 0, len(left)+len(right))
	for _, value := range append(left, right...) {
		if seen[value] {
			continue
		}
		seen[value] = true
		merged = append(merged, value)
	}
	return merged
}

func jiraPopulation(ctx context.Context, store knowledge.KnowledgeStore, project knowledge.KnowledgeObject) (metrics.Population, error) {
	var pop metrics.Population
	issues, err := store.Traverse(ctx, project.Koid, string(ontology.RelContainsIssue), knowledge.Outbound, 1)
	if err != nil {
		return pop, fmt.Errorf("intelligence: traverse Jira issues of %s: %w", project.ExternalID, err)
	}
	sprints := map[string]knowledge.KnowledgeObject{}
	for _, ko := range issues {
		if ko.TypeName != "Issue" && ko.TypeName != "Epic" {
			continue
		}
		issue, err := convert[ontology.JiraIssue](ko)
		if err != nil {
			return pop, fmt.Errorf("intelligence: decode Jira issue %s: %w", ko.ExternalID, err)
		}
		pop.JiraIssues = append(pop.JiraIssues, metrics.Entity[ontology.JiraIssue]{ExternalID: ko.ExternalID, Value: issue})
		if err := collectJiraLinks(ctx, store, ko, &pop, sprints); err != nil {
			return pop, err
		}
	}
	for _, ko := range sprints {
		sprint, err := convert[ontology.JiraSprint](ko)
		if err != nil {
			return pop, fmt.Errorf("intelligence: decode Jira sprint %s: %w", ko.ExternalID, err)
		}
		pop.JiraSprints = append(pop.JiraSprints, metrics.Entity[ontology.JiraSprint]{ExternalID: ko.ExternalID, Value: sprint})
	}
	return pop, nil
}

func collectJiraLinks(ctx context.Context, store knowledge.KnowledgeStore, issue knowledge.KnowledgeObject, pop *metrics.Population, sprints map[string]knowledge.KnowledgeObject) error {
	assigned, err := store.Traverse(ctx, issue.Koid, string(ontology.RelAssignedTo), knowledge.Outbound, 1)
	if err != nil {
		return fmt.Errorf("intelligence: traverse assignee of %s: %w", issue.ExternalID, err)
	}
	if len(assigned) > 0 {
		pop.JiraAssignedIssueIDs = append(pop.JiraAssignedIssueIDs, issue.ExternalID)
	}
	linked, err := store.Traverse(ctx, issue.Koid, string(ontology.RelInSprint), knowledge.Outbound, 1)
	if err != nil {
		return fmt.Errorf("intelligence: traverse sprints of %s: %w", issue.ExternalID, err)
	}
	for _, sprint := range linked {
		pop.JiraSprintMembershipIssueIDs = append(pop.JiraSprintMembershipIssueIDs, issue.ExternalID)
		sprints[sprint.ExternalID] = sprint
	}
	return nil
}

// convert round-trips a knowledge object's properties into its typed form —
// the same reconstruction discipline the §20 evidence contract uses.
func convert[T any](ko knowledge.KnowledgeObject) (T, error) {
	var v T
	props, err := json.Marshal(ko.Properties)
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(props, &v); err != nil {
		return v, err
	}
	return v, nil
}

// collectSessionTasks walks the session's CONTAINS_TASK children and records
// each task plus its containing session — the pairing the task-failure
// report (item 46) groups by. collectTelemetry cannot: only tasks carry a
// location.
func collectSessionTasks(ctx context.Context, store knowledge.KnowledgeStore, session knowledge.KnowledgeObject, pop *metrics.Population) error {
	kos, err := store.Traverse(ctx, session.Koid, string(ontology.RelContainsTask), knowledge.Outbound, 1)
	if err != nil {
		return fmt.Errorf("intelligence: traverse tasks of %s: %w", session.ExternalID, err)
	}
	for _, ko := range kos {
		if ko.TypeName != "AgentTask" {
			continue
		}
		v, err := convert[ontology.AgentTask](ko)
		if err != nil {
			return fmt.Errorf("intelligence: decode AgentTask %s: %w", ko.ExternalID, err)
		}
		if pop.TaskSessions == nil {
			pop.TaskSessions = map[string]string{}
		}
		pop.TaskSessions[ko.ExternalID] = session.ExternalID
		pop.AgentTasks = append(pop.AgentTasks, metrics.Entity[ontology.AgentTask]{ExternalID: ko.ExternalID, Value: v})
	}
	return nil
}

// collectTelemetry walks one containment edge from the session and appends
// the decoded children of the given type. Each session is visited once — the
// population walk dedupes by the contribution's recorded session — so the
// metrics read one copy of every child.
func collectTelemetry[T any](ctx context.Context, store knowledge.KnowledgeStore, from string, rel ontology.RelType, typ string, dst *[]metrics.Entity[T]) error {
	kos, err := store.Traverse(ctx, from, string(rel), knowledge.Outbound, 1)
	if err != nil {
		return fmt.Errorf("intelligence: traverse %s: %w", rel, err)
	}
	for _, ko := range kos {
		if ko.TypeName != typ {
			continue
		}
		v, err := convert[T](ko)
		if err != nil {
			return fmt.Errorf("intelligence: decode %s %s: %w", typ, ko.ExternalID, err)
		}
		*dst = append(*dst, metrics.Entity[T]{ExternalID: ko.ExternalID, Value: v})
	}
	return nil
}
