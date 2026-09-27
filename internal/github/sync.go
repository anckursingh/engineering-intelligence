package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/anckursingh/engineering-intelligence/internal/checkpoint"
	"github.com/anckursingh/engineering-intelligence/internal/identity"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// Config is everything one sync run needs.
type Config struct {
	Owner          string
	Repos          []string // empty = all org repos
	CheckpointPath string
	Since          time.Time // zero = use checkpoint watermark (backfill override)
	Token          string
	HTTPClient     *http.Client             // test seam: transport that redirects to httptest
	Sleep          func(time.Duration)      // test seam; nil = real sleep
	Store          knowledge.KnowledgeStore // nil = in-memory dev store
	Tenant         string                   // optional: scopes the store to one tenant (§8)
}

// Count tallies one entity type over a run.
type Count struct {
	New     int
	Updated int
	Skipped int
}

// SyncResult summarizes a successful run.
// Relationships counts relationship writes issued; the store's set semantics
// collapse duplicates (e.g. re-relating repo→org on every run).
type SyncResult struct {
	RunID         string
	Counts        map[string]Count
	Relationships int
	Checkpoint    *checkpoint.Checkpoint
}

// closeRefRe matches GitHub close keywords in PR bodies.
// ponytail: body-only regex misses timeline-linked issues; upgrade path is
// GraphQL ClosingIssuesReferences.
var closeRefRe = regexp.MustCompile(`(?i)\b(?:close|closes|closed|fix|fixes|fixed|resolve|resolves|resolved)\s+#(\d+)\b`)

type syncer struct {
	ctx      context.Context
	client   *Client
	store    knowledge.KnowledgeStore
	runID    string
	counts   map[string]Count
	rels     int
	totalNew int

	// run-local identity dedup. ponytail: cross-source merging (Jira) will
	// persist these claims in AIKOQL in the next slice.
	byLogin map[string]string
	byEmail map[string]string
	byKey   map[string]string

	shaToCommit map[string]string // sha → commit koid (merge-commit lookup)
	issueKoids  map[int]string    // issue number → koid, per repo
}

// upsert counts New/Updated honestly: the store bumps Version only when the
// property payload changed, so the pre-upsert version distinguishes "changed"
// from "re-seen identical" (which counts nothing).
// ponytail: two store round-trips per object; the SDK adapter can batch the
// pre-check once real latency shows up.
func (s *syncer) upsert(ko knowledge.KnowledgeObject) (string, error) {
	prev, prevErr := s.store.GetByExternalID(s.ctx, ko.ExternalID)
	if prevErr != nil && !errors.Is(prevErr, knowledge.ErrNotFound) {
		return "", fmt.Errorf("github: lookup %s %s: %w", ko.TypeName, ko.ExternalID, prevErr)
	}
	res, err := s.store.Upsert(s.ctx, ko)
	if err != nil {
		return "", fmt.Errorf("github: upsert %s %s: %w", ko.TypeName, ko.ExternalID, err)
	}
	c := s.counts[ko.TypeName]
	if prevErr != nil {
		c.New++
		s.totalNew++
	} else if res.Version > prev.Version {
		c.Updated++
	}
	s.counts[ko.TypeName] = c
	return res.Koid, nil
}

func (s *syncer) relate(from, to string, relType ontology.RelType) error {
	err := s.store.Relate(s.ctx, knowledge.Relationship{Type: string(relType), From: from, To: to})
	if err != nil {
		return fmt.Errorf("github: relate %s %s->%s: %w", relType, from, to, err)
	}
	s.rels++
	return nil
}

// engineer upserts the canonical engineer for a resolved source identity,
// reusing an existing engineer when any of login/email/key was seen before.
func (s *syncer) engineer(p identity.Person, prov knowledge.Provenance) (string, error) {
	if p.Key == "" {
		return "", nil // ghost author: no engineer, no edge
	}
	if koid, ok := s.byLogin[strings.ToLower(p.Login)]; p.Login != "" && ok {
		return koid, nil
	}
	if koid, ok := s.byEmail[p.Email]; p.Email != "" && ok {
		return koid, nil
	}
	if koid, ok := s.byKey[p.Key]; ok {
		return koid, nil
	}
	e := ontology.Engineer{Name: p.Name, Email: p.Email, Login: p.Login, IdentityKey: p.Key, IdentityRule: p.Rule}
	ko, err := e.KnowledgeObject(prov)
	if err != nil {
		return "", fmt.Errorf("github: map engineer %s: %w", p.Key, err)
	}
	koid, err := s.upsert(ko)
	if err != nil {
		return "", err
	}
	s.byKey[p.Key] = koid
	if p.Login != "" {
		s.byLogin[strings.ToLower(p.Login)] = koid
	}
	if p.Email != "" {
		s.byEmail[p.Email] = koid
	}
	return koid, nil
}

