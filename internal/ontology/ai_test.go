// AI development telemetry tests (§25-26): the canonical AI concepts stay
// vendor-neutral (provider/source are attributes, never types) and AI
// attribution is evidence-based — DIRECT/STRONG require named telemetry, and
// there is no code-style inference path.
package ontology

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

func TestAIExternalIDCollisions(t *testing.T) {
	ids := []string{
		AgentExternalID("anthropic", "opus"), ModelExternalID("anthropic", "opus"), // same provider/name, different prefix
		SessionExternalID("claude-code", "s1"), InteractionExternalID("claude-code", "s1"),
		AgentRunExternalID("claude-code", "r1"), AgentTaskExternalID("claude-code", "r1"),
		AgentOutcomeExternalID("claude-code", "r1"), AIContributionExternalID("claude-code", "r1"),
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate external id %q", id)
		}
		seen[id] = true
	}
}

// TestAITypesMap pins the §25 mapping: every concept maps to a knowledge
// object under the ei.com AI namespace with provenance, and the nested
// structs (session timestamps, attribution) survive the round trip.
func TestAITypesMap(t *testing.T) {
	prov := NewSourceProvenance("claude-code", "https://example.com/session/s1", time.Now().UTC())
	t0 := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		typName string
		extID   string
		mapFn   func(knowledge.Provenance) (knowledge.KnowledgeObject, error)
	}{
		{"agent", "Agent", AgentExternalID("anthropic", "opus"),
			(Agent{Name: "opus", Provider: "anthropic", Model: "claude-opus", Source: "claude-code"}).KnowledgeObject},
		{"model", "Model", ModelExternalID("anthropic", "claude-opus"),
			(Model{Name: "claude-opus", Provider: "anthropic", Source: "claude-code"}).KnowledgeObject},
		{"coding session", "CodingSession", SessionExternalID("claude-code", "s1"),
			(CodingSession{Source: "claude-code", SessionID: "s1", Agent: AgentExternalID("anthropic", "opus"), StartedAt: t0, EndedAt: t0.Add(time.Hour)}).KnowledgeObject},
		{"interaction", "Interaction", InteractionExternalID("claude-code", "i1"),
			(Interaction{Source: "claude-code", ID: "i1", Agent: AgentExternalID("anthropic", "opus"), StartedAt: t0}).KnowledgeObject},
		{"agent run", "AgentRun", AgentRunExternalID("claude-code", "r1"),
			(AgentRun{Source: "claude-code", ID: "r1", Agent: AgentExternalID("anthropic", "opus"), StartedAt: t0, EndedAt: t0.Add(time.Hour), Status: "completed", CostUSD: 0.42}).KnowledgeObject},
		{"agent task", "AgentTask", AgentTaskExternalID("claude-code", "t1"),
			(AgentTask{Source: "claude-code", ID: "t1", Run: AgentRunExternalID("claude-code", "r1"), Description: "fix bug", Status: "completed", Retries: 1, HumanIntervention: true, CompletedAt: t0.Add(time.Hour)}).KnowledgeObject},
		{"agent outcome", "AgentOutcome", AgentOutcomeExternalID("claude-code", "o1"),
			(AgentOutcome{Source: "claude-code", ID: "o1", Task: AgentTaskExternalID("claude-code", "t1"), Status: "succeeded", CompletedAt: t0.Add(time.Hour)}).KnowledgeObject},
		{"code contribution", "CodeContribution", AIContributionExternalID("claude-code", "c1"),
			(CodeContribution{Source: "claude-code", ID: "c1", Repository: "acme/widgets", PRNumber: 42,
				Session:     SessionExternalID("claude-code", "s1"),
				Attribution: Attribution{Level: AttributionDirect, Source: "claude-code", Evidence: "session:s1", AttributedAt: t0}}).KnowledgeObject},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ko, err := c.mapFn(prov)
			if err != nil {
				t.Fatal(err)
			}
			if ko.TypeName != c.typName || ko.ExternalID != c.extID {
				t.Errorf("type/ext = %q/%q, want %q/%q", ko.TypeName, ko.ExternalID, c.typName, c.extID)
			}
			if ko.Provenance.Source != "claude-code" {
				t.Errorf("provenance source = %q, want claude-code", ko.Provenance.Source)
			}
		})
	}

	// Nested structs survive the round trip: session with timestamps,
	// contribution with attribution.
	session := CodingSession{Source: "claude-code", SessionID: "s1", Agent: AgentExternalID("anthropic", "opus"), StartedAt: t0, EndedAt: t0.Add(time.Hour)}
	sessionKO, err := session.KnowledgeObject(prov)
	if err != nil {
		t.Fatal(err)
	}
	if back := roundTripAI[CodingSession](t, sessionKO); !reflect.DeepEqual(back, session) {
		t.Errorf("session round trip = %+v, want %+v", back, session)
	}

	contrib := CodeContribution{Source: "claude-code", ID: "c1", Repository: "acme/widgets", PRNumber: 42,
		Session:     SessionExternalID("claude-code", "s1"),
		Attribution: Attribution{Level: AttributionDirect, Source: "claude-code", Evidence: "session:s1", AttributedAt: t0}}
	contribKO, err := contrib.KnowledgeObject(prov)
	if err != nil {
		t.Fatal(err)
	}
	if back := roundTripAI[CodeContribution](t, contribKO); !reflect.DeepEqual(back, contrib) {
		t.Errorf("contribution round trip = %+v, want %+v", back, contrib)
	}
}

// TestAttributionValidate pins §26: DIRECT and STRONG claims must name their
// telemetry source and evidence. A bare DIRECT claim — attribution asserted
// with nothing behind it — is invalid; the only way to DIRECT is explicit
// telemetry. (There is no function that derives attribution from code style:
// UNKNOWN stays UNKNOWN until telemetry says otherwise.)
func TestAttributionValidate(t *testing.T) {
	cases := []struct {
		name string
		att  Attribution
		want bool
	}{
		{"direct with telemetry", Attribution{Level: AttributionDirect, Source: "claude-code", Evidence: "session:s1"}, true},
		{"strong with telemetry", Attribution{Level: AttributionStrong, Source: "claude-code", Evidence: "session:s1"}, true},
		{"direct without source", Attribution{Level: AttributionDirect, Evidence: "session:s1"}, false},
		{"direct without evidence", Attribution{Level: AttributionDirect, Source: "claude-code"}, false},
		{"strong without source", Attribution{Level: AttributionStrong, Evidence: "session:s1"}, false},
		{"inferred bare", Attribution{Level: AttributionInferred}, true},
		{"unknown bare", Attribution{Level: AttributionUnknown}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.att.Validate()
			if (err == nil) != c.want {
				t.Errorf("Validate(%+v) = %v, want valid=%v", c.att, err, c.want)
			}
		})
	}
}

func roundTripAI[T any](t *testing.T, ko knowledge.KnowledgeObject) T {
	t.Helper()
	props, err := json.Marshal(ko.Properties)
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err := json.Unmarshal(props, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
