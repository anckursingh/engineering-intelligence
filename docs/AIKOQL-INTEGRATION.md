# AIKOQL Integration Contract

## Principle

Engineering Intelligence depends on AIKOQL as a database.

It must not depend on AIKOQL internal Rust crates.

## Desired boundary

```text
Go application
      |
      v
aikoql-go client / public protocol
      |
      v
AIKOQL server
```

## Required capabilities

The application needs stable support for:

### Knowledge objects

- create
- update
- retrieve
- batch mutation
- idempotency

### Relationships

- create/delete
- outbound traversal
- inbound traversal
- filtered traversal

### Queries

- structured queries
- filtering
- temporal filtering
- graph traversal
- semantic retrieval where applicable

### Events

- subscribe
- replay
- checkpoint/ack

### Provenance

- source metadata
- lineage references
- timestamps

### Transactions

Cross-entity mutations must have a defined atomicity model.

## Go client

A first-class Go client should only be created once the public database interface is stable.

Conceptually:

```go
client, err := aikoql.Connect(ctx, endpoint)

objects, err := client.Query(ctx, query)
err = client.Mutate(ctx, mutations)
events, err := client.Subscribe(ctx, filter)
```

The exact API should be designed from the actual stable AIKOQL server contract.

## Important constraint

Do not resurrect a Go SDK merely by copying the historical deleted SDK.

The Go client should be:

- generated or contract-driven where practical
- versioned
- integration-tested against the real AIKOQL server
- independent of internal Rust crates

## Development sequence

1. Freeze public AIKOQL API contract.
2. Identify gaps required by Engineering Intelligence.
3. Add only generic database capabilities to AIKOQL.
4. Implement Go client.
5. Build Engineering Intelligence against the client.