// Sync runs one incremental GitHub synchronization.
func Sync(ctx context.Context, cfg Config) (*SyncResult, error) {
	runStart := time.Now().UTC()
	runID := runStart.Format("20060102T150405Z")

	ck, err := checkpoint.Load(cfg.CheckpointPath)
	if err != nil {
		return nil, err
	}
	watermark := ck.Github.UpdatedSince
	if !cfg.Since.IsZero() {
		watermark = cfg.Since
	}

	client, err := NewClient(cfg.Token, cfg.HTTPClient)
	if err != nil {
		return nil, err
	}
	if cfg.Sleep != nil {
		client.sleep = cfg.Sleep
	}
	store := cfg.Store
	if store == nil {
		store = knowledge.NewMemory()
	}
	if cfg.Tenant != "" {
		store = knowledge.WithTenant(store, cfg.Tenant)
	}
	s := &syncer{
		ctx:         ctx,
		client:      client,
		store:       store,
		runID:       runID,
		counts:      map[string]Count{},
		byLogin:     map[string]string{},
		byEmail:     map[string]string{},
		byKey:       map[string]string{},
		shaToCommit: map[string]string{},
		issueKoids:  map[int]string{},
	}

	// Organization.
	org, _, err := retry(ctx, client, "orgs.get", func() (*gh.Organization, *gh.Response, error) {
		return client.gh.Organizations.Get(ctx, cfg.Owner)
	})
	if err != nil {
		return nil, fmt.Errorf("github: get organization %s: %w", cfg.Owner, err)
	}
	orgOnt := toOrganization(org)
	orgKO, err := orgOnt.KnowledgeObject(ontology.NewProvenance(orgOnt.HTMLURL, orgOnt.UpdatedAt, runID))
	if err != nil {
		return nil, err
	}
	orgKoid, err := s.upsert(orgKO)
	if err != nil {
		return nil, err
	}

	// Repositories (forks skipped — ponytail: add "all" if forks matter).
	repos, err := paginate(ctx, client, "repos.list", func(page int) ([]*gh.Repository, *gh.Response, error) {
		return client.gh.Repositories.ListByOrg(ctx, cfg.Owner, &gh.RepositoryListByOrgOptions{
			Type:        "sources",
			ListOptions: gh.ListOptions{PerPage: 100, Page: page},
		})
	})
	if err != nil {
		return nil, err
	}
	want := make(map[string]bool, len(cfg.Repos))
	for _, r := range cfg.Repos {
		want[r] = true
	}

	for _, r := range repos {
		name := r.GetName()
		if len(want) > 0 && !want[name] {
			continue
		}
		key := cfg.Owner + "/" + name
		repoOnt := toRepository(r, cfg.Owner)
		repoKO, err := repoOnt.KnowledgeObject(ontology.NewProvenance(repoOnt.HTMLURL, repoOnt.UpdatedAt, runID))
		if err != nil {
			return nil, err
		}
		repoKoid, err := s.upsert(repoKO)
		if err != nil {
			return nil, err
		}
		if err := s.relate(repoKoid, orgKoid, ontology.RelPartOf); err != nil {
			return nil, err
		}

		if err := s.syncCommits(cfg.Owner, name, repoOnt.DefaultBranch, ck); err != nil {
			return nil, fmt.Errorf("github: sync commits %s: %w", key, err)
		}
		if err := s.syncIssues(cfg.Owner, name, watermark); err != nil {
			return nil, fmt.Errorf("github: sync issues %s: %w", key, err)
		}
		if err := s.syncPRs(cfg.Owner, name, repoKoid, watermark); err != nil {
			return nil, fmt.Errorf("github: sync pull requests %s: %w", key, err)
		}
	}

	// Advance the checkpoint only after a fully successful run: a crash
	// re-runs everything since the last committed checkpoint, and idempotent
	// upserts make the overlap harmless (AC-ING-004, AC-REL-001).
	ck.Github.UpdatedSince = runStart
	ck.Runs = append(ck.Runs, checkpoint.RunRecord{
		StartedAt:  runStart,
		EndedAt:    time.Now().UTC(),
		Owner:      cfg.Owner,
		NewObjects: s.totalNew,
	})
	if len(ck.Runs) > 5 {
		ck.Runs = ck.Runs[len(ck.Runs)-5:]
	}
	if err := ck.Save(cfg.CheckpointPath); err != nil {
		return nil, err
	}

	return &SyncResult{RunID: runID, Counts: s.counts, Relationships: s.rels, Checkpoint: ck}, nil
}

