package jira

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/checkpoint"
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
	RunID      string
	Counts     map[string]Count
	Checkpoint *checkpoint.Checkpoint
}

type syncer struct {
	ctx      context.Context
	client   *Client
	store    knowledge.KnowledgeStore
	run      *ingestion.Run
	counts   map[string]Count
	totalNew int
}

// upsert counts New/Updated honestly via the pre-upsert version, mirroring
// the github connector: the store bumps Version only when the property
// payload changed, so an identical re-seen issue counts nothing.
func (s *syncer) upsert(ko knowledge.KnowledgeObject) error {
	prev, prevErr := s.store.GetByExternalID(s.ctx, ko.ExternalID)
	if prevErr != nil && !errors.Is(prevErr, knowledge.ErrNotFound) {
		return fmt.Errorf("jira: lookup %s %s: %w", ko.TypeName, ko.ExternalID, prevErr)
	}
	res, err := s.run.Apply(s.ctx, s.store, ingestion.Mutation{Objects: []knowledge.KnowledgeObject{ko}})
	if err != nil {
		return fmt.Errorf("jira: upsert %s %s: %w", ko.TypeName, ko.ExternalID, err)
	}
	c := s.counts[ko.TypeName]
	if prevErr != nil {
		c.New++
		s.totalNew++
	} else if res.Objects[0].Version > prev.Version {
		c.Updated++
	}
	s.counts[ko.TypeName] = c
	return nil
}

// ingest maps one wire issue to the canonical ontology and upserts it (§14:
// Jira data normalizes into the same canonical concepts, never the reverse).
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
	return s.upsert(ko)
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
		ctx:    ctx,
		client: client,
		store:  store,
		run:    run,
		counts: map[string]Count{},
	}

	jql := fmt.Sprintf("project = %q", cfg.Project)
	if !watermark.IsZero() {
		// Minute truncation re-fetches a partial minute of overlap — the
		// upsert identity check keeps those counts at zero.
		jql += fmt.Sprintf(" AND updated >= %q", watermark.UTC().Format("2006/01/02 15:04"))
	}
	jql += " ORDER BY updated ASC"

	for startAt := 0; ; {
		page, err := client.search(ctx, jql, startAt)
		if err != nil {
			return nil, err
		}
		for _, w := range page.Issues {
			if err := s.ingest(w); err != nil {
				return nil, err
			}
		}
		next := startAt + len(page.Issues)
		if len(page.Issues) == 0 || next >= page.Total {
			break
		}
		startAt = next
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

	return &SyncResult{RunID: run.ID, Counts: s.counts, Checkpoint: ck}, nil
}
