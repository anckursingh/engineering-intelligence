// api.go is the §24 HTTP surface over a KnowledgeStore: structured evidence,
// not only prose. Endpoints: GET /health, GET /metrics, GET /metrics/{metric},
// POST /investigations, GET /investigations/{id}, POST /comparisons
// (Milestone F), GET /board (§28), POST /ask (§29).
// Investigations are deterministic and cheap, so results live in memory —
// ponytail: persist them when a client that restarts servers exists.
package intelligence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
)

// metricDefinition is the §24 shape for a metric's contract.
type metricDefinition struct {
	Name        string   `json:"name"`
	Formula     string   `json:"formula"`
	Population  string   `json:"population"`
	Window      string   `json:"window"`
	Sources     []string `json:"sources"`
	Filters     []string `json:"filters"`
	Aggregation string   `json:"aggregation"`
	Limitations []string `json:"limitations"`
}

func defDTO(d metrics.Definition) metricDefinition {
	return metricDefinition{
		Name: d.Name, Formula: d.Formula, Population: d.Population, Window: d.Window,
		Sources: d.Sources, Filters: d.Filters, Aggregation: d.Aggregation, Limitations: d.Limitations,
	}
}

// Comparison is §24's structured metric result — the doc's example JSON
// (metric/window/value/comparison/change/epistemic_state/evidence).
type Comparison struct {
	Metric         string              `json:"metric"`
	Window         string              `json:"window"`
	Value          float64             `json:"value"`
	Comparison     float64             `json:"comparison"`
	Change         float64             `json:"change"`
	EpistemicState string              `json:"epistemic_state"`
	Evidence       []evidence.Evidence `json:"evidence"`
}

func toComparison(f Finding, windowName string, state evidence.State) Comparison {
	return Comparison{
		Metric:         f.Metric,
		Window:         windowName,
		Value:          f.To,
		Comparison:     f.From,
		Change:         f.ChangePct / 100, // doc's "change" is a fraction, e.g. 1.05
		EpistemicState: state.String(),
		Evidence:       f.Evidence,
	}
}

// windowDTO is a window as the exit contract carries it: name + RFC3339
// range.
type windowDTO struct {
	Name  string `json:"name"`
	Start string `json:"start"`
	End   string `json:"end"`
}

func toWindowDTO(w Window) windowDTO {
	return windowDTO{Name: w.Name, Start: w.Range.Start.Format(time.RFC3339), End: w.Range.End.Format(time.RFC3339)}
}

// InvestigationResult is the stored and returned shape of an investigation.
// Scope, WindowA/B and EpistemicState are the Milestone F exit contract
// (item 47): every answer carries its population, time window and epistemic
// state alongside claim, evidence and limitations.
type InvestigationResult struct {
	ID             string       `json:"id"`
	Question       string       `json:"question"`
	Statement      string       `json:"statement"`
	Scope          string       `json:"scope"`
	WindowA        windowDTO    `json:"window_a"`
	WindowB        windowDTO    `json:"window_b"`
	EpistemicState string       `json:"epistemic_state"`
	Primary        Comparison   `json:"primary"`
	Factors        []Comparison `json:"factors,omitempty"`
	Limitations    []string     `json:"limitations"`
}

// API serves the §24 endpoints over one store.
type API struct {
	store        knowledge.KnowledgeStore
	board        *boardCache
	mux          *http.ServeMux
	mu           sync.Mutex
	next         int
	done         map[string]InvestigationResult
	defaultScope string
}

func NewAPI(store knowledge.KnowledgeStore) *API {
	return NewAPIWithDefaultScope(store, "")
}

func NewAPIWithDefaultScope(store knowledge.KnowledgeStore, scope string) *API {
	a := &API{store: store, board: newBoardCache(time.Minute), done: map[string]InvestigationResult{}}
	a.defaultScope = scope
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /metrics", a.handleMetrics)
	mux.HandleFunc("GET /metrics/{metric}", a.handleMetric)
	mux.HandleFunc("POST /investigations", a.handleInvestigate)
	mux.HandleFunc("GET /investigations/{id}", a.handleGetInvestigation)
	mux.HandleFunc("POST /comparisons", a.handleComparisons)
	mux.HandleFunc("GET /board", a.handleBoard)
	mux.HandleFunc("POST /ask", a.handleAsk)
	mux.HandleFunc("GET /{$}", a.handleDashboard)
	a.mux = mux
	return a
}

