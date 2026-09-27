# Competitive Teardown

## Purpose

Compare the proposed product against established engineering intelligence platforms.

This document intentionally distinguishes market capabilities from our proposed product direction.

## Competitive set

### Jellyfish

Strengths:
- executive engineering intelligence
- investment allocation
- AI impact
- engineering planning
- broad integrations

Implication:
Do not compete by building another executive DORA dashboard.

### LinearB

Strengths:
- delivery workflow
- engineering metrics
- workflow automation
- AI impact
- forecasting

Implication:
Workflow automation and PR analytics are already mature.

### DX

Strengths:
- developer experience
- engineering intelligence
- AI code insights
- AI dollar impact
- agent experience

Implication:
AI attribution and agent measurement are already competitive territory.

### Swarmia

Strengths:
- engineering productivity
- DORA
- developer experience
- AI adoption and cost
- benchmarks

Implication:
AI adoption metrics alone are commodity.

### Oobeya

Strengths:
- engineering intelligence
- AI impact
- AI chat
- multiple engineering data sources
- enterprise deployment options

Implication:
Natural-language engineering analytics is already emerging.

### Faros

Strengths:
- broad engineering data integration
- normalized engineering data
- productivity
- quality
- DORA
- AI evaluation

Implication:
Cross-source normalization is not itself a moat.

### Cortex

Strengths:
- software catalog
- platform engineering
- engineering intelligence
- AI-related insights

Implication:
Service and architecture context can become part of engineering intelligence.

## Capability comparison

| Capability | Market | Proposed product |
|---|---|---|
| DORA | Mature | Required |
| PR analytics | Mature | Required |
| Developer experience | Mature/emerging | Later |
| AI adoption | Emerging/mature | Required |
| AI cost | Emerging | Required where available |
| AI code attribution | Emerging | Required |
| Agent effectiveness | Emerging | Core |
| Cross-source identity | Mature capability | Core |
| Engineering knowledge graph | Uneven | Core |
| Temporal reasoning | Uneven | Core |
| Evidence lineage | Uneven | Core |
| Explainable investigations | Emerging | Core |
| Autonomous engineering agent | Emerging | Later |
| Open/self-hosted | Available from some vendors | Strategic option |

## Strategic conclusion

The product must not position itself as:

- another DORA dashboard
- another developer productivity score
- another AI adoption dashboard
- a cheaper Jellyfish
- a cheaper LinearB

The product thesis is:

**A temporal engineering knowledge system that explains relationships between work, code, delivery, production, AI assistance and autonomous agents.**

## Evidence requirement

Competitive claims and pricing should be revalidated before any external marketing or sales material is produced. Vendor packaging changes frequently.

---

# Moat & Viability Assessment

*Assessment: 2026-09-27. Market claims revalidated against 2026 sources; revalidate again before external use.*

## 1. What changed since the comparison above

The competitive set's direction has been confirmed and accelerated:

