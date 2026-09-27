// agent.go is the evidence-backed engineering agent (§29): natural language
// in, evidence-referenced answer out. Classification routes the supported
// question to the deterministic engine — question classification → AIKOQL
// retrieval (Population) → graph traversal → metric calculation → evidence
// collection → reasoning (CycleTimeChange) → answer with evidence references.
// Unknown questions get an honest refusal, never an invented answer: the
// agent's "reasoning" IS the deterministic engine, so there is no LLM step
// to drift and nothing to fabricate from.
package intelligence

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
)

// Query is a natural-language question with its data scope and the period it
// asks about. The comparison window is derived — the immediately preceding
// period of equal length — the natural reading of "change".
type Query struct {
	Text  string    `json:"question"`
	Scope string    `json:"scope"`
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
}

// Answer is the evidence-referenced answer: the engine's statement plus the
// objects it was computed from (§29's last step).
type Answer struct {
	Question    string              `json:"question"`
	Class       string              `json:"class"`
	Statement   string              `json:"statement"`
	Evidence    []evidence.Evidence `json:"evidence"`
	Limitations []string            `json:"limitations"`
}

// Ask classifies the question and runs the matching engine.
func Ask(ctx context.Context, store knowledge.KnowledgeStore, q Query) (Answer, error) {
	if classify(q.Text) == "cycle_time_change" {
		return askCycleTimeChange(ctx, store, q)
	}
	return Answer{
		Question: q.Text,
		Class:    "unsupported",
		Statement: "I can only answer: " + QuestionCycleTime +
			" (the comparison window is the period before the one you give).",
	}, nil
}

// classify matches normalized phrasing to an engine class. "" = unsupported.
func classify(text string) string {
	norm := strings.ToLower(text)
	if strings.Contains(norm, "cycle time") && (hasChangeWord(norm) || strings.HasPrefix(strings.TrimSpace(norm), "why")) {
		return "cycle_time_change"
	}
	return ""
}

var changeWords = []string{
	"change", "changed", "changing", "increase", "decrease",
	"slower", "faster", "higher", "lower", "worse", "improved",
}

func hasChangeWord(norm string) bool {
	for _, w := range changeWords {
		if strings.Contains(norm, w) {
			return true
		}
	}
	return false
}

// askCycleTimeChange answers "why did cycle time change": the given period
// against the immediately preceding period of equal length.
func askCycleTimeChange(ctx context.Context, store knowledge.KnowledgeStore, q Query) (Answer, error) {
	pop, err := Population(ctx, store, q.Scope)
	if err != nil {
		return Answer{}, err
	}
	length := q.To.Sub(q.From)
	inv := CycleTimeChange(pop,
		Window{Name: "Previous period", Range: metrics.Window{Start: q.From.Add(-length), End: q.From}},
		Window{Name: "Current period", Range: metrics.Window{Start: q.From, End: q.To}},
	)
	ev := append([]evidence.Evidence{}, inv.Primary.Evidence...)
	for _, f := range inv.Factors {
		ev = append(ev, f.Evidence...)
	}
	return Answer{
		Question:    q.Text,
		Class:       "cycle_time_change",
		Statement:   inv.Statement,
		Evidence:    ev,
		Limitations: inv.Limitations,
	}, nil
}

func (a *API) handleAsk(w http.ResponseWriter, r *http.Request) {
	var q Query
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if q.Text == "" {
		writeErr(w, http.StatusBadRequest, "question required")
		return
	}
	if q.Scope == "" {
		writeErr(w, http.StatusBadRequest, "scope required")
		return
	}
	if q.From.IsZero() || q.To.IsZero() || q.To.Before(q.From) {
		writeErr(w, http.StatusBadRequest, "from and to must be RFC3339, to after from")
		return
	}
	ans, err := Ask(r.Context(), a.store, q)
	if err != nil {
		if errors.Is(err, knowledge.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "unknown scope")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ans)
}
