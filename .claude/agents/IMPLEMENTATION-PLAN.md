# Engineering Intelligence — Implementation Plan (agent knowledge base)

Contract: `E:\downloads\ENGINEERING-INTELLIGENCE-TDD-IMPLEMENTATION.md`.
If a requirement is ambiguous, the TDD doc wins. Update this file as each item completes.

## Non-negotiable rules (§2, §3, §30, §31)

- AIKOQL stays a generic database; EI owns all domain semantics.
- Feature order: failing test → verify red → minimum implementation → refactor after green → acceptance → commit.
- Never rewrite existing acceptance tests to accommodate an implementation.
- Gates before every commit: `./scripts/verify.ps1` (+ `-Race` where CGO exists); acceptance + e2e where applicable.
- One coherent commit per item below, in order.

## Status

Phase 1A (GitHub slice): done. Working: Phase 1B (contract freeze).

## Commit sequence (§36)

- [x] 1. Add KnowledgeStore contract tests — `internal/knowledge/knowledgetest`, graded against Memory
- [ ] 2. Add AIKOQL public client contract — inspect live API first (MCP over TCP :9090, token init, tools/call); do NOT copy historical SDK code
- [ ] 3. Add AIKOQL client unit tests — handshake, upsert, idempotency, retrieval, relationships, traversal, error mapping, ctx cancellation (§5 order)
- [ ] 4. Implement AIKOQL client — stdlib only (net conn + JSON-RPC 2.0); transport errors never leak past it
- [ ] 5. Add AikoqlStore — `internal/knowledge/aikoql.go`, mechanical translation, passes the same contract suite
- [ ] 6. Run GitHub acceptance suite against AikoqlStore — swap the store, all ACs stay green
- [ ] 7. Add tenant-scoped identity — (tenant, source, external_id) never collide; isolation tests (§8)
- [ ] 8. Add ingestion application layer — `internal/ingestion` (§10–12): mutation model + atomicity, relationship vocabulary finalized
- [ ] 9. Refactor GitHub connector through the ingestion layer
- [ ] 10. Harden GitHub incremental sync — ctx-aware retry, merge-commit model, authoritative PR→issue links (§13)
- [ ] 11. Refine relationship vocabulary — per rel: source, target, meaning, inverse, cardinality, test (§12)
- [ ] 12. Add Jira connector — only after persistence + tenant identity + ingestion exist (§14)
- [ ] 13. Add cross-source identity resolution — persistent SourceIdentity RESOLVES_TO Engineer, never silent merges (§9, §15)
- [ ] 14. Add deterministic metric engine — `internal/metrics` (§16–19)
- [ ] 15. Add evidence model — reconstructable explanations, epistemic states (§20–21)
- [ ] 16. Add first investigation — "why did cycle time change?", association ≠ causation (§22–23)
- [ ] 17. Add investigation API (§24)
- [ ] 18. Add AI development telemetry (§25–26)
- [ ] 19. Add AI development metrics (§27)
- [ ] 20. Add first product board (§28)
- [ ] 21. Add evidence-backed engineering agent (§29)

## Do-not-build list (§35)

Connector sprawl, autonomous production changes, productivity scores / individual ranking, custom ML, causal inference, big dashboards, k8s-first deployment, microservices, chatbot before deterministic investigations, EI-specific AIKOQL schema. No complexity "because the roadmap mentions it".

## Exit gate for Phase 1B–1D (§39)

Against real AIKOQL: GitHub sync, identity resolution, idempotent rerun, checkpoint recovery, relationship creation, inbound + outbound traversal, provenance persistence, process restart. Only then move to tenant identity + ingestion.
