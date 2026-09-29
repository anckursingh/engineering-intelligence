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

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// Population walks org → repos → PRs → reviews + AI contributions, then the
// telemetry of each contribution's recorded session (interactions, runs,
// tasks) and returns the typed objects with their store external IDs.
// ponytail: org scope only — repo scoping joins when a question needs it.
func Population(ctx context.Context, store knowledge.KnowledgeStore, scope string) (metrics.Population, error) {
	var pop metrics.Population
	seenSession := map[string]bool{}
	org, err := store.GetByExternalID(ctx, scope)
	if err != nil {
		return pop, fmt.Errorf("intelligence: resolve scope %s: %w", scope, err)
	}
	repos, err := store.Traverse(ctx, org.Koid, string(ontology.RelBelongsTo), knowledge.Inbound, 1)
	if err != nil {
		return pop, fmt.Errorf("intelligence: traverse repos of %s: %w", scope, err)
	}
	for _, repo := range repos {
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
