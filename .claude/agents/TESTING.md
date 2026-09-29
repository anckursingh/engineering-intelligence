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

| CONTAINS_BUILD | Repository → Build | new — the CI fetch's repo→run edge |
| HAS_BUILD | PullRequest → Build | replaced PASSED — the edge carries no outcome; the conclusion lives on the Build |
| PART_OF | Commit → PullRequest | new — PR-branch commits from the PR's commit list |

## Connector hardening (§13)

- [x] 13.1 context-interruptible retry backoff — `TestSyncContextCancelDuringRetry` (cancel mid-backoff returns promptly, `errors.Is(err, context.Canceled)`); the test seam (`cfg.Sleep`) stays non-interruptible by design, production uses timer+select
- [x] 13.2 incremental ingestion design — deferred: full-list polling stays until webhooks/events + cursor + reconciliation have equivalent acceptance coverage (do not build first)
- [x] 13.3 merge commit model — `TestMergeCommitModel`: squash and rebase merges land their commit on the default branch (one MERGED_AS edge via the same history lookup); unavailable merge SHA leaves no edge, no error. PR→commits/head-commit/resulting-branch-state modeling joins when metrics need it
- [x] 13.4 PR→issue linking — authoritative now (item 39): the sync asks GitHub's GraphQL `closingIssuesReferences` per changed PR — `TestPRToIssueLinks` (duplicate nodes collapse to one IMPLEMENTS edge; a referenced issue that no longer exists (404) skips the link without failing the run) + `TestPRAuthoritativeIssueLinks` (a PR whose body has no close keyword still links — the timeline-event case the old body regex could never see). The regex is deleted; the on-demand issue refetch stays (issues outside the incremental window)
- [x] Personal-account owners (dogfood-driven) — org endpoint first, user endpoint on 404: `TestSyncUserOwner` (org 404 → user fetch, owner is a User never an Organization, BELONGS_TO reaches `github.com:account:<login>`); `TestUserAccountKnowledgeObject` (account prefix never collides with login-keyed engineers); `TestPopulationFromUserScope` (scope walk is root-type-agnostic); fixture `githubtest.NewUserWorld`
- [x] PR merged state from `merged_at` (dogfood-driven) — the PR list endpoint leaves `merged` nil (only the single-PR GET populates it), so every PR normalized as unmerged and the board's AI Development section found "no AI telemetry" although 1,942 contributions were stored: `TestToPullRequestMergedFromMergedAt` (merged_at set → Merged true — the authoritative merge signal; absent → false)

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

## Investigation engine (§22–23)

Suite: `internal/intelligence`. The first investigation, "Why did cycle time change?", is a deterministic function `CycleTimeChange(pop, a, b Window)` over a caller-built Population — item 17's API owns population building and graph traversal.

