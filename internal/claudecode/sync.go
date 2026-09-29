// sync.go ingests a Claude Code transcript directory into the knowledge
// store: the §38 MVP's "one AI-development telemetry source". Each
// transcript becomes a CodingSession with its interactions, agent runs,
// tasks, and code contributions; contributions link to the PR the session
// created (the tool output that recorded the PR URL) via AI_CONTRIBUTES.
// The connector records gaps, never fills them: a contribution whose PR is
// not in the store stays unlinked — run the GitHub sync first.
package claudecode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/ingestion"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// Config is everything one ingest run needs.
type Config struct {
	Dir   string                   // transcript directory
	Store knowledge.KnowledgeStore // nil = in-memory dev store
	// Tenant optionally scopes the store to one tenant (§8).
	Tenant string
	// RepoOf is a test seam; nil = read <cwd>/.git/config.
	RepoOf func(cwd string) string
}

// Count tallies one entity type over a run.
type Count struct {
	New     int
	Updated int
	Skipped int
}

// SyncResult summarizes a successful run.
type SyncResult struct {
	RunID         string
	Counts        map[string]Count
	Relationships int
	Sessions      int
	Unlinked      int // contributions whose PR was absent from the store (no edge)
	Unattributed  int // code edits whose repository could not be derived (no object)
	Unparsed      int // transcript lines that were not valid JSON
}

// Sync ingests every *.jsonl transcript under Dir.
func Sync(ctx context.Context, cfg Config) (*SyncResult, error) {
	store := cfg.Store
	if store == nil {
		store = knowledge.NewMemory()
	}
	if cfg.Tenant != "" {
		store = knowledge.WithTenant(store, cfg.Tenant)
	}
	repo := cfg.RepoOf
	if repo == nil {
		repo = defaultRepo
	}
	run := ingestion.NewRun()
	res := &SyncResult{RunID: run.ID, Counts: map[string]Count{}}

	entries, err := os.ReadDir(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("claudecode: read dir %s: %w", cfg.Dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".jsonl" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("claudecode: %w", err)
		}
		if err := ingestFile(ctx, run, store, filepath.Join(cfg.Dir, name), repo, res); err != nil {
			return nil, fmt.Errorf("claudecode: %s: %w", name, err)
		}
	}
	return res, nil
}

func ingestFile(ctx context.Context, run *ingestion.Run, store knowledge.KnowledgeStore, path string, repo repoOf, res *SyncResult) error {
	p, err := parseFile(path, repo)
	if err != nil {
		return err
	}
	res.Unattributed += p.unattributed
	res.Unparsed += p.unparsed
	if p.session.SessionID == "" || p.session.StartedAt.IsZero() {
		return nil // nothing provable in this file
	}
	res.Sessions++

	prov := ontology.NewSourceProvenance(source, path, fileMTime(path))
	type mapped interface {
		KnowledgeObject(knowledge.Provenance) (knowledge.KnowledgeObject, error)
	}
	objs := []mapped{p.session}
	for _, v := range p.interactions {
		objs = append(objs, v)
	}
	for _, v := range p.runs {
		objs = append(objs, v)
	}
	for _, v := range p.tasks {
		objs = append(objs, v)
	}
	for _, v := range p.contributions {
		objs = append(objs, v)
	}

	koids := make([]string, len(objs))
	for i, v := range objs {
		ko, err := v.KnowledgeObject(prov)
		if err != nil {
			return fmt.Errorf("claudecode: map %T: %w", v, err)
		}
		koid, err := upsert(ctx, run, store, ko, res)
		if err != nil {
			return err
		}
		koids[i] = koid
	}

	// Task koids by tool_use id: the AgentTask → CodeContribution link
	// (AUTHORED). The contribution's id embeds the producing task's tool_use
	// id (sessionID:tool_use — both carry no colons), so the join needs no
	// extra parsing state.
	taskKoids := make(map[string]string, len(p.tasks))
	for i, tk := range p.tasks {
		taskKoids[tk.ID] = koids[1+len(p.interactions)+len(p.runs)+i]
	}

	// Contributions relate to the PR the session created. The PR must exist
	// in the store — the telemetry only proves the PR URL, so a missing PR
	// leaves the contribution unlinked, reported as such. The task link
	// above does not depend on the PR: the task produced the code either way.
	for i, c := range p.contributions {
		objIdx := 1 + len(p.interactions) + len(p.runs) + len(p.tasks) + i
		if taskKoid, ok := taskKoids[taskID(c.ID)]; ok {
			if _, err := run.Apply(ctx, store, ingestion.Mutation{Relationships: []knowledge.Relationship{{
				Type: string(ontology.RelAuthored),
				From: taskKoid,
				To:   koids[objIdx],
			}}}); err != nil {
				return fmt.Errorf("claudecode: relate task contribution: %w", err)
			}
			res.Relationships++
		}
		if c.PRNumber == 0 {
			res.Unlinked++
			continue
		}
		prKO, err := store.GetByExternalID(ctx, prExternalID(c.Repository, c.PRNumber))
		if errors.Is(err, knowledge.ErrNotFound) {
			res.Unlinked++
			continue
		}
		if err != nil {
			return fmt.Errorf("claudecode: lookup PR %s#%d: %w", c.Repository, c.PRNumber, err)
		}
		if _, err := run.Apply(ctx, store, ingestion.Mutation{Relationships: []knowledge.Relationship{{
			Type: string(ontology.RelAIContributes),
			From: koids[objIdx],
			To:   prKO.Koid,
		}}}); err != nil {
			return fmt.Errorf("claudecode: relate contribution: %w", err)
		}
		res.Relationships++
	}
	return nil
}

// taskID extracts the producing task's tool_use id from a contribution id
// (sessionID:tool_use). Empty when the id carries no separator — no edge.
func taskID(contribID string) string {
	if i := strings.LastIndex(contribID, ":"); i >= 0 {
		return contribID[i+1:]
	}
	return ""
}

func prExternalID(ownerRepo string, num int) string {
	return fmt.Sprintf("github.com:pr:%s#%d", ownerRepo, num)
}

// upsert counts New/Updated honestly (the github pattern): the store bumps
// Version only when the payload changed, so the pre-check distinguishes
// "changed" from "re-seen identical" (which counts nothing).
func upsert(ctx context.Context, run *ingestion.Run, store knowledge.KnowledgeStore, ko knowledge.KnowledgeObject, res *SyncResult) (string, error) {
	prev, prevErr := store.GetByExternalID(ctx, ko.ExternalID)
	if prevErr != nil && !errors.Is(prevErr, knowledge.ErrNotFound) {
		return "", fmt.Errorf("claudecode: lookup %s %s: %w", ko.TypeName, ko.ExternalID, prevErr)
	}
	applied, err := run.Apply(ctx, store, ingestion.Mutation{Objects: []knowledge.KnowledgeObject{ko}})
	if err != nil {
		return "", fmt.Errorf("claudecode: upsert %s %s: %w", ko.TypeName, ko.ExternalID, err)
	}
	stored := applied.Objects[0]
	c := res.Counts[ko.TypeName]
	if prevErr != nil {
		c.New++
	} else if stored.Version > prev.Version {
		c.Updated++
	}
	res.Counts[ko.TypeName] = c
	return stored.Koid, nil
}

// fileMTime is the transcript's source-side update time; a stat failure
// leaves it zero — provenance is stamped, not guessed.
func fileMTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}
