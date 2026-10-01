# Design Partner Validation Plan

## Objective

Find out whether evidence-backed engineering investigations help an engineering
leader make a better decision than their existing dashboards and manual joins.
The goal is to validate a narrow workflow, not to validate the whole ontology or
every connector.

## Participant and data boundary

Work with one engineering organization that can provide an approved, limited
evaluation dataset. Agree up front on:

- participating teams and repositories;
- data sources and time period;
- treatment of developer identities and AI transcripts;
- who can access the local POC and results;
- retention and deletion date;
- whether findings may be used outside the evaluation.

Avoid collecting private AI prompts unless the participant explicitly approves
that source and use. The product should prefer aggregate workflow evidence where
it is sufficient.

## Three questions to validate

Use questions supported by the current API:

1. **Why did review latency change?** Compare equal-length periods and inspect
   the primary measure, candidate factors, evidence, and limitations.
2. **How do evidence-qualified AI-attributed PRs compare with PRs without
   positive AI evidence?** Use `POST /comparisons` for cycle time, review
   latency, PR size, and review cycles where data exists. Only valid DIRECT
   or STRONG attribution counts as positive AI evidence. The other group may
   include unknown or unobserved AI activity; it is not a human-authored cohort.
   Treat the result as an association, not a causal estimate.
3. **Where are agent tasks failing?** Use `POST /ask` to locate failed,
   completed tasks by session and inspect the underlying task evidence.

These questions cover workflow change, AI attribution, and agent outcomes. If
the participant's most important question is unsupported, record it rather than
stretching the engine's answer to fit.

## Session protocol

1. Agree on the team, sources, and time windows before showing results.
2. Ask the participant to state what they already believe changed and why.
3. Run the syncs and note created, updated, skipped, unlinked, unattributed, and
   unparsed counts.
4. Run the three questions and save the raw API responses.
5. Ask the participant to inspect evidence references and challenge each claim.
6. Record corrections, missing links, ambiguous identity decisions, and gaps in
   metric definitions.
7. Ask what decision, if any, they would make from the result and what extra
   evidence they would need.
8. Follow up after the team has had time to act; record whether the investigation
   led to a decision or remained interesting but unused.

Do not present a correlation as causal impact. In particular, AI-attributed and
no-positive-AI-evidence PRs may differ in work type, team, size, telemetry
coverage, or selection into usage.

## Evidence to capture

For each investigation, keep:

- question, scope, time windows, and source set;
- raw response including evidence IDs, epistemic state, and limitations;
- participant's corrections and trust rating with the reasons;
- evidence coverage issues and identity/attribution uncertainty;
- resulting action, owner, and follow-up outcome if one exists;
- setup effort and time from data access to a checked answer.

Keep notes at team/system level. Do not create individual productivity rankings.

## Exit criteria

The POC earns a next product iteration when all are true:

- At least three investigations run on the agreed dataset without manual data
  repair during the session.
- The participant can follow at least one material conclusion to source records
  and agrees that its limitations are stated honestly.
- The participant identifies a concrete decision or investigation the result
  supports.
- Any incorrect joins or attribution gaps are recorded with a reproducible
  failure example and a proposed fix.
- Setup and recurring data access are feasible for the participant.

Failure is useful: if leaders want a different question, if evidence cannot be
trusted, or if the result does not affect a decision, change the wedge before
adding connectors or broadening the agent.

## Developer follow-up loop

After each evaluation, turn observations into a prioritized issue list:

1. Correctness defects in source mapping, identity, or metric calculation.
2. Missing provenance or unexplained attribution.
3. Unsupported questions that block a demonstrated decision.
4. Setup friction and ingestion reliability.
5. UI needs that recur across participants.

Implement only the smallest change that addresses repeated, evidenced friction.
Preserve a fixture or acceptance case for every discovered data-quality failure.

### First local dataset finding

The first populated-dataset investigation showed review-latency values under an
hour rounded to `0.0 days` in the explanation even though the structured metric
and board held non-zero values. The investigation renderer now expresses
sub-hour durations in minutes and sub-day durations in hours; a regression test
pins the short-duration case. This is a presentation correction: the underlying
metric values and evidence lineage were already present.

The same board showed AI cost as `$0` for transcript telemetry that does not
report cost. Cost now has an explicit reported/unknown state, and aggregate cost
is omitted unless every in-window run has reported cost. The board calls out
that coverage gap instead of presenting unknown spend as free usage. Explicitly
reported zero remains a valid measured value; older records with non-zero cost
remain readable.
