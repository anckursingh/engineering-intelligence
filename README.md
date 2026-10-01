# Engineering Intelligence

[![CI](https://github.com/anckursingh/engineering-intelligence/actions/workflows/ci.yml/badge.svg)](https://github.com/anckursingh/engineering-intelligence/actions/workflows/ci.yml)

Engineering Intelligence is a proof of concept for an evidence-backed model of software delivery. It ingests engineering activity, connects it into a canonical graph, and computes deterministic metrics and investigations over that data.

**AIKOQL is the persistence and graph layer. Engineering Intelligence owns the engineering ontology, metric definitions, attribution rules, and explanations.**

## Current proof of concept

The runnable vertical slice includes:

- GitHub repository, issue, pull request, review, commit, CI, deployment, and release sync.
- Jira issue and epic sync.
- Claude Code transcript ingestion, with explicit unlinked and unattributed counts.
- AIKOQL-backed persistence, incremental checkpoints, idempotent writes, identity resolution, and provenance.
- Deterministic metrics, board and comparison endpoints, and evidence-referenced investigations.

This is a POC, not a production-ready hosted service. See [the current runbook](docs/POC-RUNBOOK.md) and [the validation plan](docs/VALIDATION-PLAN.md) for the intended first evaluation.

## Repository layout

```text
cmd/ei                          CLI: sync, ingest, serve
internal/github                 GitHub connector
internal/jira                   Jira connector
internal/claudecode             Claude Code transcript connector
internal/{identity,ontology,knowledge,checkpoint}
internal/metrics                Deterministic metric definitions and calculations
internal/intelligence           Population traversal, investigations, API, board
test/acceptance                 Offline black-box MVP acceptance suite
test/e2e                        Environment-gated live GitHub E2E
docs/                           Product, architecture, integration, and validation docs
```

## Run locally

The CLI starts an AIKOQL MCP server process for each sync/ingest command and uses the same `--db` directory to persist data. `ei serve` keeps its AIKOQL process alive while serving the HTTP API.

Prerequisites:

- Go version from `go.mod`.
- An AIKOQL MCP server binary compatible with the adapter in `internal/knowledge`.
- GitHub token for practical API limits. Jira and Claude Code ingestion are optional.

PowerShell example:

```powershell
$env:AIKOQL_MCP_BIN = (Resolve-Path "C:\path\to\aikoql-mcp.exe").Path
$env:GITHUB_TOKEN = "<token>" # optional for public repositories

go build -o ei.exe ./cmd/ei
./ei.exe sync github --owner go-playground --repo colors --db .ei/db
./ei.exe serve --db .ei/db --addr 127.0.0.1:8080
```

In another terminal, request the board for the organization and time window:

```powershell
Invoke-RestMethod "http://localhost:8080/board?scope=github.com%3Aorg%3Ago-playground&start=2026-09-01T00%3A00%3A00Z&end=2026-10-01T00%3A00%3A00Z"
```

Use `GET /health`, `GET /metrics`, and `GET /board` to explore the API. `POST /ask`, `POST /investigations`, and `POST /comparisons` are described with request examples in [the POC runbook](docs/POC-RUNBOOK.md). Scope identifiers depend on whether the GitHub owner is an organization or personal account; the runbook explains both.

The board accepts comma-separated scope IDs to combine ingested sources in one view. For example, set `EI_BOARD_SCOPE` or pass `--scope` to `ei serve`:

```powershell
$env:EI_BOARD_SCOPE = "github.com:account:OWNER,jira.com:example.atlassian.net:project:KEY"
./ei.exe serve --db .ei/db --addr 127.0.0.1:8080
```

The combined board shows all metrics supported by the loaded data. Each source still needs to be synced into the same AIKOQL database.

### Optional sources

```powershell
# Jira project (credentials may also be supplied through JIRA_EMAIL/JIRA_TOKEN).
./ei.exe sync jira --base-url https://example.atlassian.net --email you@example.com --token $env:JIRA_TOKEN --project ENG --db .ei/db

# Local Claude Code transcript directory.
./ei.exe ingest claude --dir "$env:USERPROFILE\.claude\projects" --db .ei/db
```

Only ingest AI transcripts when the organization has approved the data use. Transcripts may contain private prompts and source context even though the run log does not emit them.

## Verification

```powershell
./scripts/verify.ps1
$env:EI_E2E_OWNER = "go-playground"; $env:EI_E2E_REPO = "colors"
go test -run TestLiveE2E ./test/e2e/
```

The live E2E requires network access and a reachable GitHub repository. The standard verification gate is offline.

## Product direction

The initial wedge is not another DORA board. Engineering Intelligence should help a team investigate changes in flow, quality, reliability, and AI-assisted work, with visible evidence, population, time window, epistemic state, and limitations. No individual productivity score is a product goal.

Start with [Product Requirements](docs/PRD.md), [Architecture](docs/ARCHITECTURE.md), [Canonical Ontology](docs/ONTOLOGY.md), [Competitive Teardown](docs/COMPETITIVE-TEARDOWN.md), and [MVP Acceptance Criteria](docs/MVP-ACCEPTANCE.md). The next customer-validation work is tracked in [Validation Plan](docs/VALIDATION-PLAN.md).

## Strategic boundary

```text
Engineering Intelligence
        | public protocol / Go client
        v
      AIKOQL
        v
Knowledge Objects / Graph / Temporal Storage / Provenance
```

AIKOQL remains a general-purpose database. Engineering Intelligence owns engineering semantics and customer-facing reasoning.
