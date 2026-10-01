# POC Runbook

This runbook exercises the current local POC against one GitHub owner. Jira and
Claude Code are optional enrichments. It is intended for a controlled evaluation,
not unattended production use.

## Before starting

1. Build or obtain an AIKOQL MCP server binary compatible with the adapter. Set
   `AIKOQL_MCP_BIN` to its absolute path.
2. Use a GitHub token for more than a small public-repository smoke test. Keep
   tokens in process environment or `.env`; do not commit secrets.
3. Choose a dedicated database directory. Each CLI command starts its own
   AIKOQL process over that directory, so do not run concurrent commands against
   the same directory unless the server explicitly supports it.
4. For a customer evaluation, agree on data scope, access, retention, and whether
   local Claude Code transcripts may be processed. Transcripts can include
   prompts and source context.

## Build and ingest GitHub

```powershell
$env:AIKOQL_MCP_BIN = (Resolve-Path "C:\path\to\aikoql-mcp.exe").Path
$env:GITHUB_TOKEN = "<token>"
go build -o ei.exe ./cmd/ei
./ei.exe sync github --owner <owner> --repo <repo> --db .ei/poc-db
```

The first GitHub sync builds the initial history and writes a checkpoint under
`.ei/`. Later syncs use the checkpoint unless `--since RFC3339` is supplied.
Sync Jira into the same AIKOQL database with `ei sync jira`; each connector has
its own checkpoint.

Organization scopes use `github.com:org:<login>`. Personal account scopes use
`github.com:account:<login>`. These are the root external IDs used by the
population traversal.

## Optional AI telemetry

Ingest a local Claude Code transcript directory after syncing GitHub:

```powershell
./ei.exe ingest claude --dir "$env:USERPROFILE\.claude\projects" --db .ei/poc-db
```

The connector only links contributions when transcript evidence identifies a
repository and PR that already exists in the store. The command reports
unlinked, unattributed, and unparsed records; these are coverage gaps, not
records to silently infer away. This connector does not currently provide
complete organization-wide Claude telemetry or transcript cost.

## Start the API

```powershell
./ei.exe serve --db .ei/poc-db --addr 127.0.0.1:8080
```

Keep the server running in this terminal. In a second PowerShell terminal, set
the scope and RFC3339 time window:

```powershell
$scope = "github.com:org:<owner>" # or github.com:account:<login>
$start = "2026-09-01T00:00:00Z"
$end = "2026-10-01T00:00:00Z"
Invoke-RestMethod "http://localhost:8080/board?scope=$([uri]::EscapeDataString($scope))&start=$([uri]::EscapeDataString($start))&end=$([uri]::EscapeDataString($end))"
```

## Exercise the investigation surfaces

Ask a supported question using two equal-length periods. For example:

```powershell
$body = @{
  question = "What is associated with increased review latency?"
  scope = $scope
  from = "2026-09-01T00:00:00Z"
  to = "2026-10-01T00:00:00Z"
} | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri "http://localhost:8080/ask" -ContentType "application/json" -Body $body
```

The API derives the immediately preceding period of equal length. Other
supported `/ask` questions are:

- `Why did cycle time change?`
- `Why did CI quality change?`
- `What changed after AI adoption increased?`
- `Where are agent tasks failing?`

`/ask` accepts one period. `POST /investigations` accepts explicitly named
`window_a` and `window_b` periods for a metric comparison. `POST /comparisons`
compares merged PRs with evidence-qualified AI attribution against PRs without
positive AI evidence. Only valid DIRECT or STRONG attribution counts as
positive evidence. The other group may include unknown or unobserved AI
activity; it is not a human-authored cohort. The comparison is observational;
population differences do not establish that AI caused an outcome.

Each result should be reviewed for its population/scope, time window, evidence
object IDs, epistemic state, and limitations. Resolve a sample of evidence IDs
back to source records before treating a finding as decision-grade.

## First evaluation checklist

- [ ] Sync one agreed organization or team and record the date range and sources.
- [ ] Confirm the root scope resolves to the expected repositories.
- [ ] Check sync summaries and AI attribution gaps before interpreting metrics.
- [ ] Ask at least three supported questions and save the raw JSON responses.
- [ ] Have an engineering leader independently check the cited source records.
- [ ] Record whether the answer changed a concrete decision or investigation.
- [ ] Log missing data, incorrect links, confusing language, and unsupported questions.
- [ ] Delete the evaluation database and source exports according to the agreed
  retention boundary when the evaluation ends.

Do not use these results to rank individual engineers. The current POC has no
authentication or authorization layer; bind the API to loopback as shown above
and use only data approved for this evaluation.
