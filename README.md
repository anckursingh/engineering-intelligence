# Engineering Intelligence

[![CI](https://github.com/anckursingh/engineering-intelligence/actions/workflows/ci.yml/badge.svg)](https://github.com/anckursingh/engineering-intelligence/actions/workflows/ci.yml)

Engineering Intelligence is a standalone product concept for understanding software engineering as a living knowledge system.

It combines engineering delivery, quality, reliability, AI-assisted development, and agentic development into one evidence-backed intelligence layer.

**AIKOQL is the database. It is not part of the product domain model.**

## Product thesis

Traditional engineering intelligence platforms primarily turn engineering telemetry into metrics and dashboards.

Engineering Intelligence aims to build a canonical, temporal, provenance-aware model of:

- people and teams
- products and services
- requirements and work
- code and repositories
- reviews and CI/CD
- deployments and incidents
- AI coding assistants
- autonomous coding agents
- engineering outcomes

The product then reasons over that model and explains its conclusions with evidence.

## Repository status

Phase 1 (GitHub vertical slice) is implemented: connector → normalization → identity v1 → canonical ontology → `KnowledgeStore` (in-memory dev double; the AIKOQL adapter lands with the Go SDK). Incremental sync with checkpointing, CLI, offline unit + acceptance tests, env-gated live E2E, CI on push and PRs.

## Layout

```text
cmd/ei                          CLI (one binary)
internal/github                 GitHub connector (client, normalize, sync)
internal/github/githubtest      shared fake-GitHub world for the test suites
internal/{identity,ontology,knowledge,checkpoint}
test/acceptance                 black-box gate for docs/MVP-ACCEPTANCE.md
test/e2e                        live GitHub E2E (env-gated)
scripts/verify.ps1              the gate: fmt + vet + tests + build
docs/                           product and architecture docs
```

Unit tests live beside their code (`*_test.go` in-package, Go convention); the
external suites live in `test/` and share one fixture world via `githubtest`.

## Quick start

```powershell
go build ./cmd/ei
.\ei.exe sync github --owner go-playground --repo colors   # GITHUB_TOKEN optional
```

## Tests

```powershell
./scripts/verify.ps1                                      # unit + acceptance, all offline
$env:EI_E2E_OWNER = "go-playground"; $env:EI_E2E_REPO = "colors"
go test -run TestLiveE2E ./test/e2e/                      # live GitHub E2E (pick a dormant repo)
```

## Core documents

- [Product Requirements](docs/PRD.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Canonical Ontology](docs/ONTOLOGY.md)
- [Competitive Teardown](docs/COMPETITIVE-TEARDOWN.md)
- [MVP Acceptance Criteria](docs/MVP-ACCEPTANCE.md)
- [Product Principles](docs/PRODUCT-PRINCIPLES.md)
- [Roadmap](docs/ROADMAP.md)
- [AI Development Intelligence](docs/AI-DEVELOPMENT-INTELLIGENCE.md)
- [AIKOQL Integration Contract](docs/AIKOQL-INTEGRATION.md)
- [Risks and Open Questions](docs/RISKS.md)

## Strategic boundary

```text
Engineering Intelligence
        |
        | public API / Go client
        v
      AIKOQL
        |
        v
 Knowledge Objects / Graph / Semantic / Events / Storage
```

Engineering Intelligence owns engineering semantics.

AIKOQL remains a general-purpose database.
