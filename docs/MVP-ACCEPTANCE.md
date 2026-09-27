# MVP Acceptance Criteria

## Automation

Runnable gate: `go test -run TestAcceptanceSlice1 ./test/acceptance/` — covers
AC-ING-001/004/005, AC-ID-002, AC-KG-001/004, AC-REL-001/002 against the offline
fixture world, each subtest named by its AC id. Live GitHub E2E (env-gated):
`EI_E2E_OWNER` + `EI_E2E_REPO`. AC-AQ-004 is proven by the aikoql container smoke
(ghcr.io/anckursingh/aikoql:0.1.19, MCP over TCP); it becomes a CI job with the
SDK adapter.

## 1. Ingestion

### AC-ING-001
GitHub data can be incrementally synchronized.

### AC-ING-002
Jira data can be incrementally synchronized.

### AC-ING-003
At least one AI-development source can be synchronized.

### AC-ING-004
A failed synchronization can resume from a checkpoint.

### AC-ING-005
Re-running the same source page does not create duplicate canonical entities.

## 2. Identity

### AC-ID-001
The system can map at least two source identities to one canonical Engineer.

### AC-ID-002
Every automatic identity decision has an explainable reason.

### AC-ID-003
Ambiguous identity mappings can be reviewed.

## 3. Knowledge

### AC-KG-001
A PR can be traversed to its author, repository, issue, reviews and builds.

### AC-KG-002
An AI interaction can be linked to its agent and resulting engineering activity when evidence exists.

### AC-KG-003
Deployment can be linked to the originating PR/commit.

### AC-KG-004
Source provenance is retained.

## 4. Metrics

### AC-MET-001
Cycle time is reproducible for a defined population and window.

### AC-MET-002
PR throughput is reproducible.

### AC-MET-003
Review latency is reproducible.

### AC-MET-004
Deployment frequency is reproducible.

### AC-MET-005
Change failure rate is reproducible when incident/deployment linkage exists.

### AC-MET-006
AI adoption is reproducible for the selected AI source.

### AC-MET-007
AI-assisted versus non-AI-assisted activity can be compared where evidence permits.

## 5. Intelligence

### AC-INT-001
A user can ask why a metric changed.

### AC-INT-002
The response identifies the comparison window and population.

### AC-INT-003
The response distinguishes observation from interpretation.

### AC-INT-004
The response provides evidence references.

### AC-INT-005
The response explicitly states important data limitations.

### AC-INT-006
The system does not claim causality from observational data without an appropriate design.

## 6. Dashboard

### AC-UI-001
Board view shows delivery, quality, reliability and AI-development dimensions.

### AC-UI-002
Every material metric can be drilled into.

### AC-UI-003
AI development metrics can be segmented by team, repository and development mode.

### AC-UI-004
No default individual productivity score exists.

## 7. AIKOQL boundary

### AC-AQ-001
Engineering Intelligence communicates through a supported public AIKOQL interface.

### AC-AQ-002
No Engineering Intelligence domain types are added to AIKOQL.

### AC-AQ-003
No Engineering Intelligence-specific logic exists inside AIKOQL.

### AC-AQ-004
The application can be tested against a standalone AIKOQL deployment.

## 8. Reliability

### AC-REL-001
Connector failure does not corrupt canonical knowledge.

### AC-REL-002
Ingestion operations are idempotent.

### AC-REL-003
Metric calculations are deterministic for the same data snapshot.

### AC-REL-004
All production insights can be reproduced from retained evidence.
