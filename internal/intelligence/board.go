// board.go is the first product board (§28): one response with the doc's
// five sections answering "how is engineering operating", "how is AI
// changing the workflow", and "how strong is the evidence". Empty sections
// are honest notes, never fabricated numbers, and the board carries no
// per-person data — individual ranking is impossible by construction.
// "What changed / why might it have changed" stays the investigations
// endpoint's job (§22-24); the board shows the current window.
package intelligence

import (
	"errors"
	"net/http"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
)

// boardItem is one computed value with its epistemic state and evidence.
type boardItem struct {
	Metric         string              `json:"metric"`
	Label          string              `json:"label"`
	Value          float64             `json:"value"`
	Unit           string              `json:"unit"`
	EpistemicState string              `json:"epistemic_state"`
	Evidence       []evidence.Evidence `json:"evidence"`
}

// boardSection is one §28 section; Note explains an honest absence.
type boardSection struct {
	ID    string      `json:"id"`
	Title string      `json:"title"`
	Items []boardItem `json:"items"`
	Note  string      `json:"note,omitempty"`
}

// board is the §28 response: the five sections in doc order.
type board struct {
	Sections []boardSection `json:"sections"`
}

// buildBoard computes the board for one window over one Population. The
// candidates table is the metric registry: 0-2 are the flow set, 3 is the
// AI-assisted share (the org-reachable AI metric).
func buildBoard(pop metrics.Population, win metrics.Window) board {
	var flow, ai []boardItem
	for _, c := range []candidate{candidates[0], candidates[1], candidates[2]} {
		if v, ok, ev := summarize(c.compute(pop, win)); ok {
			flow = append(flow, boardItem{Metric: c.name, Label: c.label, Value: v, Unit: c.unit,
				EpistemicState: evidence.StateCalculated.String(), Evidence: ev})
		}
	}
	if v, ok, ev := summarize(candidates[3].compute(pop, win)); ok {
		ai = append(ai, boardItem{Metric: candidates[3].name, Label: candidates[3].label, Value: v, Unit: candidates[3].unit,
			EpistemicState: evidence.StateCalculated.String(), Evidence: ev})
	}
	sections := []boardSection{
		{ID: "engineering_flow", Title: "Engineering Flow", Items: flow},
		{ID: "quality", Title: "Quality", Note: "no quality data — no CI source ingested yet"},
		{ID: "reliability", Title: "Reliability", Note: "no deployment or incident data — no sources ingested yet"},
		{ID: "ai_development", Title: "AI Development", Items: ai},
	}
	if len(flow) == 0 {
		sections[0].Note = "no flow data in this window"
	}
	if len(ai) == 0 {
		sections[3].Note = "no AI telemetry linked to this scope"
	}
	return board{Sections: append(sections, evidenceCoverage(append(flow, ai...)))}
}

// evidenceCoverage reports, per epistemic state present in the board's
// items, how many distinct objects back it — §28's "how strong is the
// evidence", in object counts, never a confidence score.
func evidenceCoverage(items []boardItem) boardSection {
	sec := boardSection{ID: "evidence_coverage", Title: "Evidence Coverage"}
	perState := map[string]map[string]bool{}
	for _, it := range items {
		for _, ev := range it.Evidence {
			m := perState[ev.State.String()]
			if m == nil {
				m = map[string]bool{}
				perState[ev.State.String()] = m
			}
			for _, id := range ev.ObjectIDs {
				m[id] = true
			}
		}
	}
	for _, s := range []evidence.State{evidence.StateObserved, evidence.StateCalculated, evidence.StateInferred, evidence.StateHypothesized, evidence.StateUnknown} {
		if ids, ok := perState[s.String()]; ok {
			sec.Items = append(sec.Items, boardItem{Metric: "evidence_coverage", Label: s.String(),
				Value: float64(len(ids)), Unit: "objects", EpistemicState: s.String()})
		}
	}
	if len(sec.Items) == 0 {
		sec.Note = "no computed values to cover — nothing has evidence yet"
	}
	return sec
}

// handleBoard serves GET /board?scope=<extID>&start=<RFC3339>&end=<RFC3339>.
func (a *API) handleBoard(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	start, err := time.Parse(time.RFC3339, q.Get("start"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "start must be RFC3339")
		return
	}
	end, err := time.Parse(time.RFC3339, q.Get("end"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "end must be RFC3339")
		return
	}
	pop, err := Population(r.Context(), a.store, q.Get("scope"))
	if err != nil {
		// An unknown scope is the caller naming something wrong (a navigation
		// typo on the board), not a server fault.
		if errors.Is(err, knowledge.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, "unknown scope")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buildBoard(pop, metrics.Window{Start: start, End: end}))
}
