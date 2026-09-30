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

func JiraAccountExternalID(site, accountID string) string {
	return fmt.Sprintf("jira.com:%s:account:%s", site, accountID)
}

func JiraSprintExternalID(site string, sprintID int64) string {
	return fmt.Sprintf("jira.com:%s:sprint:%d", site, sprintID)
}

func JiraProjectExternalID(site, key string) string {
	return fmt.Sprintf("jira.com:%s:project:%s", site, strings.ToUpper(key))
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

type JiraSprint struct {
	Site      string `json:"-"`
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Project   string `json:"project"`
	BoardID   int64  `json:"board_id"`
	StartDate string `json:"start_date,omitempty"`
	EndDate   string `json:"end_date,omitempty"`
}

type JiraProject struct {
	Site string `json:"-"`
	ID   string `json:"id,omitempty"`
	Key  string `json:"key"`
	Name string `json:"name,omitempty"`
}

func (p JiraProject) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("JiraProject", JiraProjectExternalID(p.Site, p.Key), p, prov)
}

func (s JiraSprint) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("Sprint", JiraSprintExternalID(s.Site, s.ID), s, prov)
}

func (i JiraIssue) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	typeName := "Issue"
	if strings.EqualFold(i.IssueType, "Epic") {
		typeName = "Epic"
	}
	return toKnowledgeObject(typeName, JiraIssueExternalID(i.Site, i.Key), i, prov)
}
