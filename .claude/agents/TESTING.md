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

## Jira connector (§14)

Suite: `internal/jira` (unit, external package via `jiratest`) + acceptance AC-ING-002 (Memory + live AikoqlStore).

- `TestJiraSyncRun1Full` — canonical mapping: TypeName "Issue"/"Epic" by issuetype, extID `jira.com:<site-host>:issue:<KEY>` (port excluded — test-server ports must never leak into identity), provenance Source "jira" + run-stamped IngestionRun, checkpoint watermark advanced, one run record.
- `TestJiraSyncRun2Delta` — incrementality is the JQL itself: run2's query carries `updated >= "<minute-truncated watermark>"`, the fake world honors the boundary in both date formats (classic `2006/01/02 15:04` and RFC3339), so only the delta issue lands; unchanged rerun = zero counts (AC-ING-005); delta issue resolvable.
- `TestLoadCorruptFailsLoudly` (checkpoint) — corrupt checkpoint errors, never silently reset (both connectors share the file now).
- Wire time formats: Jira classic `2006-01-02T15:04:05.000-0700` and RFC3339 (fake world serves the latter).

Scope notes: library-only (no CLI wiring); no retry (ponytail — Jira 429s rare at this scale, add backoff when hit); no assignee/engineer edges (item 13 owns identity). Watermark minute-truncation re-fetches a partial minute of overlap — idempotent upsert keeps those counts honest.

## Persistent identity (§9, §15)

Suite: `internal/identity` (resolver unit tests) + acceptance AC-ID-001/002/003 (Memory + live AikoqlStore).

- Every automatic identity decision is a persistent `SourceIdentity` claim, extID `ei.com:identity:<source>:<canonical-key>` (keys `login:x` / `email:x`, noreply addresses decode to login keys), with a `RESOLVES_TO` edge to the canonical `Engineer` and the engineer koid denormalized as `canonical_engineer` for O(1) reuse reads. Claim properties are the reviewable decision (AC-ID-003): source, source_identity, matching_rule, confidence, created_at, resolved_at, canonical_engineer.
- **Never silently merge**: the only merge condition is an exact canonical-key match. Different keys stay different engineers (`TestResolveConflictingNeverMerges`); cross-source same-key reuse resolves through the other source's claim (`TestResolveCrossSourceSameEmail`, probing is email-only); tenants are isolated (`TestResolveTenantIsolation`); unusable identities (no login, no email) resolve to nothing (`TestResolveUnknownIdentity`); same identity across runs lands on the same engineer (`TestResolveSameIdentityAcrossRuns`).
- Resolver call economy (pinned by `TestResolveWithinRunUsesCache` with a counting store): a run-local seen cache makes repeat resolutions free; reuse/probe reads the denormalized property instead of traversing; `created` is derived structurally (ponytail: a crash between engineer and claim writes makes the next run miscount New — idempotent upsert means no duplicate). The `RESOLVES_TO` edge itself stays pinned by one traverse-based test (`claimTargetViaEdge`).
- **Live-budget note**: the aikoql stdio server caps ~120 calls/min per process, and the identity ACs' full-world syncs exceed it — so AC-ID-001/003 run the lean `githubtest.NewIdentityWorld` (org + repo + two commits: ann via noreply, bob via email). Before adding objects to that world, re-run the live leg (`AIKOQL_MCP_BIN` + `-run TestAcceptanceSlice1Aikoql`). Full-world identity behavior remains pinned by AC-KG-004/AC-ID-002 and the resolver suite.
- No issue→engineer edges yet (ponytail): the vocabulary grows a rel when metrics need "who works on what".

## Deterministic metric engine (§16–19)

Suite: `internal/metrics`. Pure functions of a `Population{Entity[T]}` — no store, no I/O, no context: the same population always yields the same values (reproducible). Investigations (item 17) own the traversal that builds a population.

- Types per §16: `Definition` (name/formula/population/window/sources/filters/aggregation/limitations), `Observation` (value/entity/window/evidence), `Window` (half-open `[Start, End)`, inverted → empty), `Population`, `evidence.Evidence` (§20 generic representation, metrics stamp it State CALCULATED).
- First metrics (flow set with data): `cycle_time` (merged − created, per PR, anchored at merged_at), `review_latency` (earliest valid review's submitted_at − created_at, per PR, anchored at created_at, evidence = PR + first review), `throughput` (count of merged PRs per window, evidence = every counted PR). Deployment frequency, CI failure rate, rework, AI metrics: deferred until their sources exist (no deployment/Build data).
- Temporal semantics (§19): event times ONLY — created/merged/submitted, never `updated_at`/`observed_at`. The doc's example is pinned verbatim (`TestCycleTimeExactValue`): created 09-01, merged 09-05, ingested 09-08 → 4.0 days. A source update after the window end never excludes an in-window event (`late-arriving event` case).
- §18 edge suite (`TestCycleTimeEdges`, `TestReviewLatencyEdges`, `TestThroughputEmptyWindow`, `TestCycleTimeDuplicateEvent`, `TestCycleTimeTimezoneBoundary`): empty population → empty; missing created/merged/submitted → excluded; unmerged → excluded; merged before created (out-of-order) → excluded as a data error; merged at window start included, at window end excluded (half-open); duplicates collapse by external ID; instants compared across zones (NY/Tokyo), not wall clocks.
- Invalid data never errors — it is excluded by the documented filters; every exclusion rule appears in the metric's `Definition.Filters`.

## Evidence model (§20–21)

Suite: `internal/evidence` (external package — metrics imports evidence, so the package-internal tests can't live inside it) + the metric evidence assertions.

- `Evidence` is the generic representation (§20): Type, Source, SourceURL, ObjectIDs, ObservedAt, Confidence, State. ObjectIDs are the reconstruction handle — they must resolve back through the store.
- `State` (§21): OBSERVED / CALCULATED / INFERRED / HYPOTHESIZED, never collapsed (`TestStatesDistinctAndOrdered` pins distinctness + `Strength()` order: observed 0 < calculated 1 < inferred 2 < hypothesized 3, unknown 4).
- No generated explanation text is the source of truth — `TestExplanationReconstructs` pins the §20 rule: persist the source objects, compute cycle time, re-read every cited ObjectID from the store, rebuild the population, recompute → identical value. Every insight's explanation reconstructs from source objects + deterministic calculations.
- Metrics stamp what they are: `State=StateCalculated`, Type = ontology type, ObjectIDs = the objects computed from (cycle time cites the PR; review latency the PR + first review; throughput one entry citing every counted PR). OBSERVED evidence joins when a connector emits it; investigations (item 16) own the rest of the state ladder.

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
- [x] checkpoint corruption — `TestLoadCorruptFailsLoudly`: corrupt checkpoint errors loudly (never silently reset), both connectors share the path
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
