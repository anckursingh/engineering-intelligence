# Architecture

## 1. Architectural boundary

Engineering Intelligence is a separate product and repository.

AIKOQL is a database dependency.

```text
Sources
  |
  v
Go Connectors
  |
  v
Normalization
  |
  v
Identity Resolution
  |
  v
Canonical Engineering Ontology
  |
  v
AIKOQL
  |
  +--> Graph / relationships
  +--> Semantic retrieval
  +--> Events / CDC
  +--> Temporal knowledge
  +--> Provenance
  |
  v
Metric Engine
  |
  v
Reasoning / Intelligence
  |
  +--> API
  +--> Web application
  +--> Engineering Agent
```

## 2. Services

### API

Go HTTP API for:

- dashboard queries
- metric queries
- investigation queries
- agent requests
- administration

### Ingestion workers

Go workers for:

- connector synchronization
- normalization
- identity resolution
- reconciliation
- checkpoint management

### Intelligence service

Owns:

- metric definitions
- attribution
- temporal comparisons
- anomaly detection
- explanations
- insight generation

### Agent service

Owns:

- intent interpretation
- query planning
- AIKOQL query execution
- evidence retrieval
- explanation generation
- confidence/limitations

### Web

Board, manager and investigation views.

## 3. Data flow

```text
External Source
  -> Raw observation
  -> Normalized event
  -> Identity resolution
  -> Canonical mutation
  -> AIKOQL
  -> Derived event
  -> Metric observation
  -> Insight
```

## 4. Storage principle

Engineering Intelligence should not create a second knowledge store unless a demonstrated requirement cannot be satisfied through AIKOQL.

Operational data such as connector credentials, job locks and ephemeral worker state may use conventional infrastructure where appropriate.

Engineering knowledge should live in AIKOQL.

## 5. Failure isolation

A connector failure must not corrupt canonical knowledge.

Each source needs:

- checkpoint
- retry state
- source cursor
- ingestion run
- error state
- provenance

## 6. Security

Security is owned by the product:

- tenant isolation
- RBAC
- SSO/SCIM later
- audit
- source credential management
- data retention
- deletion workflows

AIKOQL provides generic database security primitives; it does not own engineering policy.

## 7. Deployment

Initial:

```text
Go API
Go workers
Web
AIKOQL
PostgreSQL or equivalent operational store
LLM provider
```

AIKOQL should be independently deployable.

## 8. Scalability principle

Prefer append/reconcile patterns over repeatedly rebuilding the full engineering graph.

Use source checkpoints and idempotent mutations.

## 9. Explainability

Every derived metric and insight should preserve:

- source observations
- transformation/version
- time window
- population
- filters
- calculation
- evidence references

This is a core product requirement, not a future feature.
