package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/checkpoint"
	"github.com/anckursingh/engineering-intelligence/internal/identity"
	"github.com/anckursingh/engineering-intelligence/internal/ingestion"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// Config is everything one sync run needs.
type Config struct {
	BaseURL        string
	Email          string
	Token          string
	Project        string
	CheckpointPath string
	Since          time.Time                // zero = use checkpoint watermark (backfill override)
	HTTPClient     *http.Client             // test seam; nil = default client
	Store          knowledge.KnowledgeStore // nil = in-memory dev store
	Tenant         string                   // optional: scopes the store to one tenant (§8)
}

// Count tallies one entity type over a run.
type Count struct {
	New     int
	Updated int
}

// SyncResult summarizes a successful run.
type SyncResult struct {
	RunID         string
	Counts        map[string]Count
	Relationships int
	Checkpoint    *checkpoint.Checkpoint
}

type syncer struct {
	ctx           context.Context
	client        *Client
	projectScope  string
	store         knowledge.KnowledgeStore
	run           *ingestion.Run
	resolver      *identity.Resolver // persistent identity claims (§9, §15)
	counts        map[string]Count
	totalNew      int
	relationships int
	parents       []parentLink
}

type parentLink struct{ childKey, parentKey string }

// upsert counts New/Updated honestly via the pre-upsert version, mirroring
// the github connector: the store bumps Version only when the property
// payload changed, so an identical re-seen issue counts nothing.
func (s *syncer) upsert(ko knowledge.KnowledgeObject) (knowledge.KnowledgeObject, error) {
	prev, prevErr := s.store.GetByExternalID(s.ctx, ko.ExternalID)
	if prevErr != nil && !errors.Is(prevErr, knowledge.ErrNotFound) {
		return knowledge.KnowledgeObject{}, fmt.Errorf("jira: lookup %s %s: %w", ko.TypeName, ko.ExternalID, prevErr)
	}
	res, err := s.run.Apply(s.ctx, s.store, ingestion.Mutation{Objects: []knowledge.KnowledgeObject{ko}})
	if err != nil {
		return knowledge.KnowledgeObject{}, fmt.Errorf("jira: upsert %s %s: %w", ko.TypeName, ko.ExternalID, err)
	}
	c := s.counts[ko.TypeName]
	if prevErr != nil {
		c.New++
		s.totalNew++
	} else if res.Objects[0].Version > prev.Version {
		c.Updated++
	}
	s.counts[ko.TypeName] = c
	return res.Objects[0], nil
}

