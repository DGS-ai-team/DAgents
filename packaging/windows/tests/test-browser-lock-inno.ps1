param(
    [Parameter(Mandatory = $true)]
    [string]$Iscc
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$source = [IO.File]::ReadAllText((Join-Path $PSScriptRoot '..\dagents-installer.iss'))
$generated = [IO.File]::ReadAllText((Join-Path $PSScriptRoot 'browser-lock-inert.iss'))
foreach ($directive in @(
    @('CloseApplications', '@@CLOSE_APPLICATIONS@@'),
    @('CloseApplicationsFilter', '@@CLOSE_FILTER@@'),
    @('RestartApplications', '@@RESTART_APPLICATIONS@@')
)) {
    $match = [regex]::Match($source, ('(?m)^' + $directive[0] + '=([^\r\n]+)'))
    if (-not $match.Success) { throw "Missing production directive: $($directive[0])" }
    $generated = $generated.Replace($directive[1], $match.Groups[1].Value)
}

$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\', '/')
$caseRoot = Join-Path $tempRoot ('dagents-browser-lock-inert-' + [guid]::NewGuid().ToString('N'))
$installRoot = Join-Path $caseRoot 'upgrade target 中文'
$otherRoot = Join-Path $caseRoot 'other install'
$targetBin = Join-Path $installRoot 'bin'
$otherBin = Join-Path $otherRoot 'bin'
New-Item -ItemType Directory -Path $targetBin, $otherBin | Out-Null
$browserPath = Join-Path $targetBin 'dagents-browser.exe'
$otherBrowserPath = Join-Path $otherBin 'dagents-browser.exe'
$externalNodePath = Join-Path $targetBin 'dagents-node.exe'
foreach ($path in @($browserPath, $otherBrowserPath, $externalNodePath)) {
    Copy-Item -LiteralPath (Join-Path $env:SystemRoot 'System32\ping.exe') -Destination $path
}
$replacement = Join-Path $caseRoot 'replacement.exe'
Copy-Item -LiteralPath (Join-Path $env:SystemRoot 'System32\cmd.exe') -Destination $replacement
$generated = $generated.Replace('@@REPLACEMENT@@', $replacement)
$generatedPath = Join-Path $caseRoot 'browser-lock.generated.iss'
[IO.File]::WriteAllText($generatedPath, $generated, [Text.UTF8Encoding]::new($true))
& $Iscc $generatedPath ('/O' + $caseRoot) *> (Join-Path $caseRoot 'compile.log')
if ($LASTEXITCODE -ne 0) { throw "ISCC failed; retain $caseRoot" }

$processes = @()
try {
    foreach ($path in @($browserPath, $otherBrowserPath, $externalNodePath)) {
        $process = Start-Process -FilePath $path -ArgumentList '-t 127.0.0.1' -WindowStyle Hidden -PassThru
        # Keep a handle to this exact process rather than resolving a later PID.
        $null = $process.Handle
        $processes += $process
    }
    if (@($processes | Where-Object HasExited).Count -ne 0) { throw 'A lock fixture exited before setup.' }
    $setupPath = Join-Path $caseRoot 'dagents-browser-lock-inert.exe'
    $setup = Start-Process -FilePath $setupPath -WindowStyle Hidden -PassThru -ArgumentList (
        '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /DIR="' + $installRoot + '" /LOG="' + (Join-Path $caseRoot 'setup.log') + '"'
    )
    if (-not $setup.WaitForExit(45000)) {
        $setup.Kill($true)
        throw "Setup timed out; retain $caseRoot"
    }
    if ($setup.ExitCode -ne 0) { throw "Setup exit $($setup.ExitCode); retain $caseRoot" }
    if (-not $processes[0].WaitForExit(5000)) { throw 'The legacy browser was left running.' }
    if ((Get-FileHash -LiteralPath $browserPath).Hash -cne (Get-FileHash -LiteralPath $replacement).Hash) {
        throw 'Setup did not replace the locked browser executable.'
    }
    if ($processes[1].HasExited -or $processes[2].HasExited) {
        throw 'Setup stopped another installation or an external Node.'
    }
} finally {
    foreach ($process in $processes) {
        if (-not $process.HasExited) { $process.Kill(); $null = $process.WaitForExit(5000) }
        $process.Dispose()
    }
}

$resolvedCase = [IO.Path]::GetFullPath($caseRoot).TrimEnd('\', '/')
if ([IO.Path]::GetFullPath((Split-Path -Parent $resolvedCase)).TrimEnd('\', '/') -cne $tempRoot -or
    (Split-Path -Leaf $resolvedCase) -cnotmatch '^dagents-browser-lock-inert-[0-9a-f]{32}$') {
    throw 'Refusing cleanup outside the unique test boundary.'
}
foreach ($item in Get-ChildItem -LiteralPath $resolvedCase -Force -Recurse) {
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Refusing cleanup of a reparse point.' }
}
Remove-Item -LiteralPath $resolvedCase -Recurse -Force
[ordered]@{
    passed = $true
    engine = 'Inno Setup Restart Manager'
    legacy_browser_replaced = $true
    other_install_browser_survived = $true
    external_node_survived = $true
    reboot_required = $false
} | ConvertTo-Json -Compress | Write-Output
