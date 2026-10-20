param(
    [Parameter(Mandatory = $true)]
    [string]$Iscc
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$installerSource = Join-Path $PSScriptRoot '..\dagents-installer.iss'
$harnessSource = Join-Path $PSScriptRoot 'silent-policy-preservation-inert.iss'
$isccPath = [IO.Path]::GetFullPath($Iscc)
foreach ($path in @($installerSource, $harnessSource, $isccPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required test input is missing: $path"
    }
}

$appIdSubKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{8D7B12E6-4E35-4F01-9CB4-1C88F10921A7}_is1'
function Test-InertAppIdRegistered {
    foreach ($view in @([Microsoft.Win32.RegistryView]::Registry64, [Microsoft.Win32.RegistryView]::Registry32)) {
        $base = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::CurrentUser, $view)
        try {
            $key = $base.OpenSubKey($appIdSubKey, $false)
            if ($key) {
                $key.Dispose()
                return $true
            }
        } finally {
            $base.Dispose()
        }
    }
    return $false
}
if (Test-InertAppIdRegistered) {
    throw 'The inert policy test AppId is already registered; refusing to continue.'
}

$source = [IO.File]::ReadAllText($installerSource)
function Get-TopLevelFunction([string]$Name, [string]$SourceText) {
    $escaped = [regex]::Escape($Name)
    $match = [regex]::Match($SourceText, "(?ms)^function $escaped\([^\r\n]*\r?\n.*?^end;")
    if (-not $match.Success) {
        throw "Could not extract production function $Name."
    }
    return $match.Value
}
$policyFunction = Get-TopLevelFunction 'PolicyExistsAt' $source
$nextButtonFunction = Get-TopLevelFunction 'NextButtonClick' $source
if ($nextButtonFunction -notmatch 'SuppressibleMsgBox\(' -or
    $nextButtonFunction -notmatch 'MB_YESNO or MB_DEFBUTTON2, IDNO\)') {
    throw 'Production policy confirmation must be suppressible and default to IDNO.'
}

$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\', '/')
$caseRoot = Join-Path $tempRoot ('dagents-policy-inert-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $caseRoot | Out-Null
$installRoot = Join-Path $caseRoot 'existing-target'
$policyPath = Join-Path $installRoot '.runtime\policy\tool.approval.txt'
New-Item -ItemType Directory -Path (Split-Path -Parent $policyPath) -Force | Out-Null
[IO.File]::WriteAllText($policyPath, 'inert-policy-keep', [Text.Encoding]::ASCII)

$template = [IO.File]::ReadAllText($harnessSource)
$generated = $template.Replace('@@POLICY_EXISTS_AT@@', $policyFunction).Replace('@@NEXT_BUTTON_CLICK@@', $nextButtonFunction)
$generatedPath = Join-Path $caseRoot 'silent-policy-preservation.generated.iss'
[IO.File]::WriteAllText($generatedPath, $generated, (New-Object System.Text.UTF8Encoding($true)))
$compileLog = Join-Path $caseRoot 'compile.log'
& $isccPath $generatedPath ('/O' + $caseRoot) *> $compileLog
if ($LASTEXITCODE -ne 0) {
    throw "ISCC failed with exit code $LASTEXITCODE; retain $caseRoot."
}

$setupPath = Join-Path $caseRoot 'dagents-silent-policy-inert-regression.exe'
$setup = Start-Process -FilePath $setupPath -ArgumentList ('/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /DIR="' + $installRoot + '"') -PassThru
if (-not $setup.WaitForExit(20000)) {
    try { $setup.Kill($true) } catch { }
    throw "Silent inert setup did not finish in 20 seconds; its process tree was stopped, retain $caseRoot."
}
if ($setup.ExitCode -ne 0) {
    throw "Silent inert setup failed with exit code $($setup.ExitCode); retain $caseRoot."
}
$resultPath = Join-Path $installRoot 'inert-policy-result.txt'
if (-not (Test-Path -LiteralPath $resultPath -PathType Leaf) -or
    [IO.File]::ReadAllText($resultPath) -cne 'overwrite=false;policy=preserved') {
    throw "Silent policy preservation result failed; retain $caseRoot."
}
if ([IO.File]::ReadAllText($policyPath) -cne 'inert-policy-keep') {
    throw "Existing policy content changed; retain $caseRoot."
}
$uninstallerPath = Join-Path $installRoot 'unins000.exe'
if (-not (Test-Path -LiteralPath $uninstallerPath -PathType Leaf)) {
    throw "Inert uninstaller is missing; retain $caseRoot."
}
$uninstaller = Start-Process -FilePath $uninstallerPath -ArgumentList '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART' -PassThru
if (-not $uninstaller.WaitForExit(20000)) {
    try { $uninstaller.Kill($true) } catch { }
    throw "Silent inert uninstall did not finish in 20 seconds; its process tree was stopped, retain $caseRoot."
}
if ($uninstaller.ExitCode -ne 0) {
    throw "Silent inert uninstall failed with exit code $($uninstaller.ExitCode); retain $caseRoot."
}
if (Test-InertAppIdRegistered) {
    throw 'The inert policy test AppId remained registered after uninstall.'
}

$resolvedCase = [IO.Path]::GetFullPath($caseRoot).TrimEnd('\', '/')
if ([IO.Path]::GetFullPath((Split-Path -Parent $resolvedCase)).TrimEnd('\', '/') -cne $tempRoot -or
    (Split-Path -Leaf $resolvedCase) -cnotmatch '^dagents-policy-inert-[0-9a-f]{32}$') {
    throw 'Refusing cleanup because the inert test path is outside its unique temp boundary.'
}
foreach ($item in Get-ChildItem -LiteralPath $caseRoot -Force -Recurse) {
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
        throw "Refusing cleanup because the inert test tree contains a reparse point: $($item.FullName)"
    }
}
Remove-Item -LiteralPath $caseRoot -Recurse -Force
[ordered]@{
    passed = $true
    engine = 'Inno Setup'
    silent_policy = 'default-preserved'
    policy_content_unchanged = $true
    interactive_prompt_source_contract = $true
} | ConvertTo-Json -Compress | Write-Output