- §22 flow outputs: Question, comparison windows, primary metric change (`Finding`: From/To/ChangePct), candidate contributing factors (other metrics that moved), evidence lineage per finding (pooled `evidence.Evidence` from both windows' observations), confidence, limitations, and a generated statement. Statements are generated text only — reconstruction still lives in evidence + the deterministic engine (§20).
- Candidates = the metrics with data: review_latency, throughput, ci_pass_rate, pr_size, review_cycles. No custom stats, no causal inference (do-not-build §35).
- §23 fixture pinned verbatim: Month A cycle time 2 days → Month B 4 days, review latency +100%, throughput −33.3% → statement ends "The data supports an association, but does not establish causality." The engine NEVER claims causation. The appeared/disappeared legs of `TestCycleTimeChangeFactorAppeared` grew review_cycles sentences when the metric landed ("appeared in Month B at 0.0 cycles" — B's reviews carry no change requests, a real zero; "disappeared in Month B").
- Honest factor rendering: unchanged → not a candidate; present in only one window → "appeared/disappeared in <window>" (never a fake zero); From == 0 → "changed from X to Y" without a percentage.
- Edges: primary unchanged → "Cycle time did not change (X days).", no factors; primary missing in a window → "Not enough cycle_time data in <name> to investigate." (a result, not an error).
- State = StateCalculated; Confidence = 1.0 — certainty of the arithmetic; the limitations field states it is not a causal link.

## Investigation API (§24)

Suite: `internal/intelligence` (`population_test.go`, `api_test.go`; Memory + live AikoqlStore).

- `Population(ctx, store, scope)` is the §22 graph step: resolve the org external ID → koid, then walk `BELONGS_TO` inbound (org → repos), `TARGETS` inbound (repo → PRs), `CONTAINS_REVIEW` outbound (PR → reviews); typed values recover via the same JSON round-trip the §20 reconstruction test uses. Org scope only (`ponytail:` repo scoping joins when a question needs it).
- `NewAPI(store)` serves the §24 endpoints via stdlib pattern routing (Go 1.22+): GET /health; GET /metrics and GET /metrics/{metric} (definitions; value queries go through investigations); POST /investigations `{scope, window_a, window_b, question?}` — 400 on malformed body, bad RFC3339 window, missing scope, or any question other than the first one; GET /investigations/{id} — results kept in memory (deterministic recompute; persist when a restarting server has a client).
- Response is §24's structured shape, not only prose: primary/factors each carry `metric/window/value/comparison/change/epistemic_state/evidence`; `change` is the doc's fraction (ChangePct/100 — 1.0 for 2→4). Plus `id/question/statement/limitations`.
- Pinned by `TestAPIInvestigationEndpoints` (the full §23 statement through the HTTP layer), `TestAPIMetricsAndHealth`, `TestAPIBadRequests`, `TestPopulationFromScope`, `TestPopulationScopeIsolation`, and live legs `TestPopulationLiveAikoql` / `TestAPIInvestigationLiveAikoql`.
- Fixture gotcha (hit and fixed): sections of a fixture must use disjoint PR numbers — external IDs collide otherwise and upserts overwrite the other window's objects.

## AI development telemetry (§25–26)

Suite: `internal/ontology` (`ai_test.go`; package-internal, Memory-store-agnostic — mapping + validation only).

- The eight canonical concepts are vendor-neutral types: Agent, Model, CodingSession, Interaction, AgentRun, AgentTask, AgentOutcome, CodeContribution. Provider and source are ATTRIBUTES, never types — no ClaudeCodeSession (`TestAITypesMap` pins all 8 TypeNames + ext IDs + provenance source).
- External IDs live under the `ei.com:` AI namespace, prefix-disjoint per type: `ei.com:agent:<provider>:<name>`, `ei.com:model:<provider>:<name>`, `ei.com:coding-session|interaction|agent-run|agent-task|agent-outcome|ai-contribution:<source>:<id>`. `TestAIExternalIDCollisions` pins the same provider/name and same source/id across types never collide.
- `NewSourceProvenance(source, sourceURL, sourceUpdatedAt)` is NewProvenance for non-GitHub sources: Source = the telemetry source (e.g. "claude-code"), ConnectorVersion `ei-ai/v0`.
- §26 attribution is evidence-based (`TestAttributionValidate` pins 7 cases): `Attribution{Level, Source, Evidence, AttributedAt}`; DIRECT and STRONG require a named telemetry source AND an evidence reference — a bare DIRECT claim is invalid. INFERRED/UNKNOWN are valid without them. There is NO function that derives attribution from code style; UNKNOWN stays UNKNOWN until telemetry says otherwise.
- No connector in this item — no real telemetry source exists to connect; PR-linking rels join when item 19's AI metrics need them.

## AI development metrics (§27)

Suite: `internal/metrics` (`ai_test.go`, package-internal) + the investigation factor tests.

- Eight metrics, pure functions of the Population's AI telemetry, event times only (§19): `ai_assisted_pr_pct` (merged PRs with ≥1 valid DIRECT/STRONG contribution / merged PRs × 100, anchored merged_at), `ai_interaction_volume` / `ai_run_volume` (counts anchored started_at), `ai_task_completion` / `human_intervention_rate` / `retry_rate` (shares over finished tasks — completed_at in window, status completed|failed; pending tasks have no event time and are excluded), `ai_cost` (sum cost_usd over runs started in window), `cost_per_completed_task` (cost / tasks completed in window).
- §26 counting is pinned (`TestAIAssistedPRPctAttributionFiltering`): only valid DIRECT/STRONG telemetry claims count — INFERRED, UNKNOWN, bare DIRECT (no source), wrong-repo and orphan contributions never count; a PR with only those shows 0.0, with PR-only evidence.
- Honest absence: a window with no objects yields NO observation (source silence is unknown, not zero — `none()` helper); `cost_per_completed_task` with no runs or no completed tasks is absence, not zero. `ai_assisted_pr_pct` differs: merged PRs with zero qualifying contributions is a real 0.0 — that design keeps the §23 fixture's factor list at 2 (flat 0.0→0.0 is not a candidate).
- §18 edges pinned per metric: exact values, empty population, missing event times, duplicate external IDs, and a timezone-boundary case (an interaction at 2026-09-30 23:30 UTC is September in New York, October in Tokyo — in-window either way, instants not wall clocks).
- Investigation wiring: `RelAIContributes` (CodeContribution → PullRequest, inbound walk in `Population`) brings contributions into the org-scoped graph; `ai_assisted_pr_pct` is a candidate factor — `TestCycleTimeChangeAIFactor` pins the statement gain ("AI-assisted PR percentage changed from 0.0 to 100.0 %." — the From==0 rule renders a percentage without a %-of-%), `TestCycleTimeChangeAIFlatExcluded` pins that a flat share is not listed.
- PR size (roadmap Layer A, item 34): `pr_size` = mean additions+deletions over merged PRs anchored at merged_at, one observation per window with every counted PR cited — `TestPRSizeExact` (two sized PRs → 90.0, both cited; a merged no-size PR and an unmerged PR never count), `TestPRSizeAbsence` (empty/out-of-window/unmerged/no-size → nil, silence not zero). The connector maps the PR list's additions/deletions (`TestToPullRequestMapsDiffSize`); the "PR size not evaluated" limitation is gone — the metric's own limitations carry the caveat (pre-ingestion PRs show no size: excluded, not zero). Board deliberately unchanged — the roadmap's FLOW section keeps three metrics; pr_size joins as an investigation factor only. GET /metrics now returns 7 definitions (the API test tracks the candidate table).
- Review cycles (roadmap Layer A, item 35): `review_cycles` = mean count of CHANGES_REQUESTED reviews per merged, reviewed PR, anchored at merged_at — no new ingestion needed, the Review objects already carry state. `TestReviewCyclesExact` (2+1+0 cycles over 3 reviewed PRs → 1.0, evidence cites the 3 PRs + 3 changes_requested reviews), `TestReviewCyclesAbsence` (a merged PR with no reviews is silence, never zero; out-of-window/unmerged never count). Real-zero rule mirrors ai_assisted: reviewed PRs with no change requests are genuine 0.0s — which is why the §23 fixture's appeared leg now renders "Review cycles appeared in Month B at 0.0 cycles."
- Deferred (documented): AI-related rework (no rework signal in any source); run/task/interaction metrics as investigation factors (telemetry objects carry no org/repo edge — a connector that links them joins the candidates then; the metrics themselves are ready and tested).

## Product board (§28)

Suite: `internal/intelligence` (`board_test.go`; Memory + live AikoqlStore).

- GET /board?scope=<extID>&start=<RFC3339>&end=<RFC3339> returns the doc's five sections in order: Engineering Flow, Quality, Reliability, AI Development, Evidence Coverage (`TestBoardSections` pins order + titles).
- Engineering Flow = window means of cycle_time / review_latency / throughput; AI Development = ai_assisted_pr_pct. Every item carries `metric/label/value/unit/epistemic_state/evidence` — the same calculated-evidence discipline as investigations.
- Honest absence: Quality ("no CI data — no workflow runs ingested yet") and Reliability ("no deployment or incident data") are notes — never fabricated numbers; a window with no data returns the same five sections with notes (`TestBoardEmptyWindow`); coverage with nothing to cover carries a note.
- Evidence Coverage = per epistemic state present, the count of distinct objects backing it (§28 "how strong is the evidence" as object counts, not a confidence score).
- No individual ranking (`TestBoardNoIndividualRanking`) — by construction: items cite only PR/review/contribution objects. Check structurally (no `Engineer`/`SourceIdentity` evidence types, no identity keys) — the word "Engineering" trips a substring check on the doc's own title.
- Unknown scope → 400 (a navigation typo; POST /investigations keeps 500 — there scope comes from a request body). "What changed / why" stays the investigations endpoint's job.

## CI workflow runs + ci_pass_rate (item 32, post-contract)

Suite: `internal/github` (`normalize_test.go`, `sync_test.go`), `internal/metrics` (`ci_test.go`), `internal/intelligence` (`population_test.go`, `board_test.go`; Memory + live AikoqlStore).

- The GitHub sync now fetches workflow runs per repo (`Actions.ListRepositoryWorkflowRuns` — the v92 name; the real API wraps them in a `workflow_runs` envelope, not a bare array) as the long-reserved `ontology.Build` objects, then CI-watermarks, upserts, and edges them: `CONTAINS_BUILD` (repo → run) and `HAS_BUILD` (PR → run, resolved from the run's own `pull_requests` array — a PR the sync has never seen yields no edge, the run still stores). HAS_BUILD deliberately replaces the first draft's `PASSED` (roadmap §3.1): a workflow associated with a PR can still fail, so the edge never claims an outcome — a failed run linked to a PR is still HAS_BUILD, and the conclusion lives on the Build.
- `TestToBuild` — the list endpoint carries no completed_at: a completed run's updated_at stands for completion (GitHub stops touching the record once a run finishes) and an in-progress run's CompletedAt stays zero so its advancing updated_at re-passes the watermark and flips when it finishes.
- `TestSyncCI` — run 1: counts `{Build: {New: 4}}` (success, failure, in-progress, cancelled — the outcome-vocabulary AC needs every conclusion), repo→CONTAINS_BUILD outbound reaches all 4, PR#3→HAS_BUILD outbound reaches the success build; run 2 after `FinishInProgressBuild`: `{Updated: 1, Skipped: 3}` (the finished run re-normalizes; the settled ones watermark-skip). Stale `PASSED` edges in a pre-rename db are inert — nothing reads them.
- `ci_pass_rate` (new candidate, so the board's Quality section and GET /metrics both grew): success / verdict runs × 100 where a verdict is success|failure|timed_out — cancelled/skipped runs never tested the code and a conclusion-less run is unclassifiable, so neither counts (`TestCIPassRateExact` — 2 success + 1 failure + 1 timed_out + 1 cancelled → 50.0, the 4 verdict builds cited; `TestCIPassRateExcludesNoVerdict`; `TestCIPassRateAbsence` — empty/out-of-window/only-cancelled → nil, silence is not zero). Anchored at completed_at (§19), CALCULATED with every counted Build in evidence.
- Board: Quality carries ci_pass_rate when builds exist (`TestBoardQualityWithCI` — 3 success + 1 failure → 75.0 %) and the honest note otherwise; the population walk traverses CONTAINS_BUILD outbound (pinned in `TestPopulationFromScope`).

## Authoritative issue links (item 39, post-contract — roadmap Milestone B)

Suite: `internal/github` (`client.go`, `sync_test.go`).

- Issue links now come from GitHub's authoritative data — GraphQL `closingIssuesReferences` per changed PR (posted through go-github's raw request path; v92 has no GraphQL method, and the raw path keeps the rate-limit retry). The body-keyword regex is deleted: GitHub resolves closing links from timeline events, keywords and manual edits, so the regex only ever saw a subset. A GraphQL-level error (auth scope, malformed query) fails the run — links are never silently dropped.
- `TestPRAuthoritativeIssueLinks` — PR#9's body has no close keyword but GitHub links it to issue #1; the edge exists. `TestPRToIssueLinks` keeps its two legs with the authoritative data: duplicate nodes collapse to one IMPLEMENTS edge, a nonexistent issue (404) skips without failing. The on-demand issue refetch stays — it resolves numbers outside the incremental window, whatever the link source.
- Fixture: `/graphql` answers `closingIssuesReferences` from `prCloses` per PR (PR#3 closes #1 — the same edge the regex produced, so every existing count pin holds).
- Board and GET /metrics stay unchanged (7 definitions).

## Deployments + services (item 40, post-contract — roadmap Milestone C)

Suite: `internal/github` (`normalize_test.go`, `sync_test.go`), `internal/ontology` (`rels.go`).

- The sync now ingests each repo's deployments (`repos/{repo}/deployments`) as `ontology.Deployment` (id, sha, ref, environment, description, times — no state: the list has no terminal state and nothing consumes it yet) and derives `ontology.Service` from the environment name scoped to the repo (`github.com:service:<owner>/<repo>:<env>` — the GitHub-only world's service signal; env names are [A-Za-z0-9_-]+, so the id scheme stays collision-free).
- AFFECTS (Deployment → Service) links each deployment to its service; PRODUCED (Build → Deployment) links it to every build whose head commit is the deployed sha — sha association, never causation (Actions-created deployments carry no run id, so the sha is the strongest link GitHub exposes).
- Every listed run records into `buildRunIDsBySHA` BEFORE the watermark gate, so a deployment at a sha whose build predates the window still links through the store's deterministic `BuildExternalID` lookup; a build older than the store's window yields no edge. The maps reset per repo at the top of syncCI — a sha is content-addressed, so cross-repo runs at the same sha must not leak edges. Deployments themselves are not watermark-gated (one short list call; idempotent upserts make re-listing free).
- `TestToDeployment` pins the mapping (incl. toService); `TestSyncDeployments` pins the two-edge graph (dep300 → build 200 over PRODUCED, → production over AFFECTS), the no-build-sha silence (dep301 at DeadSha leaves no PRODUCED edge), and the run-2 leg: dep302 at ShaB links through the store to build 200 (watermark-skipped in run 2), zero extra API calls. The Milestone C exit graph Build → Deployment → Service reconstructs.
- Board and GET /metrics stay unchanged (7 definitions).

## AgentTask → CodeContribution link (item 42, post-contract — roadmap Milestone D)

Suite: `internal/claudecode` (`sync_test.go`), `internal/ontology` (`rels.go`).

- The claudecode connector now links each code-editing task to the contribution its tool_use produced — AUTHORED, whose doc covers both Engineer → Commit|PR and AgentTask → CodeContribution (1:1; a non-code task produces nothing). The join needs no new parsing: the contribution's id embeds the producing task's tool_use id (sessionID:tool_use — both carry no colons).
- The task link does not depend on the PR link: an unlinked contribution (PR absent from the store) still keeps its AUTHORED edge.
- `TestSyncAgentTaskLinks` — toolu_1's outbound AUTHORED reaches exactly its contribution, which reaches the seeded PR (the AgentTask → CodeContribution → PR chain); toolu_2 (Bash, no code) has no AUTHORED outbound. TestSyncWritesTelemetry's relationships pin grew 2 → 4.
- Milestone D's exit graph AgentTask → CodeContribution → PR → Build reconstructs: the task leg here, PR → Build by TestSyncCI (HAS_BUILD). Not built (absent from the transcript or the exit graph): Engineer → Interaction (no human identity in transcripts), Interaction → AgentRun (pairing unrecorded), Agent/Model/AgentOutcome entities (model is an attribute on every object already).

## AI workflow metrics on the board (item 43, post-contract — roadmap Milestone E)

Suite: `internal/intelligence` (`board.go`, `board_test.go`, `population.go`, `population_test.go`), `internal/claudecode` (`sync.go`, `sync_test.go`), `internal/ontology` (`rels.go`).

- The AI Development section runs the §27 metric set (Milestone E list): ai_assisted_pr_pct, ai_interaction_volume, ai_run_volume, ai_task_completion, human_intervention_rate, retry_rate, ai_cost, cost_per_completed_task — a workflow dimension, not a single adoption counter. Each renders only when its objects exist in the population (absence is an honest gap, never a zero).
- Reach: the claudecode connector now links each session to its children — CONTAINS_INTERACTION / CONTAINS_RUN / CONTAINS_TASK (CodingSession → child, 1:N) — and the population walk extends to contribution → AUTHORED inbound → task → CONTAINS_TASK inbound → session → children. Edges only: a contribution without a task link leaves its session's telemetry out of the population.
- `TestSyncSessionContainment` — the session's outbound containment reaches exactly its 1 interaction / 3 runs / 3 tasks from the canonical fixture. TestSyncWritesTelemetry's relationships pin grew 4 → 11.
- `TestPopulationCollectsTelemetry` — through one PR-scoped contribution the walk collects 1 interaction, 1 run, 2 tasks (the session's full task set, not just the AUTHORED one).
- `TestBoardAIWorkflowMetrics` — with telemetry linked, the AI section lists all eight items in order with pinned values (100.0% assisted, 2 interactions, 2 runs, 75.0% completion, 25.0% intervention, 50.0% retry, 3.0 USD cost, 1.0 USD/task), CALCULATED with evidence each, no note. The §23 fixture still renders only ai_assisted_pr_pct (its zero is real — merged PRs, no contributions) and the empty-window board still notes "no AI telemetry linked to this scope" (TestBoardSections, TestBoardEmptyWindow).
- Deliberately deferred: "AI-assisted vs non-AI-attributed comparisons" lands with Milestone F's population comparison (comparisons live there, §22); the investigation's candidate factors stay unchanged (the seven new metrics join when a question needs them).

## Releases (item 41, post-contract — roadmap Milestone C)

Suite: `internal/github` (`normalize_test.go`, `sync_test.go`), `internal/ontology` (`rels.go`).

- The sync now ingests each repo's releases (`repos/{repo}/releases`) as `ontology.Release` (id, tag_name, name, target_commitish, prerelease, created/published_at) hanging off the repo with CONTAINS_RELEASE (Repository → Release) — the roadmap's target graph has no further edge contract for releases.
- `target_commitish` stays a property, never an edge: it is a branch or tag name as often as a commit sha (`TestToRelease` pins the mapping with a branch-name target).
- Like deployments, releases are not watermark-gated — one short list call, idempotent upserts make re-listing free.
- `TestSyncReleases` — two releases (stable + prerelease; the prerelease flag stores verbatim) and the repo reaches both over CONTAINS_RELEASE. Milestone C's full exit graph PR → Build → Deployment → Service reconstructs (HAS_BUILD → PRODUCED → AFFECTS).
- Board and GET /metrics stay unchanged (7 definitions).

## Commit ↔ PR links (item 38, post-contract — roadmap Milestone B)

Suite: `internal/github` (`sync_test.go`), `internal/ontology` (`rels.go`).

- The PR's own commit list (`pulls/{n}/commits`, one paginated call per changed PR) now ingests PR-branch commits the default-branch walk never sees and links each with PART_OF (Commit → PullRequest) — the Milestone B graph reconstructs Engineer → Commit → PR.
- The per-commit handling (upsert, merge-lookup record, author edge) now lives in one shared `syncCommit`, used by both the default-branch walk and the PR path.
- `TestSyncPRCommits` — PR#3's branch commit PrSha stores, reaches the PR over PART_OF, and its author resolves to the same noreply ann as ShaA (one engineer, no duplicate). The fixture serves `/pulls/{n}/commits` from `prCommits`; absent numbers answer the empty list.
- Count pins grew: TestSyncRun1Full Commit {New: 3} and 14 relationships; TestSyncCI 19; TestSyncSkipsEmptyRepo Commit {New: 3} (widgets: ShaA, ShaB, PrSha — the empty repo still contributes none).
- Board and GET /metrics stay unchanged (7 definitions).

## Review lifecycle (item 37, post-contract — roadmap Milestone B)

Suite: `internal/github` (`normalize_test.go`, `sync_test.go`), `internal/ontology` (`rels.go`).

- The five review-lifecycle states are now distinguishable in the graph: the submitted states live on `Review.State` verbatim (APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED — `TestToReviewStatesVerbatim` pins the pass-through), and "requested" is NOT a Review state — GitHub keeps no request history or timestamps, so the current request set lives on `PullRequest.RequestedReviewers` and the `REQUESTED_REVIEW` edge (PR → Engineer, the request snapshot at sync time; DISMISSED marks the end of a request).
- `TestReviewLifecycleStates` — PR#3's requested-reviewer bob is reached over REQUESTED_REVIEW (`github.com:user:bob`), and the fixture's second review (`DISMISSED`) stores with state verbatim.
- Identity honesty (`TestSyncRun1Full` pins): the requested reviewer resolves by login, the commit author by email — GitHub hides emails in PR data and the resolver never silently merges, so request-bob and commit-bob are two Engineers (`{New: 3}`). Reviews `{New: 2}`, base relationships 14 (TestSyncCI 19).
- Board and GET /metrics stay unchanged (7 definitions).

## Richer PR metadata (item 36, post-contract — roadmap Milestone B)

Suite: `internal/github` (`normalize_test.go`, `sync_test.go`).

- The PR list endpoint leaves `merge_method` and `requested_reviewers` unpopulated, so the sync fetches the single-PR GET per changed PR (watermark-gated — run 2 of `TestSyncRun2Delta` skips PR#3 before the GET, keeping the delta counts pinned) and stores the richer record on PullRequest: labels, draft, merge method, requested-reviewers snapshot. A PR deleted between the list and the GET is skipped, not an error.
- go-github's `PullRequest` struct has no merge_method field (the REST body carries it; the SDK struct does not), so the GET decodes the raw body as `pullDetail` (embedded `gh.PullRequest` + the field) and `toPullRequestDetail` maps it. The fixture serves the wire shape as a raw map (`prDetails`); absent numbers fall back to the list entry.
- `TestToPullRequestMapsRicherMetadata` — labels/draft/merge method/requested reviewers all mapped; `TestSyncPRRichMetadata` — PR#3's stored properties carry them (`merge_method: squash`, `draft: false`, `labels: [enhancement]`, `requested_reviewers: [bob]`).
- Nothing consumes these yet — richer metadata for future investigations; the board and GET /metrics stay unchanged (7 definitions).

## Dashboard + board caching (item 31, post-contract)

Suite: `internal/intelligence` (`board_ui_test.go`, `cache.go`; Memory store + httptest).

- `GET /{$}` serves the embedded dashboard (`TestDashboardServed` — 200 text/html, contains "Engineering Intelligence"); the exact-root pattern keeps unknown paths 404 (the test also pins `/nope`).
- `boardCache` memoizes board responses per `scope|start|end` (TTL 60s, sha256 ETag, injectable `now` clock; errors are never cached — put only on success). Safe because the board is deterministic per key and one server per db dir means no writer exists while `ei serve` holds the db.
- `TestBoardCacheSecondRequestSkipsStore` — a `countingStore` wrapper proves the second identical request performs zero store reads, returns the identical body and ETag.
- `TestBoardCacheETagRevalidates` — `If-None-Match` with the cached ETag → 304, empty body.
- `TestBoardCacheKeyedByWindowAndScope` — a different window/scope misses (store reads increase, different ETag).
- `TestBoardCacheExpires` — clock injection past the TTL recomputes.
- `TestBoardCacheNeverCachesErrors` — a failing store returns 500 and caches nothing; once unfailed, the next request recomputes (reads increase) and serves 200.
- Client: the page sends `If-None-Match` manually (fetch never auto-revalidates), keeps the last good board in localStorage for instant paint, and a 304 keeps the painted data — revalidation, not staleness.

## Evidence-backed engineering agent (§29)

Suite: `internal/intelligence` (`agent_test.go`; Memory + live AikoqlStore).

- The agent is `Ask(ctx, store, Query{Text, Scope, From, To}) → Answer{Question, Class, Statement, Evidence, Limitations}` — the §29 flow mapped onto the deterministic pieces: question classification → Population (retrieval + traversal) → CycleTimeChange (metrics + evidence + reasoning) → answer with evidence references. The agent's "reasoning" IS the deterministic engine: no LLM step exists to drift.
- Classification matches normalized phrasing (lowercase; change words or "why…" + "cycle time" — `TestAskClassification` pins variants and refusals). The comparison window is derived — the immediately preceding period of equal length — the natural reading of "change".
- Never fabricate (`TestAskCycleTimeChange` + `TestAskUnknownScope`): every cited evidence ObjectID resolves back through the store; unknown questions are honest refusals listing the supported question (no evidence, no invented answer); an unknown scope is an error, not an answer.
- POST /ask serves the same contract over HTTP (400 on malformed body / empty question / bad or inverted times / unknown scope — scope only matters when the engine runs; a refusal needs no scope).
- Deferred: an LLM reasoning step and any conversational UI join when a real natural-language client exists; the classification table grows one entry per supported question.

## AI telemetry source — Claude Code connector (§25–26, §38 MVP path)

Suite: `internal/claudecode` (`sync_test.go`; Memory + live AikoqlStore). The §38 MVP's "one AI-development telemetry source" — a transcript is a log the tool itself wrote, and every extraction is backed by a line in it.

- One CodingSession per .jsonl (sessionId, model, first/last timestamps); an Interaction per user prompt; an AgentRun per tool-calling assistant message (EndedAt = its last tool result, Status failed on any is_error); an AgentTask per tool_use (pending while no result has landed; `permission denied` in an is_error result sets human_intervention — a telemetry fact, not a judgment); a CodeContribution per Edit/Write/MultiEdit/NotebookEdit whose repository derives from the message cwd's .git/config origin remote (test seam `RepoOf`; the real reader is unit-tested on https + ssh URL forms).
- PR link comes from telemetry only: the PR URL the session's own tool output recorded (`gh pr create` result text) — first URL per repo wins (transcripts are chronological). Attribution is DIRECT with source + the tool_use record as evidence (`Validate()` passes).
- Honest gaps, never filled (`TestParseHonestGaps`, `TestSyncUnlinkedContribution`): no PR URL → PRNumber 0 + unlinked; no repository identity → unattributed, no object; PR absent from the store → contribution stored but unlinked (run the GitHub sync first); malformed/truncated lines → skipped and counted as unparsed. Rerun idempotent (re-seen identical counts nothing — the github pre-check pattern).
- Fixture transcripts mirror the real format probed from live transcripts (type/sessionId/timestamp/cwd/uuid/message/content blocks), including fractional-second timestamps (RFC3339Nano).
- Ponytail notes: CostUSD stays 0 (this transcript version records no cost); no .git walk-up (sessions run at the repo root); no checkpoint (transcript dirs are small — add when a scan becomes slow); Agent/Model/AgentOutcome objects not emitted (no metric or question reads them yet).

## AIKOQL wire contract (probe-verified, encoded in internal/knowledge/aikoql.go)

The server is the source of truth; these facts were measured against the live binary, not assumed:

- create: `remember{type_name, properties, idempotency_key=ExternalID}` → version 1; identical re-remember = no-op.
- update: keyed re-remember with changed properties is silently IGNORED; content updates require `remember{koid, expected_version}` (stale → VERSION_CONFLICT).
- the server's idempotency index is global and type-blind: key reuse under another type_name silently returns the existing object. AikoqlStore keeps its own `ExternalIDIndex` objects and detects ErrTypeConflict client-side.
- no untyped KOQL exists (`MATCH *` and unknown entity names are COMPILE_ERROR) → GetByExternalID resolves via `ExternalIDIndex`.
- relate: duplicate = no-op, bumps FROM version once.
- traverse: UNDIRECTED, ignores the direction argument; hits carry a per-hit `direction` label relative to the query node → directed traversal is a client-side BFS of depth-1 traverses filtered by label (cycle topology verified).
- batch: `{operations: [{op: "remember"|"relate"|"forget", ...tool args}]}` → `{results: [{op, ok, result|error}], count}`. Per-op failures do NOT stop the batch, and `$N.koid` resolves to the Nth koid-returning op's result (shifts when any op fails) — the adapter never uses `$N` references; created objects' index entries go in a second batch call with real koids (TestAikoqlBatchUpsertLive).
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
  - upsert (create path) ≈ **8.5ms/op**
  - get by external ID ≈ **2.1ms/op**
  - depth-3 directed traverse, 20-node chain ≈ **4.9ms/op**
  - (2026-09-29, commit 29) the external-ID lookup measured above went through a `MATCH ExternalIDIndex WHERE external_id == X` — fine on a fresh bench DB, but the dogfood ingest (~150K index objects) exposed it as a full type scan (no property index): **~600ms per lookup, O(n) and growing**. The adapter now resolves via the server's O(1) `get_by_idem` tool; the measured bench numbers are superseded, not re-run.
- [x] 100K+ objects (dogfood, 2026-09-29): the Claude transcript ingest over `.ei/dogfood-db` crawled at ~3.3 tool calls/s — one get + one full-scan aikoql per object (~595ms per pair, measured CPU/call matches). Fixed with `get_by_idem` (Mnemosyne commit 8937a52 + EI commit 29): per-object reads drop to two O(1) calls. The remaining unmeasured scale item: 1M objects.
- [ ] connector scale: 10 / 100 / 1000 repositories; 10K / 100K / 1M issues

Do not claim scalability until measured. Currently measured: per-op latency only.

## Observability (§33)

- [x] one JSON line per ingestion run on stderr (`run_id`, `source`, `tenant_id` (default), `started_at`/`ended_at`, `objects_seen`/`objects_created`/`objects_updated`/`objects_skipped`, `relationships_written`, `errors`, `checkpoint`) — `cmd/ei` `runLog`/`logRun`; `TestRunLogShape` pins the key set (required keys present, no `errors` on a clean run). `connector_version` is stamped per-object in provenance (`github-connector/v0`, `jira-connector/v0`, `ei-ai/v0`) rather than repeated in the run line (ponytail: export the consts if a log consumer needs it).
- [x] never log access tokens, secrets, private AI prompts, unnecessary PII — by construction: `runLog` has no token/secret/PII field, and `TestRunLogShape` fails if a sensitive key (`token`/`secret`/`password`/`email`/`prompt`) is ever added. `errors` carries connector error text only (tokens never appear in URLs/error paths).

## §38 MVP questions — answerable through the built API

1. What changed in engineering performance? → `POST /ask` + `POST /investigations` (cycle time change)
2. How did flow, quality and reliability change? → `GET /board` (flow metrics + ci_pass_rate; reliability honest note until a deployment/incident source exists)
3. How is AI-assisted development changing the workflow? → `ai_assisted_pr_pct` on the board + as an investigation factor
4. What evidence supports the observed change? → every observation/factor carries an `evidence` array citing object IDs (§20)
5. What is observed versus calculated versus inferred? → `epistemic_state` per observation + board Evidence Coverage section
6. What can the system not conclude? → `limitations` + the association-not-causation statement (§23)

## CLI wiring (§38 runnable MVP)

- `ei` dispatches `sync github` / `sync jira` / `ingest claude` / `serve`; anything else → usage error, exit 1.
- `AIKOQL_MCP_BIN` names the server binary; `--db` its database dir. `openStore` dials over stdio with a 60s timeout and closes on every path (including Initialize failure).
- **Flag validation happens before any store spawn** — pinned by `TestRunRequiredFlagsBeforeStore` (missing-flag cases run with `AIKOQL_MCP_BIN` unset; a store attempt would surface a different error and fail the test).
- `ei serve --db DIR [--addr :8080]` = `http.ListenAndServe` over `intelligence.NewAPI` (§24/§28/§29 routes); the store lives for the process lifetime.
- Summaries print each connector's SyncResult counts; re-seen-identical rows are skipped (`Count{}`), honest gaps (unlinked/unattributed/unparsed) always printed when nonzero.
- Live proof: `TestOpenStoreLive` (upsert + GetByExternalID round trip through the CLI's own opener) + a full `ei ingest claude` smoke against a fresh db (exit 0, counts printed; contribution without a repo .git/config → `unattributed 1`, never fabricated).
- `.env` in the working directory (gitignored) supplies env vars at startup: stdlib-only `KEY=value` parser, process env never overridden, missing file is no error — pinned by `TestLoadDotEnv` (comments/blanks/quotes/`#`-in-value/preexisting env) and `TestLoadDotEnvMissingFile`.
- Jira credentials fall back to `JIRA_EMAIL`/`JIRA_TOKEN` when `--email`/`--token` are empty; flags win when given — pinned by `TestRunJiraEnvCredentials` (env creds pass validation, fail only at the store step).

## Definition of Done per feature (§37)

All applicable rows true before "done":

- **Code**: implementation complete, interfaces documented, no duplicated abstraction, no domain leakage into AIKOQL.
- **TDD**: failing test existed before implementation; unit + integration + acceptance pass.
- **Reliability**: failure behavior tested; retry tested where relevant; context cancellation tested; idempotency tested where relevant.
- **Data**: provenance preserved; temporal semantics defined; identity semantics defined; evidence lineage preserved.
- **Product**: acceptance criterion mapped; limitations documented; user-visible behavior defined.
- **Operations**: logs/metrics where needed; checkpoint behavior defined; secrets protected.

## Investigation question dispatch (item 44, post-contract — roadmap Milestone F)

- The engine is `MetricChange(pop, a, b, primaryName, question)` — the shared two-window comparison behind every supported question; `CycleTimeChange` delegates with `"cycle_time"`. The primary candidate is excluded from its own factor loop, missing data says "Not enough <name> data in <window> to investigate." (the first window lacking it), unchanged says "<Label> did not change (...).", and `concluded(question, statement)` stamps the question on every output.
- Questions (`questionPrimary`): Why did cycle time change? → cycle_time; Why did CI quality change? → ci_pass_rate; What changed after AI adoption increased? → ai_assisted_pr_pct; What is associated with increased review latency? → review_latency.
- Classification (agent.go): a topic phrase ("cycle time" | "ci quality" | "ai adoption" | "review latency") plus a change word (or why-prefix) routes to the matching engine class; anything else is the honest refusal, which now lists all four supported questions ("I can only answer: ...; ..."). `askChange` is the single ask path (previous/current derived windows → MetricChange).
- POST /investigations dispatches on the question field (`questionPrimary`); omitted question keeps the §23 default (cycle time), unknown → 400 "unsupported question".
- Tests: `TestMetricChangeCIQuality` pins the CI statement (75.0 → 25.0 %, factors cycle time + review latency + throughput, primary never a factor) and `TestMetricChangeMissingData` the named-metric honesty; `TestAskCIDispatch` (classification + dispatch + evidence resolution over seedAgentCIWorld); `TestAskClassification` now covers all four classes; `TestInvestigateQuestionDispatch` pins the API's question → primary dispatch. All §23 pins hold unchanged — delegation is behavior-preserving.

## Population comparison (item 45, post-contract — roadmap Milestone F)

- `ComparePopulations(pop, win) (assisted, unattributed int, rows []PopulationComparison)`: one window's merged PRs partition by AI attribution (CodeContribution names repository + PR number), reviews follow their PRs, and the PR-scoped metrics (cycle_time, review_latency, pr_size, review_cycles) compare between the sub-populations. A nil side is an honest gap (a metric not computable on that side), a metric absent on both sides drops the row entirely.
- POST /comparisons {scope,start,end} returns populations (id/label/merged_prs) + metric rows (metric/label/unit + `ai_assisted`/`unattributed` ComparisonValues with CALCULATED state and evidence); bad window or unknown scope → 400.
- Honest-partition gotcha: a side's PRs carrying no reviews at all makes review_latency/review_cycles absent there — silence, never a fake zero (item 35's contract, now pinned cross-sectionally).
- Tests: `TestComparePopulationsPartitionsAI`, `TestComparePopulationsUnattributedAbsent`, `TestAPIComparisonsEndpoint`.
