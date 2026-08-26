# Run the repository's complete local quality gate on Windows.
# Usage: powershell -ExecutionPolicy Bypass -File scripts/verify.ps1
$ErrorActionPreference = "Stop"
Set-Location (Split-Path -Parent $PSScriptRoot)

function Invoke-Step([string]$Name, [scriptblock]$Action) {
    Write-Host "[verify] $Name"
    & $Action
    if ($LASTEXITCODE -ne 0) {
        throw "step failed: $Name (exit code $LASTEXITCODE)"
    }
}

$goPackages = @(
    "./shared/config/...",
    "./shared/logfiles/...",
    "./shared/update/...",
    "./shared/workgroup/...",
    "./node/...",
    "./client/...",
    "./desktop/tray/..."
)

Invoke-Step "install Node Web UI dependencies" { npm ci --prefix node/webui/frontend }
Invoke-Step "build Node Web UI" { npm run build --prefix node/webui/frontend }
Invoke-Step "test Node Web UI" { npm test --prefix node/webui/frontend }
Invoke-Step "install Manage Console dependencies" { npm ci --prefix manage/console/frontend }
Invoke-Step "build Manage Console" { npm run build --prefix manage/console/frontend }
Invoke-Step "install Python dependencies" {
    python -m pip install --requirement requirements.lock --requirement requirements-dev.txt
}
Invoke-Step "Ruff" { python -m ruff check manage scripts tests }
Invoke-Step "Pyright" { python -m pyright --project pyrightconfig.json }
Invoke-Step "Python unittest" { python -m unittest discover -s tests -p "test_*.py" -v }
Invoke-Step "API and fixture contracts" { python scripts/ci/check_contracts.py }
Invoke-Step "OpenAPI route coverage" { python scripts/ci/sync_openapi_routes.py --check }

$goFiles = @(git ls-files "*.go")
$gofmtOutput = @(gofmt -l $goFiles)
if ($gofmtOutput.Count -ne 0) {
    throw "unformatted Go files:`n$($gofmtOutput -join "`n")"
}
Invoke-Step "Go vet" { go vet $goPackages }
Invoke-Step "Go tests" { go test $goPackages }
$verifyDir = Join-Path ([IO.Path]::GetTempPath()) ("dagents-verify-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $verifyDir | Out-Null
Invoke-Step "build dagents-node" { go build -o (Join-Path $verifyDir "dagents-node.exe") ./node/cmd/dagents-node }
Invoke-Step "build dagents-client" { go build -o (Join-Path $verifyDir "dagents-client.exe") ./client/cmd/dagents-client }

if (Get-Command cargo -ErrorAction SilentlyContinue) {
    Invoke-Step "Rust format" { cargo fmt --check --manifest-path desktop/tray-tauri/src-tauri/Cargo.toml }
    Invoke-Step "Rust tests" { cargo test --locked --manifest-path desktop/tray-tauri/src-tauri/Cargo.toml }
}

Invoke-Step "repository hygiene" { git diff --check }
Write-Host "[verify] passed"