// syncCommits fetches default-branch commits unless the head SHA is
// unchanged since the last run, and records commit koids for merge lookup.
// ponytail: a force-push triggers a full refetch instead of bisecting;
// commits are the bulk but force-pushes are rare.
func (s *syncer) syncCommits(owner, repo, branch string, ck *checkpoint.Checkpoint) error {
	key := owner + "/" + repo
	headPrev := ck.Github.Repos[key]

	page := 1
	newHead := headPrev
	for {
		batch, resp, err := retry(s.ctx, s.client, "commits.list", func() ([]*gh.RepositoryCommit, *gh.Response, error) {
			return s.client.gh.Repositories.ListCommits(s.ctx, owner, repo, &gh.CommitsListOptions{
				SHA:         branch,
				ListOptions: gh.ListOptions{PerPage: 100, Page: page},
			})
		})
		if err != nil {
			return err
		}
		if page == 1 {
			if len(batch) > 0 {
				newHead = batch[0].GetSHA()
			}
			if headPrev != "" && newHead == headPrev {
				return nil // head unchanged: nothing new on the branch
			}
			ck.Github.Repos[key] = newHead
		}
		for _, c := range batch {
			commitOnt := toCommit(c, owner, repo)
			ko, err := commitOnt.KnowledgeObject(ontology.NewProvenance(c.GetHTMLURL(), commitOnt.CommittedAt, s.runID))
			if err != nil {
				return err
			}
			commitKoid, err := s.upsert(ko)
			if err != nil {
				return err
			}
			s.shaToCommit[commitOnt.SHA] = commitKoid

			comm := c.GetCommit()
			name, email := "", ""
			if comm != nil {
				name, email = comm.GetAuthor().GetName(), comm.GetAuthor().GetEmail()
			}
			p := identity.Resolve(name, email, c.GetAuthor().GetLogin())
			engKoid, err := s.engineer(p, ontology.NewProvenance(c.GetHTMLURL(), commitOnt.CommittedAt, s.runID))
			if err != nil {
				return err
			}
			if engKoid != "" {
				if err := s.relate(engKoid, commitKoid, ontology.RelAuthored); err != nil {
					return err
				}
			}
		}
		if resp.NextPage == 0 {
			break
		}
		page = resp.NextPage
	}
	return nil
}

func (s *syncer) syncIssues(owner, repo string, watermark time.Time) error {
	s.issueKoids = map[int]string{}
	issues, err := paginate(s.ctx, s.client, "issues.list", func(page int) ([]*gh.Issue, *gh.Response, error) {
		return s.client.gh.Issues.ListByRepo(s.ctx, owner, repo, &gh.IssueListByRepoOptions{
			State:       "all",
			ListOptions: gh.ListOptions{PerPage: 100, Page: page},
		})
	})
	if err != nil {
		return err
	}
	for _, i := range issues {
		if i.IsPullRequest() { // PRs are issues in this API; the PR list owns them
			c := s.counts["Issue"]
			c.Skipped++
			s.counts["Issue"] = c
			continue
		}
		issueOnt := toIssue(i, owner, repo)
		// GitHub dropped `since` from the issues endpoint, so gate
		// client-side like the PRs below.
		if !watermark.IsZero() && issueOnt.UpdatedAt.Before(watermark) {
			c := s.counts["Issue"]
			c.Skipped++
			s.counts["Issue"] = c
			continue
		}
		ko, err := issueOnt.KnowledgeObject(ontology.NewProvenance(i.GetHTMLURL(), issueOnt.UpdatedAt, s.runID))
		if err != nil {
			return err
		}
		koid, err := s.upsert(ko)
		if err != nil {
			return err
		}
		s.issueKoids[issueOnt.Number] = koid
	}
	return nil
}

