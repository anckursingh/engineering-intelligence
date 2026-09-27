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

Phase 1A (GitHub slice): done. Working: item 15 — evidence model (§20–21).

## Commit sequence (§36)

- [x] 1. Add KnowledgeStore contract tests — `internal/knowledge/knowledgetest`, graded against Memory
- [x] 2. Add AIKOQL public client contract — live API probed over stdio (wire facts in TESTING.md §AIKOQL wire contract); consumed the user's SDK at `../Mnemosyne/crates/sdk/go` via go.mod replace, no historical code copied
- [x] 3. Add AIKOQL client unit tests — fakeDB-scripted: upsert/idempotency/retrieval/relationships/traversal/error mapping/ctx cancellation (§5 order)
- [x] 4. Implement AIKOQL client — `internal/knowledge/aikoql.go` (AikoqlStore over a thin `aikoqlDB` CallTool surface); transport errors never leak past `knowledge` errors
- [x] 5. Add AikoqlStore — passes the same contract suite (11/11 live) + restart persistence
- [x] 6. Run GitHub acceptance suite against AikoqlStore — suite parametrized on a StoreFactory; all 6 ACs green on Memory AND live AikoqlStore (TestAcceptanceSlice1Aikoql)
- [x] 7. Add tenant-scoped identity — `knowledge.WithTenant`: (tenant, source, external_id) never collide; tenant contract 4/4 on Memory AND live AikoqlStore; sync-level resolution test (§8); no UI/auth added
- [x] 8. Add ingestion application layer — `internal/ingestion` (§10–11): Mutation{Objects, Relationships} with defined non-atomicity (no tx in AIKOQL → *PartialFailure + re-apply compensation), Run identity/ordering; suite 5/5 on Memory AND live AikoqlStore
- [x] 9. Refactor GitHub connector through the ingestion layer — every sync write routes through `ingestion.Run.Apply`; the layer stamps run-scoped provenance (ObservedAt, IngestionRun) per object, never clobbering connector-supplied values; `ontology.NewProvenance(sourceURL, sourceUpdatedAt)` no longer carries run-scoped fields. Identity-resolution orchestration (run-local maps) and checkpoint advance-after-success stay in the connector until a second source makes them shared. All suites green on Memory AND live AikoqlStore
- [x] 10. Harden GitHub incremental sync (§13) — 13.1 retry backoff is context-interruptible (timer+select; TestSyncContextCancelDuringRetry proves prompt return); 13.2 keep full-list polling — replacement (webhooks/events + cursor + reconciliation) must carry equivalent acceptance coverage first; 13.3 merge-commit model tested across merge/squash/rebase/unavailable-merge-SHA (TestMergeCommitModel — one merge-commit edge, no edge on unavailable, head-commit modeling deferred until metrics need it); 13.4 PR→issue linking: duplicate body references collapse to one edge, nonexistent referenced issue skips without failing the run (TestPRToIssueLinks); authoritative linking data (GraphQL ClosingIssuesReferences) deferred with a `ponytail:` note — regex stays the fallback-only mechanism
- [x] 11. Refine relationship vocabulary — absorbed into item 9: `internal/ontology/rels.go` carries the per-rel table (source, target, meaning, inverse, cardinality, test) + constants; PART_OF split into BELONGS_TO / MERGED_AS / CONTAINS_REVIEW (§12)
- [x] 12. Add Jira connector — `internal/jira` (§14): stdlib-HTTP REST v3 client (basic auth, issue search), `checkpoint.Jira.UpdatedSince` watermark, `RunRecord.Project`; normalizes into canonical ontology (`ontology.JiraIssue` — TypeName "Issue"/"Epic", never the Jira schema; extID `jira.com:<site-host>:issue:<KEY>`, port excluded so test-server ports never leak into identity); incrementality = the JQL `updated >= "<minute-truncated watermark>"` filter itself (server-side, not client guesswork), watermark advances only after full success (AC-ING-004); counts honest via pre-upsert version compare, identical rerun = zero (AC-ING-005); fake world (`jiratest`) honors the updated>= boundary in both date formats. Scope: library-only — no CLI wiring, no retry (ponytail: Jira 429s rare at this scale, add backoff when hit), no assignee/engineer edges (item 13 owns identity). Green on Memory AND live AikoqlStore (acceptance AC-ING-002)
- [x] 13. Add cross-source identity resolution — `internal/identity` (§9, §15): every automatic decision is a persistent SourceIdentity claim (`ei.com:identity:<source>:<key>`) with RESOLVES_TO edge + denormalized `canonical_engineer` koid to its canonical Engineer; merging ONLY on exact canonical-key match (`login:x`/`email:x`) — different keys stay different engineers, never silently merged (AC-ID-003); cross-source reuse probes the other source's claim (email keys only); noreply addresses decode to login keys; confidence by rule (login/noreply 1.0, email 0.7); claims carry rule/confidence/created_at/resolved_at (AC-ID-002/003); github and jira connectors route all reporter/assignee/author/reviewer identity through the resolver. Live-aikoql call budget (120/min per server): resolver keeps a run-local seen cache, reuse reads the denormalized property (no traverse), created is derived structurally — and the identity acceptance ACs run a lean fixture world (`githubtest.NewIdentityWorld`). Green on Memory AND live AikoqlStore (AC-ID-001/002/003)
- [x] 14. Add deterministic metric engine — `internal/metrics` (§16–19): pure functions of a Population (no store, no I/O — investigations own the traversal), Definition/Observation/Window/Population/Evidence types per §16; first metrics are the flow set that has data: cycle time (merged − created, one observation per PR anchored at merged_at), review latency (earliest review's submitted_at − created_at), throughput (count of merged PRs per window) — deployment/CI/rework/AI metrics deferred until their sources exist; temporal semantics §19: event times only, never source-update or observation times ("created 09-01, merged 09-05, ingested 09-08 → four days, not seven" pinned verbatim); §18 deterministic suite: exact values, empty population, missing timestamps, duplicate events, timezone boundary (NY/Tokyo zones), partial data, late-arriving event, out-of-order event, half-open window boundaries
- [ ] 15. Add evidence model — reconstructable explanations, epistemic states (§20–21)
- [ ] 16. Add first investigation — "why did cycle time change?", association ≠ causation (§22–23)
- [ ] 17. Add investigation API (§24)
- [ ] 18. Add AI development telemetry (§25–26)
- [ ] 19. Add AI development metrics (§27)
- [ ] 20. Add first product board (§28)
- [ ] 21. Add evidence-backed engineering agent (§29)

## Do-not-build list (§35)

Connector sprawl, autonomous production changes, productivity scores / individual ranking, custom ML, causal inference, big dashboards, k8s-first deployment, microservices, chatbot before deterministic investigations, EI-specific AIKOQL schema. No complexity "because the roadmap mentions it".

## Exit gate for Phase 1B–1D (§39) — PASSED

Against real AIKOQL: GitHub sync, identity resolution, idempotent rerun, checkpoint recovery, relationship creation, inbound + outbound traversal, provenance persistence, process restart — all green (contract 11/11 + acceptance 6/6 on live AikoqlStore). Behavior AND performance measured (§32): upsert 8.5ms, lookup 2.1ms, depth-3 traverse 4.9ms; stdio rate limit 120 calls/min blocks large-N runs — batch tool is the scale path when needed. Only then move to tenant identity + ingestion.
