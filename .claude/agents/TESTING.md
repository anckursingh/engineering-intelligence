# Engineering Intelligence — Testing Checklist (agent knowledge base)

## The TDD loop (§3)

1. Read the code, tests, interfaces and docs the feature touches.
2. Write the smallest failing test that specifies the behavior.
3. Run it: `go test ./<pkg> -run <Test> -count=1` — verify it fails for the expected reason.
4. Implement the minimum code to pass. No scaffolding while red.
5. Refactor only after green.
6. Full gate: `./scripts/verify.ps1` (+ `-Race` where CGO exists); acceptance + e2e where applicable.
7. Commit one coherent capability; update IMPLEMENTATION-PLAN.md in the same commit.

## KnowledgeStore contract matrix (Phase 1B)

Suite: `internal/knowledge/knowledgetest.RunContract`. Every store is graded against it; it is the behavioral specification, not a convenience.

| Behavior | Memory | AikoqlStore |
|---|---|---|
| create + get object | ✓ | ✓ |
| get by external ID | ✓ | ✓ |
| idempotent upsert | ✓ | ✓ |
| version update | ✓ | ✓ |
| type conflict | ✓ | ✓ |
| get missing → ErrNotFound | ✓ | ✓ |
| relationship creation | ✓ | ✓ |
| duplicate relationship collapse | ✓ | ✓ |
| outbound traversal | ✓ | ✓ |
| inbound traversal | ✓ | ✓ |
| traversal depth + cycles | ✓ | ✓ |
| missing-object behavior (relate / traverse) | ✓ | ✓ |
| context cancellation (all ops) | ✓ | ✓ |

AikoqlStore grading runs live per-test servers (`AIKOQL_MCP_BIN`, stdio, no ports/token); it skips offline.

Tenant scoping (§8), suite `knowledgetest.RunTenantContract` — `knowledge.WithTenant(store, tenant)` namespaces external IDs + rel types, strips the prefix on read-back:

| Behavior | Memory | AikoqlStore |
|---|---|---|
| same identity in two tenants → two objects | ✓ | ✓ |
| same-tenant upsert idempotent | ✓ | ✓ |
| cross-tenant traversal isolated | ✓ | ✓ |
| bare store never sees tenant data | ✓ | ✓ |

Identity resolution tenant-scoped at the sync layer: `TestTenantScopedIdentityResolution` (two tenants over one store, same email → two engineers; PR graph stays in-scope). `github.Config.Tenant` wraps the store per run; empty tenant = zero behavior change.

## Ingestion layer (§10–11), suite `internal/ingestion` (Memory + live AikoqlStore)

`ingestion.Run.Apply(store, Mutation{Objects, Relationships})` — objects first, then relationships; the first failing element stops the mutation as `*PartialFailure` ("object N"/"relationship N", errors unwrap). Atomicity is defined, never assumed: no transactions in AIKOQL, so applied elements persist (no rollback) and the compensation contract is re-apply — identical re-apply is a no-op (no version bump, no duplicate edge). Same-mutation relationships reference koids from the mutation that carried the objects (`Result.Objects`).

Run-scoped provenance (§10): Apply stamps `ObservedAt = r.StartedAt` and `IngestionRun = r.ID` per object when unset, never clobbering connector-supplied values (`stamps-provenance` subtest, Memory + live). The GitHub connector routes every upsert/relate through Apply; identity-resolution maps and checkpoint advance-after-success stay in the connector.

## Relationship vocabulary (§12)

Every persisted relationship type is a data contract — do not rename casually. Per-rel table (source/target/meaning/inverse/cardinality/test) lives in `internal/ontology/rels.go` with the constants; each row is exercised by AC-KG-001 or the sync suite:

| Rel | Source → Target | Direction change |
|---|---|---|
| BELONGS_TO | Repository → Organization | same as old PART_OF |
| MERGED_AS | PullRequest → Commit | REVERSED from old PART_OF (commit→pr) |
| CONTAINS_REVIEW | PullRequest → Review | REVERSED from old PART_OF (review→pr) |
| AUTHORED / IMPLEMENTS / TARGETS / REVIEWED_BY | unchanged | — |

PASSED (PullRequest → Build) is reserved until Build fetch lands.

## Connector hardening (§13)

- [x] 13.1 context-interruptible retry backoff — `TestSyncContextCancelDuringRetry` (cancel mid-backoff returns promptly, `errors.Is(err, context.Canceled)`); the test seam (`cfg.Sleep`) stays non-interruptible by design, production uses timer+select
- [x] 13.2 incremental ingestion design — deferred: full-list polling stays until webhooks/events + cursor + reconciliation have equivalent acceptance coverage (do not build first)
- [x] 13.3 merge commit model — `TestMergeCommitModel`: squash and rebase merges land their commit on the default branch (one MERGED_AS edge via the same history lookup); unavailable merge SHA leaves no edge, no error. PR→commits/head-commit/resulting-branch-state modeling joins when metrics need it
- [x] 13.4 PR→issue linking — `TestPRToIssueLinks`: duplicate body references collapse to one IMPLEMENTS edge; a referenced issue that no longer exists (404) skips the link without failing the run. Authoritative linking data (GraphQL ClosingIssuesReferences) deferred — regex is fallback-only until it lands

Live-server spawn helper extracted to `internal/knowledge/aikoqltest.Live(t)` (used by contract, acceptance, ingestion, benchmarks).

Store-specific guarantees (Memory only, not portable): provenance-only change never bumps version.