// ingest maps one wire issue to the canonical ontology and upserts it (§14:
// Jira data normalizes into the same canonical concepts, never the reverse).
// Reporter and assignee resolve through persistent identity claims; the
// Jira parent and assignee fields provide explicit evidence for graph edges.
func (s *syncer) ingest(w wireIssue) error {
	updated, err := parseTime(w.Fields.Updated)
	if err != nil {
		return err
	}
	created, err := parseTime(w.Fields.Created)
	if err != nil {
		return err
	}
	key := strings.ToUpper(w.Key)
	issue := ontology.JiraIssue{
		Site:      s.client.Site(),
		Key:       key,
		Summary:   w.Fields.Summary,
		Status:    w.Fields.Status.Name,
		IssueType: w.Fields.IssueType.Name,
		Project:   strings.ToUpper(strings.SplitN(key, "-", 2)[0]),
		CreatedAt: created,
		UpdatedAt: updated,
	}
	ko, err := issue.KnowledgeObject(ontology.NewJiraProvenance(s.client.Site()+"/browse/"+key, updated))
	if err != nil {
		return err
	}
	projectObject, err := (ontology.JiraProject{
		Site: s.client.Site(), ID: w.Fields.Project.ID, Key: issue.Project, Name: w.Fields.Project.Name,
	}).KnowledgeObject(ontology.NewJiraProvenance(s.client.Site()+"/browse/"+key, updated))
	if err != nil {
		return err
	}
	storedProject, err := s.upsert(projectObject)
	if err != nil {
		return err
	}
	storedIssue, err := s.upsert(ko)
	if err != nil {
		return err
	}
	if err := s.store.Relate(s.ctx, knowledge.Relationship{Type: string(ontology.RelContainsIssue), From: storedProject.Koid, To: storedIssue.Koid}); err != nil {
		return fmt.Errorf("jira: relate project to issue %s: %w", key, err)
	}
	s.relationships++
	if !strings.EqualFold(s.projectScope, issue.Project) {
		scopeObject, err := (ontology.JiraProject{
			Site: s.client.Site(), ID: w.Fields.Project.ID, Key: s.projectScope, Name: w.Fields.Project.Name,
		}).KnowledgeObject(ontology.NewJiraProvenance(s.client.Site()+"/browse/"+key, updated))
		if err != nil {
			return err
		}
		scopedProject, err := s.upsert(scopeObject)
		if err != nil {
			return err
		}
		if err := s.store.Relate(s.ctx, knowledge.Relationship{Type: string(ontology.RelContainsIssue), From: scopedProject.Koid, To: storedIssue.Koid}); err != nil {
			return fmt.Errorf("jira: relate configured project scope to issue %s: %w", key, err)
		}
		s.relationships++
	}
	if w.Fields.Parent != nil && w.Fields.Parent.Key != "" {
		s.parents = append(s.parents, parentLink{childKey: key, parentKey: strings.ToUpper(w.Fields.Parent.Key)})
	}
	prov := ontology.NewJiraProvenance(s.client.Site()+"/browse/"+key, updated)
	if err := s.linkSprints(storedIssue, issue.Project, w.Fields.Custom, prov); err != nil {
		return err
	}
	for _, u := range []wireUser{w.Fields.Reporter} {
		p := jiraPerson(s.client.Site(), u)
		if p.Key == "" {
			continue
		}
		if _, _, err := s.resolver.Resolve(s.ctx, p, prov); err != nil {
			return fmt.Errorf("jira: resolve identity for %s: %w", key, err)
		}
	}
	if u := w.Fields.Assignee; jiraPerson(s.client.Site(), u).Key != "" {
		p := jiraPerson(s.client.Site(), u)
		engineer, _, err := s.resolver.Resolve(s.ctx, p, prov)
		if err != nil {
			return fmt.Errorf("jira: resolve assignee for %s: %w", key, err)
		}
		if err := s.store.Relate(s.ctx, knowledge.Relationship{Type: string(ontology.RelAssignedTo), From: storedIssue.Koid, To: engineer}); err != nil {
			return fmt.Errorf("jira: relate assignee for %s: %w", key, err)
		}
		s.relationships++
	}
	return nil
}

func (s *syncer) linkSprints(issue knowledge.KnowledgeObject, project string, fields map[string]json.RawMessage, prov knowledge.Provenance) error {
	fieldID, err := s.client.sprintField(s.ctx)
	if err != nil {
		return err
	}
	if fieldID == "" || len(fields[fieldID]) == 0 || string(fields[fieldID]) == "null" {
		return nil
	}
	var sprints []wireSprint
	if err := json.Unmarshal(fields[fieldID], &sprints); err != nil {
		return fmt.Errorf("jira: decode sprint membership for %s: %w", issue.Properties["key"], err)
	}
	for _, sprint := range sprints {
		if sprint.ID == 0 {
			continue
		}
		if err := s.linkSprint(issue, project, sprint, prov); err != nil {
			return err
		}
	}
	return nil
}

func (s *syncer) linkSprint(issue knowledge.KnowledgeObject, project string, source wireSprint, prov knowledge.Provenance) error {
	sprint, err := (ontology.JiraSprint{
		Site: s.client.Site(), ID: source.ID, Name: source.Name, State: source.State,
		Project: project, BoardID: source.BoardID, StartDate: source.StartDate, EndDate: source.EndDate,
	}).KnowledgeObject(prov)
	if err != nil {
		return fmt.Errorf("jira: map sprint %d: %w", source.ID, err)
	}
	stored, err := s.upsert(sprint)
	if err != nil {
		return err
	}
	if err := s.store.Relate(s.ctx, knowledge.Relationship{Type: string(ontology.RelInSprint), From: issue.Koid, To: stored.Koid}); err != nil {
		return fmt.Errorf("jira: relate issue to sprint %d: %w", source.ID, err)
	}
	s.relationships++
	return nil
}