func (s *syncer) syncPRs(owner, repo, repoKoid string, watermark time.Time) error {
	prs, err := paginate(s.ctx, s.client, "pulls.list", func(page int) ([]*gh.PullRequest, *gh.Response, error) {
		return s.client.gh.PullRequests.List(s.ctx, owner, repo, &gh.PullRequestListOptions{
			State:       "all",
			Sort:        "updated",
			Direction:   "asc",
			ListOptions: gh.ListOptions{PerPage: 100, Page: page},
		})
	})
	if err != nil {
		return err
	}
	for _, p := range prs {
		prOnt := toPullRequest(p, owner, repo)
		if !watermark.IsZero() && prOnt.UpdatedAt.Before(watermark) {
			c := s.counts["PullRequest"]
			c.Skipped++
			s.counts["PullRequest"] = c
			continue
		}

		ko, err := prOnt.KnowledgeObject(ontology.NewProvenance(p.GetHTMLURL(), prOnt.UpdatedAt, s.runID))
		if err != nil {
			return err
		}
		prKoid, err := s.upsert(ko)
		if err != nil {
			return err
		}
		if err := s.relate(prKoid, repoKoid, ontology.RelTargets); err != nil {
			return err
		}

		// Author (beyond the PRD core list — AC-KG-001 requires PR→author traversal).
		if prOnt.AuthorLogin != "" {
			person := identity.Resolve("", "", prOnt.AuthorLogin)
			engKoid, err := s.engineer(person, ontology.NewProvenance(p.GetHTMLURL(), prOnt.UpdatedAt, s.runID))
			if err != nil {
				return err
			}
			if err := s.relate(engKoid, prKoid, ontology.RelAuthored); err != nil {
				return err
			}
		}

		if err := s.syncPRIssues(owner, repo, prKoid, prOnt); err != nil {
			return err
		}
		if err := s.syncReviews(owner, repo, prKoid, prOnt.Number); err != nil {
			return err
		}

		// Merge commit: PART_OF edge when the commit is on the default branch.
		if prOnt.Merged && prOnt.MergeCommitSHA != "" {
			if commitKoid, ok := s.shaToCommit[strings.ToLower(prOnt.MergeCommitSHA)]; ok {
				if err := s.relate(commitKoid, prKoid, ontology.RelPartOf); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// syncPRIssues links a PR to the issues its body references, fetching
// referenced issues on demand (deduped per run) when the incremental
// window excluded them.
func (s *syncer) syncPRIssues(owner, repo, prKoid string, pr ontology.PullRequest) error {
	matches := closeRefRe.FindAllStringSubmatch(pr.Body, -1)
	seen := map[int]bool{}
	for _, m := range matches {
		num, err := strconv.Atoi(m[1])
		if err != nil || seen[num] {
			continue
		}
		seen[num] = true

		koid, ok := s.issueKoids[num]
		if !ok {
			i, _, err := retry(s.ctx, s.client, "issues.get", func() (*gh.Issue, *gh.Response, error) {
				return s.client.gh.Issues.Get(s.ctx, owner, repo, num)
			})
			if err != nil {
				return fmt.Errorf("github: refetch issue %s/%s#%d: %w", owner, repo, num, err)
			}
			issueOnt := toIssue(i, owner, repo)
			ko, err := issueOnt.KnowledgeObject(ontology.NewProvenance(i.GetHTMLURL(), issueOnt.UpdatedAt, s.runID))
			if err != nil {
				return err
			}
			koid, err = s.upsert(ko)
			if err != nil {
				return err
			}
			s.issueKoids[num] = koid
		}
		if err := s.relate(prKoid, koid, ontology.RelImplements); err != nil {
			return err
		}
	}
	return nil
}

func (s *syncer) syncReviews(owner, repo string, prKoid string, prNumber int) error {
	reviews, err := paginate(s.ctx, s.client, "pulls.reviews", func(page int) ([]*gh.PullRequestReview, *gh.Response, error) {
		return s.client.gh.PullRequests.ListReviews(s.ctx, owner, repo, prNumber, &gh.ListOptions{PerPage: 100, Page: page})
	})
	if err != nil {
		return err
	}
	for _, r := range reviews {
		reviewOnt := toReview(r, owner, repo, prNumber)
		ko, err := reviewOnt.KnowledgeObject(ontology.NewProvenance(r.GetHTMLURL(), reviewOnt.SubmittedAt, s.runID))
		if err != nil {
			return err
		}
		reviewKoid, err := s.upsert(ko)
		if err != nil {
			return err
		}
		if err := s.relate(reviewKoid, prKoid, ontology.RelPartOf); err != nil {
			return err
		}
		if reviewOnt.ReviewerLogin != "" {
			p := identity.Resolve("", "", reviewOnt.ReviewerLogin)
			engKoid, err := s.engineer(p, ontology.NewProvenance(r.GetHTMLURL(), reviewOnt.SubmittedAt, s.runID))
			if err != nil {
				return err
			}
			if err := s.relate(prKoid, engKoid, ontology.RelReviewedBy); err != nil {
				return err
			}
		}
	}
	return nil
}