## AIKOQL wire contract (probe-verified, encoded in internal/knowledge/aikoql.go)

The server is the source of truth; these facts were measured against the live binary, not assumed:

- create: `remember{type_name, properties, idempotency_key=ExternalID}` → version 1; identical re-remember = no-op.
- update: keyed re-remember with changed properties is silently IGNORED; content updates require `remember{koid, expected_version}` (stale → VERSION_CONFLICT).
- the server's idempotency index is global and type-blind: key reuse under another type_name silently returns the existing object. AikoqlStore keeps its own `ExternalIDIndex` objects and detects ErrTypeConflict client-side.
- no untyped KOQL exists (`MATCH *` and unknown entity names are COMPILE_ERROR) → GetByExternalID resolves via `ExternalIDIndex`.
- relate: duplicate = no-op, bumps FROM version once.
- traverse: UNDIRECTED, ignores the direction argument; hits carry a per-hit `direction` label relative to the query node → directed traversal is a client-side BFS of depth-1 traverses filtered by label (cycle topology verified).
- errors: NOT_FOUND + VALIDATION_ERROR (malformed koid) → ErrNotFound; everything else stays a wrapped transport error.

## AIKOQL client tests (§5 order)

- [x] client construction — covered by SDK's own suite; adapter unit tests script `CallTool` via fakeDB
- [x] object upsert / idempotency / retrieval — TestAikoqlCreateWritesIndexAndInjectBookkeeping, TestAikoqlUpdateBumpsVersionOnChangeOnly, contract suite
- [x] relationships (relate, duplicate collapse) — contract suite relate-and-duplicate
- [x] traversal (outbound, inbound, depth, cycles) — TestAikoqlTraverseFiltersByDirectionLabel, TestAikoqlTraverseDepthTwoCycleSafe, contract suite
- [x] error mapping (NOT_FOUND/VALIDATION_ERROR → knowledge errors only) — TestAikoqlErrorMapping, TestAikoqlRelateNotFound
- [x] context cancellation (all ops) — contract suite + TestAikoqlContextCancellation

## Regression contracts (§31)

A feature is not complete if any of these regress:
AC-ING-001, AC-ING-004, AC-ING-005, AC-ID-002, AC-KG-001, AC-KG-004, AC-REL-001, AC-REL-002
(`go test -run TestAcceptanceSlice1 ./test/acceptance/`; the same suite runs against a live
AikoqlStore with `AIKOQL_MCP_BIN` set via `-run TestAcceptanceSlice1Aikoql`)

## Failure semantics (§34)

- [x] GitHub unavailable / rate limit
- [x] AIKOQL unavailable — server errors mapped: NOT_FOUND/VALIDATION_ERROR → ErrNotFound, rest wrapped
- [x] network timeout / ctx cancellation mid-call — ctx pre-checks on all five ops; SDK projects deadlines onto the socket
- [x] partial source failure — ingestion.Run.Apply: first failure = *PartialFailure, prior elements persist, re-apply compensates
- [x] process restart — TestAikoqlRestartPersistence (same dbDir, fresh server, data + traversal intact)
- [ ] checkpoint corruption — add when a second consumer shares the checkpoint (Jira)
- [ ] duplicate / out-of-order event — lands with §13.2 event-driven ingestion (webhooks); current polling model cannot observe out-of-order events

Invariant: **never advance a committed checkpoint beyond data that has not been durably persisted.**

## Performance baselines (§32)

- [x] per-op latency, live server over stdio (fresh DB per bench, `AIKOQL_MCP_BIN`, `go test ./internal/knowledge -run '^$' -bench BenchmarkAikoql -benchtime=1x`):
  - upsert (create path, 4 round trips: index MATCH + remember + defensive get + index remember) ≈ **8.5ms/op**
  - get by external ID (2 round trips: index MATCH + get) ≈ **2.1ms/op**
  - depth-3 directed traverse, 20-node chain ≈ **4.9ms/op**
- [ ] 1K / 10K / 100K / 1M objects: **blocked** — the stdio transport has a server-side fixed rate limit of **120 calls/min** (error `[-32000] rate limit exceeded (max 120 calls/min)`, key `_stdio`; no CLI knob in `serve --help`). A 1K-object setup costs ~4K calls ≈ 33+ min. Scale path: the server's `batch` tool (adapter-side batching), or a raised server limit — do not build until a real sync needs it.
- [ ] connector scale: 10 / 100 / 1000 repositories; 10K / 100K / 1M issues

Do not claim scalability until measured. Currently measured: per-op latency only.

## Observability (§33)

- [ ] run_id, tenant_id, source, connector_version, started_at / ended_at, objects seen / created / updated / skipped, relationships_written, errors, checkpoint
- [ ] never log access tokens, secrets, private AI prompts, unnecessary PII

## Definition of Done per feature (§37)

All applicable rows true before "done":

- **Code**: implementation complete, interfaces documented, no duplicated abstraction, no domain leakage into AIKOQL.
- **TDD**: failing test existed before implementation; unit + integration + acceptance pass.
- **Reliability**: failure behavior tested; retry tested where relevant; context cancellation tested; idempotency tested where relevant.
- **Data**: provenance preserved; temporal semantics defined; identity semantics defined; evidence lineage preserved.
- **Product**: acceptance criterion mapped; limitations documented; user-visible behavior defined.
- **Operations**: logs/metrics where needed; checkpoint behavior defined; secrets protected.
