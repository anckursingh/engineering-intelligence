package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/ingestion"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// Persistent identity resolution (§9, §15): every automatic identity decision
// is recorded as a SourceIdentity knowledge object with a RESOLVES_TO edge to
// the canonical Engineer. Resolution never merges silently — the only merge
// condition is an exact canonical-key match ("email:x" or "login:x"); anything
// weaker stays two engineers, and every claim carries rule + confidence +
// timestamps so a human can review it (AC-ID-001/002/003).

// Confidence by rule: an email is weaker evidence than a linked identity.
const (
	ConfLogin   = 1.0
	ConfNoreply = 1.0 // the noreply address encodes the login
	ConfEmail   = 0.7
)

// SourceIdentity is the persistent resolution record (§9): who claimed what,
// under which rule, when — and, via RESOLVES_TO and canonical_engineer, which
// canonical engineer it landed on.
type SourceIdentity struct {
	Source            string    `json:"source"`
	SourceIdentity    string    `json:"source_identity"`
	MatchingRule      string    `json:"matching_rule"`
	Confidence        float64   `json:"confidence"`
	CreatedAt         time.Time `json:"created_at"`
	ResolvedAt        time.Time `json:"resolved_at"`
	CanonicalEngineer string    `json:"canonical_engineer"` // koid, denormalized for O(1) reuse reads
}

// ClaimExternalID keys a claim by source + canonical key. Keys contain no
// delimiters beyond ":" and "@", so the scheme is collision-free.
func ClaimExternalID(source, key string) string {
	return fmt.Sprintf("ei.com:identity:%s:%s", source, key)
}

// Resolver resolves source identities to canonical engineers through the
// store. One per connector run (store is the tenant-scoped store; site is the
// Jira site host for jira-created engineer IDs, "" for github).
type Resolver struct {
	store  knowledge.KnowledgeStore
	run    *ingestion.Run
	source string
	site   string
	// seen caches canonical-key → engineer koid for this run, so a second
	// sighting of the same identity in one sync costs zero store calls
	// (a key determines its claim exactly; the live acceptance leg's call
	// budget depends on this). Single-goroutine use — no lock.
	seen map[string]string
}

func NewResolver(store knowledge.KnowledgeStore, run *ingestion.Run, source, site string) *Resolver {
	return &Resolver{store: store, run: run, source: source, site: site, seen: map[string]string{}}
}

// Resolve returns the canonical engineer koid for a resolved source identity
// ("" for unusable identities — ghosts get no engineer). It creates the
// engineer and the claim on first sight and reuses them afterwards; a claim
// for the same canonical key from another source resolves to the SAME
// engineer (cross-source reuse). createdEngineer reports a new canonical
// engineer (for connector counters).
func (r *Resolver) Resolve(ctx context.Context, p Person, prov knowledge.Provenance) (string, bool, error) {
	if p.Key == "" {
		return "", false, nil
	}
	if koid, ok := r.seen[p.Key]; ok {
		return koid, false, nil
	}
	claimID := ClaimExternalID(r.source, p.Key)

	// Existing claim: reuse, and reuse its resolution exactly.
	koid := ""
	if claim, err := r.store.GetByExternalID(ctx, claimID); err == nil {
		k, err := canonicalFrom(claim, claimID)
		if err != nil {
			return "", false, err
		}
		r.seen[p.Key] = k
		return k, false, nil
	} else if !errors.Is(err, knowledge.ErrNotFound) {
		return "", false, fmt.Errorf("identity: lookup claim %s: %w", claimID, err)
	}

	// No claim for this source: probe the other source's claim for the same
	// canonical key and reuse its engineer if one exists (§15). ponytail:
	// probing is email-only — login and noreply keys are github-only by
	// construction, and jira claims are always email keys.
	if p.Rule == "email" {
		for _, src := range []string{"github", "jira"} {
			if src == r.source {
				continue
			}
			other := ClaimExternalID(src, p.Key)
			if oc, err := r.store.GetByExternalID(ctx, other); err == nil {
				k, err := canonicalFrom(oc, other)
				if err != nil {
					return "", false, err
				}
				koid = k
				break
			} else if !errors.Is(err, knowledge.ErrNotFound) {
				return "", false, fmt.Errorf("identity: lookup claim %s: %w", other, err)
			}
		}
	}

	// First sight anywhere: create the canonical engineer under the creator's
	// ID scheme. ponytail: created is derived structurally instead of by a
	// pre-lookup — a crash between the engineer write and the claim write
	// makes the next run count the engineer as New again (a miscount, not a
	// duplicate: the upsert is idempotent).
	created := koid == ""
	if koid == "" {
		extID := ontology.UserExternalID(p.Login) // "" for email-only: picked below
		switch {
		case r.source == "jira":
			extID = ontology.JiraUserExternalID(r.site, p.Email)
		case p.Rule == "email":
			extID = ontology.UserEmailExternalID(p.Email)
		}
		ko, err := ontology.KnowledgeObjectWithID("Engineer", extID, ontology.Engineer{
			Name: p.Name, Email: p.Email, Login: p.Login,
			IdentityKey: p.Key, IdentityRule: p.Rule,
		}, prov)
		if err != nil {
			return "", false, err
		}
		res, err := r.apply(ctx, ingestion.Mutation{Objects: []knowledge.KnowledgeObject{ko}})
		if err != nil {
			return "", false, err
		}
		koid = res.Objects[0].Koid
	}

	// Record the claim and its resolution — the reviewable decision (AC-ID-003).
	now := time.Now().UTC()
	claim, err := ontology.KnowledgeObjectWithID("SourceIdentity", claimID, SourceIdentity{
		Source:            r.source,
		SourceIdentity:    p.Key,
		MatchingRule:      p.Rule,
		Confidence:        confidence(p.Rule),
		CreatedAt:         now,
		ResolvedAt:        now,
		CanonicalEngineer: koid,
	}, prov)
	if err != nil {
		return "", false, err
	}
	res, err := r.apply(ctx, ingestion.Mutation{Objects: []knowledge.KnowledgeObject{claim}})
	if err != nil {
		return "", false, err
	}
	claimKoid := res.Objects[0].Koid
	if _, err := r.apply(ctx, ingestion.Mutation{Relationships: []knowledge.Relationship{{
		Type: string(ontology.RelResolvesTo), From: claimKoid, To: koid,
	}}}); err != nil {
		return "", false, err
	}
	r.seen[p.Key] = koid
	return koid, created, nil
}

// canonicalFrom reads a claim's denormalized engineer koid. Denormalized
// because reuse is the hot path: the RESOLVES_TO edge stays the graph truth
// for traversals, but a reuse read must not pay a traverse round-trip.
func canonicalFrom(claim knowledge.KnowledgeObject, claimID string) (string, error) {
	koid, ok := claim.Properties["canonical_engineer"].(string)
	if !ok || koid == "" {
		return "", fmt.Errorf("identity: claim %s missing canonical_engineer", claimID)
	}
	return koid, nil
}

func (r *Resolver) apply(ctx context.Context, m ingestion.Mutation) (ingestion.Result, error) {
	res, err := r.run.Apply(ctx, r.store, m)
	if err != nil {
		return res, fmt.Errorf("identity: apply: %w", err)
	}
	return res, nil
}

func confidence(rule string) float64 {
	switch rule {
	case "login", "github_noreply":
		return ConfLogin
	case "email":
		return ConfEmail
	}
	return 0
}