func dashboardScopeURL(scope string) string {
	return "/?scope=" + url.QueryEscape(scope)
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleMetrics(w http.ResponseWriter, r *http.Request) {
	defs := make([]metricDefinition, 0, len(candidates))
	for _, c := range candidates {
		defs = append(defs, defDTO(c.def))
	}
	writeJSON(w, http.StatusOK, defs)
}

func (a *API) handleMetric(w http.ResponseWriter, r *http.Request) {
	for _, c := range candidates {
		if c.name == r.PathValue("metric") {
			writeJSON(w, http.StatusOK, defDTO(c.def))
			return
		}
	}
	writeErr(w, http.StatusNotFound, "unknown metric")
}

type windowReq struct {
	Name  string `json:"name"`
	Start string `json:"start"` // RFC3339
	End   string `json:"end"`
}

func (w windowReq) window() (Window, error) {
	if w.Name == "" {
		return Window{}, fmt.Errorf("name required")
	}
	start, err := time.Parse(time.RFC3339, w.Start)
	if err != nil {
		return Window{}, fmt.Errorf("start: %w", err)
	}
	end, err := time.Parse(time.RFC3339, w.End)
	if err != nil {
		return Window{}, fmt.Errorf("end: %w", err)
	}
	return Window{Name: w.Name, Range: metrics.Window{Start: start, End: end}}, nil
}

func (a *API) handleInvestigate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope    string    `json:"scope"`
		Question string    `json:"question"`
		WindowA  windowReq `json:"window_a"`
		WindowB  windowReq `json:"window_b"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Scope == "" {
		writeErr(w, http.StatusBadRequest, "scope required")
		return
	}
	// An omitted question keeps the §23 default; an unknown one is a 400 —
	// the caller named a question the engine cannot answer.
	question := req.Question
	if question == "" {
		question = QuestionCycleTime
	}
	primary, ok := questionPrimary[question]
	if !ok {
		writeErr(w, http.StatusBadRequest, "unsupported question")
		return
	}
	wa, err := req.WindowA.window()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid window_a: "+err.Error())
		return
	}
	wb, err := req.WindowB.window()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid window_b: "+err.Error())
		return
	}
	pop, err := Population(r.Context(), a.store, req.Scope)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	inv := MetricChange(pop, wa, wb, primary, question)
	writeJSON(w, http.StatusOK, a.record(inv, req.Scope, wa, wb))
}

func (a *API) handleGetInvestigation(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	res, ok := a.done[r.PathValue("id")]
	a.mu.Unlock()
	if !ok {
		writeErr(w, http.StatusNotFound, "unknown investigation")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// comparisonPopulation is one population side of a comparison request.
type comparisonPopulation struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	MergedPRs int    `json:"merged_prs"`
}

// handleComparisons serves POST /comparisons (Milestone F, item 45): one
// window's merged PRs compared between AI-attributed and no-positive-AI-
// evidence populations, per PR-scoped metric.
func (a *API) handleComparisons(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope string `json:"scope"`
		Start string `json:"start"` // RFC3339
		End   string `json:"end"`   // RFC3339
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Scope == "" {
		writeErr(w, http.StatusBadRequest, "scope required")
		return
	}
	start, err := time.Parse(time.RFC3339, req.Start)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "start must be RFC3339")
		return
	}
	end, err := time.Parse(time.RFC3339, req.End)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "end must be RFC3339")
		return
	}
	if !end.After(start) {
		writeErr(w, http.StatusBadRequest, "end must be after start")
		return
	}
	pop, err := Population(r.Context(), a.store, req.Scope)
	if err != nil {
		if errors.Is(err, knowledge.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "unknown scope")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	aiAttributed, noPositiveAIEvidence, rows := ComparePopulations(pop, metrics.Window{Start: start, End: end})
	writeJSON(w, http.StatusOK, struct {
		Scope       string                 `json:"scope"`
		Start       string                 `json:"start"`
		End         string                 `json:"end"`
		Populations []comparisonPopulation `json:"populations"`
		Metrics     []PopulationComparison `json:"metrics"`
		Limitations []string               `json:"limitations"`
	}{
		Scope: req.Scope, Start: req.Start, End: req.End,
		Populations: []comparisonPopulation{
			{ID: "ai_attributed", Label: "PRs with positive AI evidence", MergedPRs: aiAttributed},
			{ID: "no_positive_ai_evidence", Label: "PRs without positive AI evidence", MergedPRs: noPositiveAIEvidence},
		},
		Metrics:     rows,
		Limitations: []string{"PRs without positive AI evidence may have unknown or unobserved AI activity; this is not a human-authored population."},
	})
}

func (a *API) record(inv Investigation, scope string, wa, wb Window) InvestigationResult {
	res := InvestigationResult{
		Question:       inv.Question,
		Statement:      inv.Statement,
		Scope:          scope,
		WindowA:        toWindowDTO(wa),
		WindowB:        toWindowDTO(wb),
		EpistemicState: inv.State.String(),
		Primary:        toComparison(inv.Primary, wb.Name, inv.State),
		Limitations:    inv.Limitations,
	}
	for _, f := range inv.Factors {
		res.Factors = append(res.Factors, toComparison(f, wb.Name, inv.State))
	}
	a.mu.Lock()
	a.next++
	res.ID = fmt.Sprintf("inv-%d", a.next)
	a.done[res.ID] = res
	a.mu.Unlock()
	return res
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "marshal response: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(code)
	// A write error here is unreportable — the client is gone and the
	// handler has no error channel left.
	_, _ = w.Write(b)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
