// population.go: the §22 flow's graph step — build a metric Population by
// walking the knowledge graph from an organization scope root: repos →
// PRs → reviews. The store stays the only source of objects; the metrics
// keep their own window filtering.
package intelligence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// Population walks org → repos → PRs → reviews + AI contributions and
// returns the typed objects with their store external IDs.
// ponytail: org scope only — repo scoping joins when a question needs it;
// run/task/interaction metrics are unreachable here because telemetry
// objects carry no org/repo edge — a telemetry connector that links them
// joins the candidate set then.
func Population(ctx context.Context, store knowledge.KnowledgeStore, scope string) (metrics.Population, error) {
	var pop metrics.Population
	org, err := store.GetByExternalID(ctx, scope)
	if err != nil {
		return pop, fmt.Errorf("intelligence: resolve scope %s: %w", scope, err)
	}
	repos, err := store.Traverse(ctx, org.Koid, string(ontology.RelBelongsTo), knowledge.Inbound, 1)
	if err != nil {
		return pop, fmt.Errorf("intelligence: traverse repos of %s: %w", scope, err)
	}
	for _, repo := range repos {
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
