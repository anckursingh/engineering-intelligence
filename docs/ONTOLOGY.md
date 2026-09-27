# Canonical Engineering Ontology

## Design rule

The ontology belongs to Engineering Intelligence, not AIKOQL.

AIKOQL stores these objects without understanding their domain semantics.

## Organization

- Organization
- BusinessUnit
- Department
- Team
- Engineer

## Product

- Product
- Service
- Component
- Repository
- Environment

## Work

- Objective
- Initiative
- Program
- Epic
- Issue
- Task

## Software delivery

- Commit
- PullRequest
- Review
- Build
- Test
- Deployment
- Release

## Quality and reliability

- Defect
- CodeSmell
- Vulnerability
- Incident
- Alert
- SLO
- ChangeFailure

## AI development

- AIAgent
- AIModel
- AIInteraction
- AICodingSession
- AICodeContribution
- AgentRun
- AgentTask
- AgentOutcome

## Analytics

- MetricDefinition
- MetricObservation
- MetricWindow
- Dimension
- Insight
- Evidence
- Attribution

## Key relationship categories

### Ownership

Engineer -> Team
Team -> Product
Engineer -> Repository

### Delivery

Issue -> PullRequest
PullRequest -> Commit
PullRequest -> Review
PullRequest -> Build
Build -> Deployment
Deployment -> Service

### AI

Engineer -> AIInteraction
AIInteraction -> AIAgent
AIInteraction -> Commit
AIInteraction -> PullRequest
AgentRun -> AgentTask
AgentTask -> PullRequest

### Reliability

Deployment -> Incident
Incident -> Service
ChangeFailure -> Deployment

## Temporal requirements

Important entities must support:

- observed_at
- valid_from
- valid_to
- source_updated_at
- ingestion_time

The product must distinguish source time from ingestion time.

## Provenance

Every source-derived object should retain:

- source system
- source object ID
- source URL where permitted
- connector version
- ingestion run
- observed timestamp
- transformation version

## Identity

Identity resolution maps source identities to canonical Engineer identities.

Example:

```text
GitHub: ankur@example.com
Jira: ankur.k
AI tool: akumar
HR: EMP-123
        |
        v
Engineer: ENG-456
```

Identity decisions must be auditable.
