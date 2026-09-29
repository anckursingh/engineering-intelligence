// agent.go is the evidence-backed engineering agent (§29): natural language
// in, evidence-referenced answer out. Classification routes each supported
// question to the deterministic engine — question classification → AIKOQL
// retrieval (Population) → graph traversal → metric calculation → evidence
// collection → reasoning (MetricChange) → answer with evidence references.
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

// classQuestions maps engine classes to their supported question; the
// primary metric comes from questionPrimary.
var classQuestions = map[string]string{
	"cycle_time_change":     QuestionCycleTime,
	"ci_quality_change":     QuestionCIQuality,
	"ai_adoption_change":    QuestionAIAdoption,
	"review_latency_change": QuestionReviewLatency,
}

// supportedQuestions renders the refusal list, in declaration order.
var supportedQuestions = []string{QuestionCycleTime, QuestionCIQuality, QuestionAIAdoption, QuestionReviewLatency}

// Ask classifies the question and runs the matching engine.
func Ask(ctx context.Context, store knowledge.KnowledgeStore, q Query) (Answer, error) {
	class := classify(q.Text)
	if question, ok := classQuestions[class]; ok {
		return askChange(ctx, store, q, class, question)
	}
	return Answer{
		Question: q.Text,
		Class:    "unsupported",
		Statement: "I can only answer: " + strings.Join(supportedQuestions, "; ") +
			" (the comparison window is the period before the one you give).",
	}, nil
}

// classify matches normalized phrasing to an engine class: a topic phrase
// plus a change word (or a why-question) routes to the matching engine.
// "" = unsupported.
func classify(text string) string {
	norm := strings.ToLower(text)
	change := hasChangeWord(norm) || strings.HasPrefix(strings.TrimSpace(norm), "why")
	switch {
	case change && strings.Contains(norm, "cycle time"):
		return "cycle_time_change"
	case change && strings.Contains(norm, "ci quality"):
		return "ci_quality_change"
	case change && strings.Contains(norm, "ai adoption"):
		return "ai_adoption_change"
	case change && strings.Contains(norm, "review latency"):
		return "review_latency_change"
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

// askChange answers one supported question: the given period against the
// immediately preceding period of equal length, through the shared
// MetricChange engine.
func askChange(ctx context.Context, store knowledge.KnowledgeStore, q Query, class, question string) (Answer, error) {
	pop, err := Population(ctx, store, q.Scope)
	if err != nil {
		return Answer{}, err
	}
	length := q.To.Sub(q.From)
	inv := MetricChange(pop,
		Window{Name: "Previous period", Range: metrics.Window{Start: q.From.Add(-length), End: q.From}},
		Window{Name: "Current period", Range: metrics.Window{Start: q.From, End: q.To}},
		questionPrimary[question], question,
	)
	ev := append([]evidence.Evidence{}, inv.Primary.Evidence...)
	for _, f := range inv.Factors {
		ev = append(ev, f.Evidence...)
	}
	return Answer{
		Question:    q.Text,
		Class:       class,
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