func jiraPerson(site string, u wireUser) identity.Person {
	if u.EmailAddress != "" {
		return identity.Resolve(u.DisplayName, u.EmailAddress, "")
	}
	if u.AccountID == "" {
		return identity.Resolve(u.DisplayName, "", "")
	}
	return identity.Resolve(u.DisplayName, "", "jira:"+site+":"+u.AccountID)
}

func (s *syncer) relateParents() error {
	for _, link := range s.parents {
		child, err := s.store.GetByExternalID(s.ctx, ontology.JiraIssueExternalID(s.client.Site(), link.childKey))
		if err != nil {
			return fmt.Errorf("jira: lookup child issue %s: %w", link.childKey, err)
		}
		parent, err := s.store.GetByExternalID(s.ctx, ontology.JiraIssueExternalID(s.client.Site(), link.parentKey))
		if errors.Is(err, knowledge.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("jira: lookup parent issue %s: %w", link.parentKey, err)
		}
		if err := s.store.Relate(s.ctx, knowledge.Relationship{Type: string(ontology.RelChildOf), From: child.Koid, To: parent.Koid}); err != nil {
			return fmt.Errorf("jira: relate parent %s -> %s: %w", link.childKey, link.parentKey, err)
		}
		s.relationships++
	}
	return nil
}

// Sync runs one incremental Jira synchronization. The JQL is the incremental
// boundary: the server filters by updated >= watermark, and the watermark
// advances only after a fully successful run (AC-ING-004) — a crash re-runs
// the overlap, and idempotent upserts make that harmless.
func Sync(ctx context.Context, cfg Config) (*SyncResult, error) {
	run := ingestion.NewRun()

	ck, err := checkpoint.Load(cfg.CheckpointPath)
	if err != nil {
		return nil, err
	}
	watermark := ck.Jira.UpdatedSince
	if !cfg.Since.IsZero() {
		watermark = cfg.Since
	}

	client, err := NewClient(cfg.BaseURL, cfg.Email, cfg.Token, cfg.HTTPClient)
	if err != nil {
		return nil, err
	}
	store := cfg.Store
	if store == nil {
		store = knowledge.NewMemory()
	}
	if cfg.Tenant != "" {
		store = knowledge.WithTenant(store, cfg.Tenant)
	}
	s := &syncer{
		ctx:          ctx,
		client:       client,
		projectScope: cfg.Project,
		store:        store,
		run:          run,
		resolver:     identity.NewResolver(store, run, "jira", client.Site()),
		counts:       map[string]Count{},
	}

	jql := fmt.Sprintf("project = %q", cfg.Project)
	if !watermark.IsZero() {
		// Minute truncation re-fetches a partial minute of overlap — the
		// upsert identity check keeps those counts at zero.
		jql += fmt.Sprintf(" AND updated >= %q", watermark.UTC().Format("2006/01/02 15:04"))
	}
	jql += " ORDER BY updated ASC"

	seenPageTokens := map[string]struct{}{}
	for nextPageToken := ""; ; {
		page, err := client.search(ctx, jql, nextPageToken)
		if err != nil {
			return nil, err
		}
		for _, w := range page.Issues {
			if err := s.ingest(w); err != nil {
				return nil, err
			}
		}
		if page.IsLast || page.NextPageToken == "" {
			break
		}
		if _, seen := seenPageTokens[page.NextPageToken]; seen {
			return nil, fmt.Errorf("jira: search repeated next page token %q", page.NextPageToken)
		}
		seenPageTokens[page.NextPageToken] = struct{}{}
		nextPageToken = page.NextPageToken
	}
	if err := s.relateParents(); err != nil {
		return nil, err
	}

	ck.Jira.UpdatedSince = run.StartedAt
	ck.Runs = append(ck.Runs, checkpoint.RunRecord{
		StartedAt:  run.StartedAt,
		EndedAt:    time.Now().UTC(),
		Project:    cfg.Project,
		NewObjects: s.totalNew,
	})
	if len(ck.Runs) > 5 {
		ck.Runs = ck.Runs[len(ck.Runs)-5:]
	}
	if err := ck.Save(cfg.CheckpointPath); err != nil {
		return nil, err
	}

	return &SyncResult{RunID: run.ID, Counts: s.counts, Relationships: s.relationships, Checkpoint: ck}, nil
}
