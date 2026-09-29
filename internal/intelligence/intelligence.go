// Package intelligence holds the deterministic investigation engine (§22-23).
// The first investigation answers "Why did cycle time change?" by comparing
// two windows over a caller-built Population and listing which other metrics
// moved alongside it — as candidate contributing factors and associations,
// never as causes. Statements are generated text ONLY: the reconstruction
// contract (§20) lives in the evidence lineage and the deterministic engine.
package intelligence

import (
	"fmt"
	"math"
	"strings"

	"github.com/anckursingh/engineering-intelligence/internal/evidence"
	"github.com/anckursingh/engineering-intelligence/internal/metrics"
)

// QuestionCycleTime is the first supported question (§22).
const QuestionCycleTime = "Why did cycle time change?"

// Window is a named comparison window (§22).
type Window struct {
	Name  string
	Range metrics.Window
}

// Finding is one metric's change between the windows: from, to, percentage,
// and the evidence lineage the numbers reconstruct from (§20).
type Finding struct {
	Metric    string
	Label     string
	Unit      string
	From      float64
	To        float64
	ChangePct float64 // (To-From)/From*100; 0 when From == 0 (percentage undefined)
	Evidence  []evidence.Evidence
}

// Investigation is the §22 flow's output: question, comparison, candidate
// contributing factors, evidence, confidence and limitations.
type Investigation struct {
	Question    string
	Primary     Finding
	Factors     []Finding
	Statement   string
	State       evidence.State
	Confidence  float64
	Limitations []string
}

// candidate is one comparable metric: primary (cycle time) or factor.
type candidate struct {
	name    string
	label   string
	unit    string
	def     metrics.Definition
	compute func(metrics.Population, metrics.Window) []metrics.Observation
}

var candidates = []candidate{
	{"cycle_time", "Cycle time", "days", metrics.DefCycleTime, metrics.CycleTime},
	{"review_latency", "Review latency", "days", metrics.DefReviewLatency, metrics.ReviewLatency},
	{"throughput", "Throughput", "PRs", metrics.DefThroughput, metrics.Throughput},
	{"ai_assisted_pr_pct", "AI-assisted PR percentage", "%", metrics.DefAIAssistedPRPct, metrics.AIAssistedPRPct},
	{"ci_pass_rate", "CI pass rate", "%", metrics.DefCIPassRate, metrics.CIPassRate},
	{"pr_size", "PR size", "lines", metrics.DefPRSize, metrics.PRSize},
}

// limitations carry every candidate's definition limits plus the causality
// boundary — the investigation never claims more than the data supports.
var limitations = func() []string {
	var out []string
	for _, c := range candidates {
		out = append(out, c.def.Limitations...)
	}
	return append(out,
		"association, not causation — no statistical inference is performed",
		"confidence reflects the arithmetic of the comparison, not any causal link",
	)
}()

// CycleTimeChange compares cycle time between two windows and reports which
// other metrics moved alongside it (§22-23). Uncomputable factors are
// reported honestly (appeared/disappeared) or skipped (unchanged) — never
// fabricated, never causal.
func CycleTimeChange(pop metrics.Population, a, b Window) Investigation {
	primary := candidates[0]
	from, okA, evA := summarize(primary.compute(pop, a.Range))
	to, okB, evB := summarize(primary.compute(pop, b.Range))
	if !okA || !okB {
		return concluded(fmt.Sprintf("Not enough cycle_time data in %s to investigate.", firstMissing(a, b, okA, okB)))
	}
	if from == to {
		res := concluded(fmt.Sprintf("Cycle time did not change (%.1f days).", from))
		res.Primary = buildFinding(primary, from, to, evA, evB)
		return res
	}

	p := buildFinding(primary, from, to, evA, evB)
	var bld strings.Builder
	fmt.Fprintf(&bld, "%s %s from %.1f to %.1f %s.", p.Label, direction(from, to), p.From, p.To, p.Unit)
	var factors []Finding
	for _, c := range candidates[1:] {
		fA, okA, evA := summarize(c.compute(pop, a.Range))
		fB, okB, evB := summarize(c.compute(pop, b.Range))
		f, sentence, listed := factorFinding(c, fA, okA, fB, okB, evA, evB, b.Name)
		if !listed {
			continue
		}
		factors = append(factors, f)
		bld.WriteString(" ")
		bld.WriteString(sentence)
	}
	bld.WriteString(" The data supports an association, but does not establish causality.")

	res := concluded(bld.String())
	res.Primary = p
	res.Factors = factors
	return res
}

// summarize is the window aggregation: mean of the per-entity observation
// values, plus the pooled evidence lineage.
func summarize(obs []metrics.Observation) (mean float64, ok bool, ev []evidence.Evidence) {
	if len(obs) == 0 {
		return 0, false, nil
	}
	for _, o := range obs {
		mean += o.Value
		ev = append(ev, o.Evidence...)
	}
	return mean / float64(len(obs)), true, ev
}

// factorFinding renders one factor's honest sentence. Missing in exactly one
// window = appeared/disappeared (never a fake zero); unchanged = not a
// candidate; zero From = no percentage.
func factorFinding(c candidate, from float64, okA bool, to float64, okB bool, evA, evB []evidence.Evidence, bName string) (Finding, string, bool) {
	switch {
	case !okA && okB:
		return Finding{Metric: c.name, Label: c.label, Unit: c.unit, To: to, Evidence: evB},
			fmt.Sprintf("%s appeared in %s at %.1f %s.", c.label, bName, to, c.unit), true
	case okA && !okB:
		return Finding{Metric: c.name, Label: c.label, Unit: c.unit, From: from, Evidence: evA},
			fmt.Sprintf("%s disappeared in %s.", c.label, bName), true
	case from == to:
		return Finding{}, "", false
	}
	f := buildFinding(c, from, to, evA, evB)
	if from == 0 {
		return f, fmt.Sprintf("%s changed from %.1f to %.1f %s.", c.label, from, to, c.unit), true
	}
	return f, fmt.Sprintf("%s %s by %.1f%%.", c.label, direction(from, to), math.Abs(f.ChangePct)), true
}

func buildFinding(c candidate, from, to float64, evA, evB []evidence.Evidence) Finding {
	f := Finding{Metric: c.name, Label: c.label, Unit: c.unit, From: from, To: to}
	if from != 0 {
		f.ChangePct = (to - from) / from * 100
	}
	f.Evidence = append(append([]evidence.Evidence{}, evA...), evB...)
	return f
}

func direction(from, to float64) string {
	if to > from {
		return "increased"
	}
	return "decreased"
}

func firstMissing(a, b Window, okA, okB bool) string {
	if !okA {
		return a.Name
	}
	return b.Name
}

// concluded stamps the shared conclusion contract: calculated state, full
// arithmetic confidence, all limitations.
func concluded(statement string) Investigation {
	return Investigation{
		Question:    QuestionCycleTime,
		Statement:   statement,
		State:       evidence.StateCalculated,
		Confidence:  1.0,
		Limitations: limitations,
	}
}
