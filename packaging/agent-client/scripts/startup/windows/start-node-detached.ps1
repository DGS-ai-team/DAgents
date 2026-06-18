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

$logDir = Split-Path -Parent $LogOut
if (-not (Test-Path -LiteralPath $logDir)) {
    New-Item -ItemType Directory -Path $logDir -Force | Out-Null
}

# start /B 与调用方控制台同组，关终端会 kill Node；Start-Process 脱离当前控制台。
$redirect = "`"$NodeExe`" -config `"$Config`" 1>>`"$LogOut`" 2>>`"$LogErr`""
$null = Start-Process -FilePath "cmd.exe" -ArgumentList @("/c", $redirect) -WorkingDirectory $WorkingDirectory -WindowStyle Hidden

Start-Sleep -Seconds 1

$node = Get-CimInstance Win32_Process -Filter "Name='dagents-node.exe'" |
    Where-Object { $_.CommandLine -like "*$Config*" } |
    Select-Object -First 1

if (-not $node) {
    Write-Error "dagents-node did not start (see $LogErr)"
    exit 1
}

Set-Content -LiteralPath $PidFile -Value $node.ProcessId -NoEncoding
exit 0
