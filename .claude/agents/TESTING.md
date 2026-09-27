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
| create + get object | ✓ | ⬜ |
| get by external ID | ✓ | ⬜ |
| idempotent upsert | ✓ | ⬜ |
| version update | ✓ | ⬜ |
| type conflict | ✓ | ⬜ |
| get missing → ErrNotFound | ✓ | ⬜ |
| relationship creation | ✓ | ⬜ |
| duplicate relationship collapse | ✓ | ⬜ |
| outbound traversal | ✓ | ⬜ |
| inbound traversal | ✓ | ⬜ |
| traversal depth + cycles | ✓ | ⬜ |
| missing-object behavior (relate / traverse) | ✓ | ⬜ |
| context cancellation (all ops) | ✓ | ⬜ |

Store-specific guarantees (Memory only, not portable): provenance-only change never bumps version.

## AIKOQL client tests (§5 order)

- [ ] client construction (dial, `initialize` token handshake, teardown)
- [ ] object upsert
- [ ] idempotency (same external_id → no duplicate)
- [ ] retrieval (get, get-by-external-id)
- [ ] relationships (relate, duplicate collapse)
- [ ] traversal (outbound, inbound, depth)
- [ ] error mapping (transport errors → `knowledge` errors only)
- [ ] context cancellation (dial + in-flight call)

## Regression contracts (§31)

A feature is not complete if any of these regress:
AC-ING-001, AC-ING-004, AC-ING-005, AC-ID-002, AC-KG-001, AC-KG-004, AC-REL-001, AC-REL-002
(`go test -run TestAcceptanceSlice1 ./test/acceptance/`)

## Failure semantics (§34)

- [x] GitHub unavailable / rate limit
- [ ] AIKOQL unavailable — add with AikoqlStore
- [ ] network timeout / ctx cancellation mid-call — add with the client
- [ ] partial source failure — add with the ingestion layer
- [ ] process restart — live integration (§7)
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
