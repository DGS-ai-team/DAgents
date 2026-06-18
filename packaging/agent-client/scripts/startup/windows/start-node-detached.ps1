param(
    [Parameter(Mandatory = $true)][string]$NodeExe,
    [Parameter(Mandatory = $true)][string]$Config,
    [Parameter(Mandatory = $true)][string]$WorkingDirectory,
    [Parameter(Mandatory = $true)][string]$LogOut,
    [Parameter(Mandatory = $true)][string]$LogErr,
    [Parameter(Mandatory = $true)][string]$PidFile
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $NodeExe)) {
    Write-Error "node binary not found: $NodeExe"
    exit 1
}

if (Test-Path -LiteralPath $Config) {
    $Config = (Resolve-Path -LiteralPath $Config).Path
}

$logDir = Split-Path -Parent $LogOut
if (-not (Test-Path -LiteralPath $logDir)) {
    New-Item -ItemType Directory -Path $logDir -Force | Out-Null
}

function Read-LogTail {
    param([string]$Path, [int]$Lines = 8)
    if (-not (Test-Path -LiteralPath $Path)) { return "" }
    $tail = Get-Content -LiteralPath $Path -Tail $Lines -ErrorAction SilentlyContinue
    if (-not $tail) { return "" }
    return "`n" + ($tail -join "`n")
}

# 直接 Start-Process dagents-node；避免 cmd /c 在含空格路径（如 Program Files）下引号/重定向失败。
try {
    $proc = Start-Process `
        -FilePath $NodeExe `
        -ArgumentList @("-config", $Config) `
        -WorkingDirectory $WorkingDirectory `
        -WindowStyle Hidden `
        -PassThru `
        -RedirectStandardOutput $LogOut `
        -RedirectStandardError $LogErr
} catch {
    Write-Error "Start-Process failed: $_$(Read-LogTail $LogErr)"
    exit 1
}

if (-not $proc) {
    Write-Error "Start-Process returned no process handle$(Read-LogTail $LogErr)"
    exit 1
}

$deadline = (Get-Date).AddSeconds(5)
while ((Get-Date) -lt $deadline) {
    if (-not $proc.HasExited) {
        [System.IO.File]::WriteAllText($PidFile, [string]$proc.Id)
        exit 0
    }
    Start-Sleep -Milliseconds 200
}

$hint = Read-LogTail $LogErr
Write-Error "dagents-node exited immediately (code=$($proc.ExitCode)); see $LogErr$hint"
exit 1
