package ontology

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

// RelType and the relationship vocabulary live in rels.go (§12).

const connectorVersion = "github-connector/v0"

// External-ID builders. Prefixes are disjoint across types, and the
// delimiters cannot appear in GitHub logins or repo names, so the scheme
// is collision-free without escaping.
func OrgExternalID(login string) string  { return "github.com:org:" + login }
func UserExternalID(login string) string { return "github.com:user:" + login }

// AccountExternalID names a personal GitHub account; the account: prefix is
// disjoint from the engineer-keyed user: scheme, so a login can be both an
// engineer and an account owner without colliding.
func AccountExternalID(login string) string       { return "github.com:account:" + login }
func UserEmailExternalID(normEmail string) string { return "github.com:email:" + normEmail }
func RepoExternalID(owner, name string) string {
	return fmt.Sprintf("github.com:repo:%s/%s", owner, name)
}
func IssueExternalID(owner, repo string, num int) string {
	return fmt.Sprintf("github.com:issue:%s/%s#%d", owner, repo, num)
}
func CommitExternalID(owner, repo, sha string) string {
	return fmt.Sprintf("github.com:commit:%s/%s@%s", owner, repo, strings.ToLower(sha))
}
func PRExternalID(owner, repo string, num int) string {
	return fmt.Sprintf("github.com:pr:%s/%s#%d", owner, repo, num)
}
func ReviewExternalID(owner, repo string, pr int, id int64) string {
	return fmt.Sprintf("github.com:review:%s/%s#%d@%d", owner, repo, pr, id)
}
func BuildExternalID(owner, repo string, id int64) string {
	return fmt.Sprintf("github.com:build:%s/%s:%d", owner, repo, id)
}
func DeploymentExternalID(owner, repo string, id int64) string {
	return fmt.Sprintf("github.com:deployment:%s/%s:%d", owner, repo, id)
}

// ServiceExternalID names the repo-scoped environment. GitHub environment
// names are [A-Za-z0-9_-]+, so the delimiter scheme stays collision-free.
func ServiceExternalID(owner, repo, env string) string {
	return fmt.Sprintf("github.com:service:%s/%s:%s", owner, repo, env)
}

// NewProvenance carries the source-side facts every knowledge object keeps
// (AC-KG-004). Run-scoped fields (ObservedAt, IngestionRun) are the
// ingestion layer's job (§10) and are stamped on Apply, not here.
func NewProvenance(sourceURL string, sourceUpdatedAt time.Time) knowledge.Provenance {
	return knowledge.Provenance{
		Source:           "github",
		SourceURL:        sourceURL,
		SourceUpdatedAt:  sourceUpdatedAt.UTC(),
		ConnectorVersion: connectorVersion,
	}
}

// toKnowledgeObject converts a typed entity to a generic knowledge object:
// marshal through the snake_case json tags, inject external_id. Sources send
// UTC timestamps, so the resulting RFC3339 strings compare equal on re-runs.
func toKnowledgeObject(typeName, externalID string, v any, prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return knowledge.KnowledgeObject{}, fmt.Errorf("ontology: marshal %s: %w", typeName, err)
	}
	var props map[string]any
	if err := json.Unmarshal(b, &props); err != nil {
		return knowledge.KnowledgeObject{}, fmt.Errorf("ontology: unmarshal %s: %w", typeName, err)
	}
	props["external_id"] = externalID
	return knowledge.KnowledgeObject{
		TypeName:   typeName,
		ExternalID: externalID,
		Properties: props,
		Provenance: prov,
	}, nil
}

// KnowledgeObjectWithID maps a typed entity with an explicit external ID —
// the identity layer needs it for engineers whose creator's ID scheme differs
// from the per-type default (e.g. a Jira-created Engineer).
func KnowledgeObjectWithID(typeName, externalID string, v any, prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject(typeName, externalID, v, prov)
}

func (o Organization) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("Organization", OrgExternalID(o.Login), o, prov)
}

func (u User) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("User", AccountExternalID(u.Login), u, prov)
}

// Engineer picks the external ID by identity rule: email-based engineers are
// keyed by normalized email, everything else by login.
func (e Engineer) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	extID := UserExternalID(e.Login)
	if e.IdentityRule == "email" {
		extID = UserEmailExternalID(e.Email)
	}
	return toKnowledgeObject("Engineer", extID, e, prov)
}

func (r Repository) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("Repository", RepoExternalID(r.Owner, r.Name), r, prov)
}

func (i Issue) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	owner, repo, _ := strings.Cut(i.Repository, "/")
	return toKnowledgeObject("Issue", IssueExternalID(owner, repo, i.Number), i, prov)
}

func (c Commit) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	owner, repo, _ := strings.Cut(c.Repository, "/")
	return toKnowledgeObject("Commit", CommitExternalID(owner, repo, c.SHA), c, prov)
}

func (p PullRequest) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	owner, repo, _ := strings.Cut(p.Repository, "/")
	return toKnowledgeObject("PullRequest", PRExternalID(owner, repo, p.Number), p, prov)
}

func (r Review) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	owner, repo, _ := strings.Cut(r.Repository, "/")
	return toKnowledgeObject("Review", ReviewExternalID(owner, repo, r.PRNumber, r.ID), r, prov)
}

func (b Build) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	owner, repo, _ := strings.Cut(b.Repository, "/")
	return toKnowledgeObject("Build", BuildExternalID(owner, repo, b.ID), b, prov)
}

func (d Deployment) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	owner, repo, _ := strings.Cut(d.Repository, "/")
	return toKnowledgeObject("Deployment", DeploymentExternalID(owner, repo, d.ID), d, prov)
}

func (svc Service) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	owner, repo, _ := strings.Cut(svc.Repository, "/")
	return toKnowledgeObject("Service", ServiceExternalID(owner, repo, svc.Name), svc, prov)
}
