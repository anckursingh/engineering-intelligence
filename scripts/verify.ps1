# Verify gate — CI and local dev share this script.
#   ./scripts/verify.ps1          # fmt + vet + unit/acceptance tests + build
#   ./scripts/verify.ps1 -Race    # adds -race (needs CGO/gcc: CI ubuntu)
# The live GitHub E2E self-enables when EI_E2E_OWNER + EI_E2E_REPO are set.
param([switch]$Race)
$ErrorActionPreference = "Stop"

$fmt = gofmt -l .
if ($fmt) {
    Write-Host "unformatted files:"
    Write-Host $fmt
    exit 1
}
Write-Host "gofmt: clean"

go vet ./...

if ($Race) {
    go test -race ./...
} else {
    go test ./...
}

go build ./cmd/ei
Write-Host "verify: ok"