- Every incumbent now sells "AI impact" (DX's AI Measurement Framework — utilisation/impact/cost; LinearB and Jellyfish pivoting from team monitoring to AI-process monitoring). AI attribution is now table stakes, not emerging.
- ~27% of production code in Q1 2026 was AI-generated (Zylos/DX research); GitClear's 623M-change analysis shows churn up ~15% and copy-paste changes nearly doubling since 2022. The market need is real and growing.
- The "delegation gap" is being named publicly: incumbent platforms treat agent output as a black box — a PR delayed by a logic error and one delayed by scope creep look identical. This is precisely the gap this product targets, and it is now visible enough that competitors are moving toward it.
- InfoWorld's 2026 disruption thesis: engineering-analytics dashboards themselves are at risk as AI agents answer metric questions directly, and the customer becomes "an AI agent's MCP server." This validates AIKOQL's MCP-native bet — and warns that a dashboard-first form factor is the weakest part of this product.
- Agent memory grew up: Mem0 (~$24M raised), Letta (~$10M), Zep/Graphiti (~$12M) are funded. The published critique of that category — they store facts, not interpretations; they cannot answer "why does the system believe this" — is AIKOQL's exact pitch. Zep's Graphiti (bi-temporal graph, Neo4j) is the closest technical neighbor.

## 2. Moat analysis

| Claimed moat | Status | Reality check |
|---|---|---|
| Provenance in the write path (AIKOQL) | Real, measured | W3 experiments show measurable wins over RAG baselines on temporal validity, conflict resolution, provenance citation, longitudinal recall. Structural provenance is the one thing a copycat must rearchitect rather than reskin. But it is only a moat if buyers care about lineage — today they buy dashboards. |
| Epistemic states (observed/calculated/inferred/hypothesized) | Real, unusual | Implements principles 5–6 as a storage property, not a UI label. No incumbent does this structurally. Unproven as a buying criterion. |
| Spec-first, deterministic benchmark culture | Process asset | Trust currency for developers; not a moat by itself. |
| Knowledge gravity / switching costs | Zero today | The classic moat in this category — and incumbents already hold it. Switching costs appear only after years of ingested org graphs. |
| Network effects (knowledge mesh) | Speculative | Cross-org knowledge sharing is blocked by privacy/security for most enterprises. Do not plan the business on it. |
| Distribution | None | No OSS mindshare of record yet (packages live, adoption unmeasured); no design partners; no sales motion. |

**Verdict: no moat today. Differentiation: real. Candidate moats: exactly two.**

1. **Structural provenance + epistemics as IP.** Write-path capture (versions, DERIVED_FROM lineage, validity boundaries, conflict objects) cannot be retrofitted cheaply. This is the only defensible technical edge, and the product principles are what convert it into a market position: "the platform that tells you what it actually knows, with receipts."
2. **Accumulated org knowledge graphs → switching costs.** Years away, contingent on winning accounts. The epistemic layer is the only reason a buyer would prefer this graph to Faros's.

## 3. Viability assessment

**The structural problem is the two-front war.** Engineering Intelligence depends on a bespoke database that is itself an ambitious platform bet. R7 names the risk; the consequences are understated:

- The product's ceiling is the DB's ceiling. The Go client does not exist; the API contract is not frozen; the semantic engine is unavailable in the current build; behavior at 1,000/10,000 engineers is unmeasured.
- The W3 build-vs-buy evidence is real but measures *agent memory* workloads, not engineering-analytics workloads. Extrapolating it to this product is a leap.
- Both markets are individually brutal: EI competes with funded incumbents pivoting onto the same delegation gap; AIKOQL competes with funded memory startups plus Claude Memory (GA) and framework-native memory.

**The two wedges, honestly compared:**

| | Wedge A: AIKOQL as agent-memory substrate | Wedge B: EI as evidence-backed engineering intelligence |
|---|---|---|
| Buyer | Developers, OSS, MCP-native stacks | CTO/VP Eng (budget, trust sales) |
| Odds | Better — the "why do you believe this" gap is unserved and measured; no surveyed framework exposes memory ops via MCP as its primary path | Worse — incumbents with data and distribution are pivoting onto the same gap right now |
| Revenue | Deferred, ecosystem-dependent | Direct, but gated on enterprise features (SSO/RBAC/audit) that are Phase 6 |
| What kills it | Mem0/Zep mindshare, framework-native memory | GitHub org-level Copilot telemetry (the source controls the data); DX owning the AI-measurement narrative |

Both are winnable as *wedges*. Neither is winnable while the same person carries both across a 12–24 month window.

## 4. Principle-by-principle PM read

- **P1 (AIKOQL stays a database)** — correct modularity, uncomfortable capital allocation. The durable moat lives in the DB; the revenue lives in the product. Fine, so long as exactly one of the two is the strategy at a time.
- **P2/P5/P6 (evidence; observation≠inference; no false causality)** — the product. Keep the bar structural, never cosmetic. This is the wedge incumbents won't copy because it slows their roadmap.
- **P4 (systems, not people)** — right call: avoids the developer-surveillance backlash and the scoreboard trap. Commercially, people-data is what sells to VPs; the aggregate/system-views design is the correct compromise. Do not cave later.
- **P7 (AI as development modes)** — ahead of the market's binary "AI or not," and ahead of the delegation-gap critique. Dependency: telemetry sources don't reliably provide this; confidence-qualified attribution is the right hedge.
- **P8/P9/P10** — sound; P10 is right-sized. Source independence is an architecture virtue, not a differentiator — keep it invisible.

## 5. Verdict and decisions

**Moat: none today.** One credible candidate (structural provenance/epistemics), one distant candidate (knowledge-graph switching costs). **Viability: conditional — viable as one wedge, not as currently scoped.**

Decisions to force this quarter:

1. **Pick the wedge.** If EI is the business: treat AIKOQL as internal infrastructure, cap its scope to what the integration contract needs, stop running two product brands. If AIKOQL is the bet: EI becomes its flagship reference app.
2. **Prove the delegation gap on one real org in six months** (Phase 1 exit). GitHub + Jira + one AI source into a working graph that answers three "why" questions with evidence. Until then the wedge is theoretical.
3. **Protect the one moat.** No metric ships without epistemic state and lineage. That is the brand — and the only thing in this plan a better-funded competitor cannot buy.
4. **Threat-watch quarterly:** GitHub/Copilot org telemetry (data owner), Zep Graphiti (closest technical neighbor), DX (owns the AI-measurement narrative today).

## 6. Sources

- [InfoWorld: How AI is upending SaaS tools](https://www.infoworld.com/article/4161544/how-ai-is-upending-saas-tools.html)
- [Metamindz: 6 Engineering Intelligence Platforms That Measure AI's Impact in 2026](https://www.metamindz.co.uk/post/engineering-intelligence-platforms-measure-ai-impact-2026)
- [The Delegation Gap: Measuring What AI Agents Cost You in Code Review](https://blog.stackademic.com/the-delegation-gap-measuring-what-ai-agents-cost-you-in-code-review-3d61c409de1e.html)
- [total-agent-memory vs the field (April 2026)](https://raw.githubusercontent.com/vbcherepanov/total-agent-memory/refs/tags/v12.1.0/docs/vs-competitors.md)
- [Atlan: Enterprise Memory for AI Agents — The Governed Substrate](https://atlan.com/know/what-is-enterprise-memory/)
- [GitKraken: 8 Secure Engineering Intelligence Platforms for Git Oversight (2026)](https://www.gitkraken.com/blog/8-secure-engineering-intelligence-platforms-for-git-oversight-2026)
