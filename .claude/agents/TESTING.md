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
(`go test -run TestAcceptanceSlice1 ./test/acceptance/`)

## Failure semantics (§34)

- [x] GitHub unavailable / rate limit
- [x] AIKOQL unavailable — server errors mapped: NOT_FOUND/VALIDATION_ERROR → ErrNotFound, rest wrapped
- [x] network timeout / ctx cancellation mid-call — ctx pre-checks on all five ops; SDK projects deadlines onto the socket
- [ ] partial source failure — add with the ingestion layer
- [x] process restart — TestAikoqlRestartPersistence (same dbDir, fresh server, data + traversal intact)
- [ ] checkpoint corruption — add with ingestion hardening
- [ ] duplicate / out-of-order event — add with ingestion hardening

Invariant: **never advance a committed checkpoint beyond data that has not been durably persisted.**

## Performance baselines (§32)

- [ ] 1K / 10K / 100K / 1M objects: upsert + relate throughput, lookup / traversal latency, sync duration, memory, AIKOQL round trips
- [ ] connector scale: 10 / 100 / 1000 repositories; 10K / 100K / 1M issues

Do not claim scalability until measured.

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
