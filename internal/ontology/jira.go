package ontology

import (
	"fmt"
	"strings"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

// JiraIssue is the canonical work item as observed from Jira. §14: Jira data
// normalizes into the same canonical concepts — the Jira schema is not the
// ontology. TypeName follows the issuetype: "Epic" for epics, "Issue"
// otherwise, so metrics can span both sources without source-specific types.
type JiraIssue struct {
	Site      string    `json:"-"` // identity scope (Jira site host), lives in external_id
	Key       string    `json:"key"`
	Summary   string    `json:"summary"`
	Status    string    `json:"status"`
	IssueType string    `json:"issue_type"`
	Project   string    `json:"project"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// JiraIssueExternalID keys issues by site host + key; the "jira.com:" prefix
// is disjoint from every github.com: prefix, so the schemes never collide.
func JiraIssueExternalID(site, key string) string {
	return fmt.Sprintf("jira.com:%s:issue:%s", site, strings.ToUpper(key))
}

// JiraUserExternalID keys engineers that Jira created (cross-source reuse
// keeps whichever engineer came first — a github-created engineer keeps its
// github.com:email: ID even when Jira resolves to it).
func JiraUserExternalID(site, normEmail string) string {
	return fmt.Sprintf("jira.com:%s:email:%s", site, normEmail)
}

// NewJiraProvenance mirrors NewProvenance: source-side facts only — the
// run-scoped fields are the ingestion layer's job (§10).
func NewJiraProvenance(sourceURL string, sourceUpdatedAt time.Time) knowledge.Provenance {
	return knowledge.Provenance{
		Source:           "jira",
		SourceURL:        sourceURL,
		SourceUpdatedAt:  sourceUpdatedAt.UTC(),
		ConnectorVersion: "jira-connector/v0",
	}
}

func (i JiraIssue) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	typeName := "Issue"
	if strings.EqualFold(i.IssueType, "Epic") {
		typeName = "Epic"
	}
	return toKnowledgeObject(typeName, JiraIssueExternalID(i.Site, i.Key), i, prov)
}
