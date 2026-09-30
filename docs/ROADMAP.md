# Roadmap

This roadmap reflects the current repository state as of 2026-09-30. The original
MVP build phases are implemented in the POC; the next gate is customer validation
and workload evidence, not connector breadth.

## Completed in the POC

- GitHub and Jira ingestion with incremental checkpoints.
- Claude Code transcript ingestion with explicit attribution gaps.
- Canonical engineering ontology, cross-source identity claims, provenance, and
  tenant-scoped storage wrapper.
- AIKOQL persistence through the public MCP tool surface and a Go adapter.
- Deterministic flow, CI quality, AI attribution, and agent task calculations.
- Evidence-referenced board, comparisons, investigations, and supported
  question API.
- CLI entry points for sync, ingest, and serving the API.

The repository's implementation plan and testing matrix remain the detailed
record of completed acceptance coverage and deferred technical work.

## Next: one design-partner evaluation

Use [the POC runbook](POC-RUNBOOK.md) and
[the validation plan](VALIDATION-PLAN.md) with one approved engineering dataset.

Exit only when three supported investigations have been independently checked
against their evidence, a participant identifies a decision the answers
support, and data-quality/setup issues are recorded as reproducible examples.

Do not add integrations before this gate unless the partner's needed evidence
cannot be obtained through the existing sources.

## Then: close evidence gaps

Prioritize fixes in this order:

1. Incorrect source mapping, identity links, or metric calculations found during
   evaluation.
2. Missing evidence coverage or unclear AI attribution.
3. Repeated unsupported questions tied to a real engineering decision.
4. Ingestion setup and failure recovery problems.
5. Interface needs repeated across evaluations.

Every trust-related defect should gain a reproducible fixture and an acceptance
case before broadening the feature surface.

## AIKOQL workload proof

The adapter already works against the public server contract in local and live
tests. Its remaining scale and query limits still need product-shaped evidence.
Measure representative reads/writes at increasing graph sizes; connector
workloads across 10, 100, and 1,000 repositories; and object counts from 10K to
1M where practical. Include traversal fan-out, ingestion retries, and recovery
from partial writes. Do not claim a supported scale ceiling until those results
are recorded.

## Deferred until evidence requires it

- Webhook/event-driven ingestion and out-of-order event reconciliation.
- Additional AI telemetry providers and broader connector coverage.
- A general-purpose natural-language query planner or LLM answer generation.
- Hosted multi-tenant service, authentication, SSO/SCIM, RBAC, audit, and data
  residency.
- Autonomous production changes, individual productivity scores, and employee
  ranking.

## Strategic boundary

Engineering Intelligence remains the customer product and owns domain meaning.
AIKOQL remains a general-purpose knowledge database. Invest in AIKOQL capabilities
when a measured Engineering Intelligence workload demonstrates the need.
