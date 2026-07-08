param(
    [Parameter(Mandatory = $true)][string]$ShellExe,
    [Parameter(Mandatory = $true)][string]$Config,
    [Parameter(Mandatory = $true)][string]$WorkingDirectory,
    [Parameter(Mandatory = $true)][string]$LogOut,
    [Parameter(Mandatory = $true)][string]$LogErr,
    [Parameter(Mandatory = $true)][string]$PidFile
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $ShellExe)) {
    Write-Error "shell binary not found: $ShellExe"
    exit 1
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

function Resolve-ConfigArgument {
    param([string]$ConfigPath, [string]$WorkDir)

    $candidate = $ConfigPath
    if (-not [System.IO.Path]::IsPathRooted($candidate)) {
        $candidate = Join-Path $WorkDir $candidate
    }
    if (Test-Path -LiteralPath $candidate) {
        $resolvedConfig = (Resolve-Path -LiteralPath $candidate).Path
        $resolvedWd = (Resolve-Path -LiteralPath $WorkDir).Path.TrimEnd('\')
        if ($resolvedConfig.StartsWith($resolvedWd, [StringComparison]::OrdinalIgnoreCase)) {
            return $resolvedConfig.Substring($resolvedWd.Length).TrimStart('\')
        }
        return $resolvedConfig
    }
    return $ConfigPath
}

$configArg = Resolve-ConfigArgument -ConfigPath $Config -WorkDir $WorkingDirectory
$argumentString = "-config `"$configArg`""

try {
    $proc = Start-Process `
        -FilePath $ShellExe `
        -ArgumentList $argumentString `
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
Write-Error "dagents-shell exited immediately (code=$($proc.ExitCode)); see $LogErr$hint"
exit 1
