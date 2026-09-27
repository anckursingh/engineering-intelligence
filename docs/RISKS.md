# Risks and Open Questions

## Product risks

### R1 — Commodity dashboard

Risk:
Product becomes another DORA/AI adoption dashboard.

Mitigation:
Make evidence-backed investigation and knowledge relationships core.

### R2 — Data quality

Risk:
Cross-source data is inconsistent.

Mitigation:
Identity resolution, provenance, confidence and data-quality metrics.

### R3 — AI attribution

Risk:
It is impossible to reliably determine what AI generated.

Mitigation:
Use source telemetry where available and expose attribution confidence.

### R4 — Surveillance perception

Risk:
Developers reject the product.

Mitigation:
Focus on systems and teams; avoid individual productivity scoring.

### R5 — Causality

Risk:
Product makes unjustified claims.

Mitigation:
Separate observed, calculated and inferred statements.

### R6 — Connector explosion

Risk:
Engineering effort disappears into integrations.

Mitigation:
Start with three sources.

### R7 — AIKOQL immaturity

Risk:
Required database capabilities do not exist yet.

Mitigation:
Add only generic capabilities to AIKOQL and maintain a clean integration contract.

### R8 — Enterprise competition

Risk:
Established vendors have mature integrations and procurement credibility.

Mitigation:
Target a differentiated intelligence workflow rather than feature parity.

## Open questions

1. Which AI telemetry source should be first?
2. How reliably can AI-generated changes be attributed?
3. What is the minimum evidence required for an AI contribution?
4. Which engineering outcomes are most valuable to CTO/VP Engineering?
5. What graph/query patterns dominate real investigations?
6. What AIKOQL workload emerges at 100, 1,000 and 10,000 engineers?
7. Can the Go client remain protocol-stable as AIKOQL evolves?
8. Which deployment model should be supported first?
