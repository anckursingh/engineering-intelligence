// AI development telemetry (§25-26): canonical, vendor-neutral concepts.
// Providers and sources are attributes, never canonical types — there is no
// ClaudeCodeSession. AI attribution is evidence-based (§26): the level comes
// from source telemetry, and there is no path that infers attribution from
// what code looks like.
package ontology

import (
	"fmt"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

// Agent is an AI coding agent as observed by a telemetry source.
type Agent struct {
	Name     string `json:"name"`
	Provider string `json:"provider"` // vendor as an attribute, not a type (§25)
	Model    string `json:"model"`
	Source   string `json:"source"` // telemetry source, e.g. "claude-code"
}

// Model is an AI model observed by a telemetry source.
type Model struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Source   string `json:"source"`
}

// CodingSession is one AI coding session (an agent working on a repository).
type CodingSession struct {
	Source    string    `json:"source"`
	SessionID string    `json:"session_id"` // source-generated
	Agent     string    `json:"agent,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

// Interaction is one user/agent interaction (a prompt round).
type Interaction struct {
	Source    string    `json:"source"`
	ID        string    `json:"id"` // source-generated
	Agent     string    `json:"agent,omitempty"`
	Model     string    `json:"model,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

// AgentRun is one agent invocation: started, ended, status, cost.
type AgentRun struct {
	Source       string    `json:"source"`
	ID           string    `json:"id"`
	Agent        string    `json:"agent"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitempty"`
	Status       string    `json:"status"` // completed | failed
	CostUSD      float64   `json:"cost_usd,omitempty"`
	CostReported bool      `json:"cost_reported,omitempty"` // explicit zero is distinct from unavailable cost
}

// AgentTask is one unit of work inside a run. Retries and human
// intervention are telemetry facts, not judgments.
type AgentTask struct {
	Source            string    `json:"source"`
	ID                string    `json:"id"`
	Run               string    `json:"run"` // AgentRun external ID
	Description       string    `json:"description"`
	Status            string    `json:"status"` // pending | completed | failed
	Retries           int       `json:"retries"`
	HumanIntervention bool      `json:"human_intervention"`
	CompletedAt       time.Time `json:"completed_at,omitempty"`
}

// AgentOutcome is the result of one task.
type AgentOutcome struct {
	Source      string    `json:"source"`
	ID          string    `json:"id"`
	Task        string    `json:"task"`   // AgentTask external ID
	Status      string    `json:"status"` // succeeded | failed | partial
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// CodeContribution links AI work to a pull request, with the §26 attribution
// that says how strongly the source telemetry supports the link.
type CodeContribution struct {
	Source      string      `json:"source"`
	ID          string      `json:"id"`
	Repository  string      `json:"repository"` // owner/name
	PRNumber    int         `json:"pr_number"`
	Session     string      `json:"session,omitempty"` // CodingSession external ID
	Attribution Attribution `json:"attribution"`
}

// AttributionLevel is §26's evidence ladder. Never inferred from code style —
// only from telemetry.
type AttributionLevel string

const (
	AttributionDirect   AttributionLevel = "DIRECT"   // source telemetry states the AI work
	AttributionStrong   AttributionLevel = "STRONG"   // strong source signal, e.g. matching session + authorship
	AttributionInferred AttributionLevel = "INFERRED" // indirect evidence only
	AttributionUnknown  AttributionLevel = "UNKNOWN"
)

// Attribution is the §26 evidence-based claim that AI shaped a contribution.
type Attribution struct {
	Level        AttributionLevel `json:"level"`
	Source       string           `json:"source"`   // telemetry source that states it
	Evidence     string           `json:"evidence"` // session/interaction reference
	AttributedAt time.Time        `json:"attributed_at"`
}

// Validate enforces §26: DIRECT and STRONG require a named telemetry source
// and an evidence reference. A bare DIRECT claim is invalid — the only way
// to DIRECT is explicit telemetry. UNKNOWN is honest and always valid.
func (a Attribution) Validate() error {
	if a.Level == AttributionDirect || a.Level == AttributionStrong {
		if a.Source == "" {
			return fmt.Errorf("ontology: %s attribution requires a telemetry source", a.Level)
		}
		if a.Evidence == "" {
			return fmt.Errorf("ontology: %s attribution requires evidence", a.Level)
		}
	}
	return nil
}

// External-ID builders. The ei.com AI namespace is prefix-disjoint from the
// github.com source namespace, and each type's prefix is disjoint within it.
func AgentExternalID(provider, name string) string {
	return "ei.com:agent:" + provider + ":" + name
}
func ModelExternalID(provider, name string) string {
	return "ei.com:model:" + provider + ":" + name
}
func SessionExternalID(source, id string) string {
	return "ei.com:coding-session:" + source + ":" + id
}
func InteractionExternalID(source, id string) string {
	return "ei.com:interaction:" + source + ":" + id
}
func AgentRunExternalID(source, id string) string {
	return "ei.com:agent-run:" + source + ":" + id
}
func AgentTaskExternalID(source, id string) string {
	return "ei.com:agent-task:" + source + ":" + id
}
func AgentOutcomeExternalID(source, id string) string {
	return "ei.com:agent-outcome:" + source + ":" + id
}
func AIContributionExternalID(source, id string) string {
	return "ei.com:ai-contribution:" + source + ":" + id
}

// NewSourceProvenance is NewProvenance for non-GitHub sources (AI telemetry).
func NewSourceProvenance(source, sourceURL string, sourceUpdatedAt time.Time) knowledge.Provenance {
	return knowledge.Provenance{
		Source:           source,
		SourceURL:        sourceURL,
		SourceUpdatedAt:  sourceUpdatedAt.UTC(),
		ConnectorVersion: "ei-ai/v0",
	}
}

func (a Agent) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("Agent", AgentExternalID(a.Provider, a.Name), a, prov)
}
func (m Model) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("Model", ModelExternalID(m.Provider, m.Name), m, prov)
}
func (s CodingSession) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("CodingSession", SessionExternalID(s.Source, s.SessionID), s, prov)
}
func (i Interaction) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("Interaction", InteractionExternalID(i.Source, i.ID), i, prov)
}
func (r AgentRun) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("AgentRun", AgentRunExternalID(r.Source, r.ID), r, prov)
}
func (t AgentTask) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("AgentTask", AgentTaskExternalID(t.Source, t.ID), t, prov)
}
func (o AgentOutcome) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("AgentOutcome", AgentOutcomeExternalID(o.Source, o.ID), o, prov)
}
func (c CodeContribution) KnowledgeObject(prov knowledge.Provenance) (knowledge.KnowledgeObject, error) {
	return toKnowledgeObject("CodeContribution", AIContributionExternalID(c.Source, c.ID), c, prov)
}
