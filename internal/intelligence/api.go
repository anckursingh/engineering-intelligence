// api.go is the §24 HTTP surface over a KnowledgeStore: structured evidence,
// not only prose. Endpoints: GET /health, GET /metrics, GET /metrics/{metric},
// POST /investigations, GET /investigations/{id}, GET /board (§28),
// POST /ask (§29).
// Investigations are deterministic and cheap, so results live in memory —
// ponytail: persist them when a client that restarts servers exists.
package intelligence

import (
	"encoding/json"
	"fmt"
	"net/http"
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

// InvestigationResult is the stored and returned shape of an investigation.
type InvestigationResult struct {
	ID          string       `json:"id"`
	Question    string       `json:"question"`
	Statement   string       `json:"statement"`
	Primary     Comparison   `json:"primary"`
	Factors     []Comparison `json:"factors,omitempty"`
	Limitations []string     `json:"limitations"`
}

// API serves the §24 endpoints over one store.
type API struct {
	store knowledge.KnowledgeStore
	mux   *http.ServeMux
	mu    sync.Mutex
	next  int
	done  map[string]InvestigationResult
}

func NewAPI(store knowledge.KnowledgeStore) *API {
	a := &API{store: store, done: map[string]InvestigationResult{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /metrics", a.handleMetrics)
	mux.HandleFunc("GET /metrics/{metric}", a.handleMetric)
	mux.HandleFunc("POST /investigations", a.handleInvestigate)
	mux.HandleFunc("GET /investigations/{id}", a.handleGetInvestigation)
	mux.HandleFunc("GET /board", a.handleBoard)
	mux.HandleFunc("POST /ask", a.handleAsk)
	a.mux = mux
	return a
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
	if req.Question != "" && req.Question != QuestionCycleTime {
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
	inv := CycleTimeChange(pop, wa, wb)
	writeJSON(w, http.StatusOK, a.record(inv, wb.Name))
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

func (a *API) record(inv Investigation, windowB string) InvestigationResult {
	res := InvestigationResult{
		Question:    inv.Question,
		Statement:   inv.Statement,
		Primary:     toComparison(inv.Primary, windowB, inv.State),
		Limitations: inv.Limitations,
	}
	for _, f := range inv.Factors {
		res.Factors = append(res.Factors, toComparison(f, windowB, inv.State))
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
