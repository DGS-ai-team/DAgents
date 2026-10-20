param(
    [Parameter(Mandatory = $true)]
    [string]$Iscc
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$installerSource = Join-Path $PSScriptRoot '..\dagents-installer.iss'
$harnessSource = Join-Path $PSScriptRoot 'path-cleanup-inert.iss'
$isccPath = [IO.Path]::GetFullPath($Iscc)
foreach ($path in @($installerSource, $harnessSource, $isccPath)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Required test input is missing: $path" }
}

$source = [IO.File]::ReadAllText($installerSource)
$functionMatch = [regex]::Match($source, '(?ms)^function RemovePathEntry\([^\r\n]*\r?\n.*?^end;')
if (-not $functionMatch.Success) {
    throw 'Could not extract the production RemovePathEntry implementation.'
}

$registryPath = 'Environment'
$registryView = [Microsoft.Win32.RegistryView]::Registry64
$registryBase = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::CurrentUser, $registryView)
try {
    $environmentKey = $registryBase.OpenSubKey($registryPath, $false)
    if (-not $environmentKey) { throw 'HKCU Environment key is unavailable.' }
    try {
        $pathExists = $environmentKey.GetValueNames() -contains 'Path'
        $pathKind = if ($pathExists) { $environmentKey.GetValueKind('Path').ToString() } else { $null }
        $pathValue = if ($pathExists) { [string]$environmentKey.GetValue('Path', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) } else { $null }
    } finally { $environmentKey.Dispose() }
} finally { $registryBase.Dispose() }
$pathHashBefore = if ($pathExists) {
    $bytes = [Text.Encoding]::Unicode.GetBytes($pathValue)
    [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes))
} else { $null }

$appIdSubKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{C73B9F58-4D36-44B3-BE63-82DC0D1CA9A4}_is1'
function Test-AppIdRegistered {
    foreach ($view in @([Microsoft.Win32.RegistryView]::Registry64, [Microsoft.Win32.RegistryView]::Registry32)) {
        $base = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::CurrentUser, $view)
        try {
            $key = $base.OpenSubKey($appIdSubKey, $false)
            if ($key) { $key.Dispose(); return $true }
        } finally { $base.Dispose() }
    }
    return $false
}
if (Test-AppIdRegistered) { throw 'The inert test AppId is already registered; refusing to continue.' }

$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\', '/')
$caseName = 'dagents-path-cleanup-inno-' + [guid]::NewGuid().ToString('N')
$caseRoot = Join-Path $tempRoot $caseName
New-Item -ItemType Directory -Path $caseRoot | Out-Null
$installRoot = Join-Path $caseRoot 'installed'
$generatedScript = Join-Path $caseRoot 'path-cleanup-inert.generated.iss'
$template = [IO.File]::ReadAllText($harnessSource)
$marker = '@@REMOVE_PATH_ENTRY@@'
if ([regex]::Matches($template, [regex]::Escape($marker)).Count -ne 1) { throw 'Inert harness insertion point is invalid.' }
$generated = $template.Replace($marker, $functionMatch.Value)
[IO.File]::WriteAllText($generatedScript, $generated, (New-Object System.Text.UTF8Encoding($true)))

$compileLog = Join-Path $caseRoot 'compile.log'
& $isccPath $generatedScript ('/O' + $caseRoot) *> $compileLog
if ($LASTEXITCODE -ne 0) { throw "ISCC failed with exit code $LASTEXITCODE; retain $caseRoot" }
$setupPath = Join-Path $caseRoot 'dagents-path-cleanup-inert-regression.exe'
$setupArguments = '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /DIR="' + $installRoot + '"'
$setup = Start-Process -FilePath $setupPath -ArgumentList $setupArguments -PassThru -Wait
if ($setup.ExitCode -ne 0) { throw "Inert setup failed with exit code $($setup.ExitCode); retain $caseRoot" }
$uninstallerPath = Join-Path $installRoot 'unins000.exe'
if (-not (Test-Path -LiteralPath $uninstallerPath -PathType Leaf)) { throw "Inert uninstaller missing; retain $caseRoot" }
$uninstaller = Start-Process -FilePath $uninstallerPath -ArgumentList '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART' -PassThru -Wait
if ($uninstaller.ExitCode -ne 0) { throw "Inert uninstall failed with exit code $($uninstaller.ExitCode); retain $caseRoot" }

$resultPath = Join-Path $installRoot 'path-cleanup-result.txt'
if (-not (Test-Path -LiteralPath $resultPath -PathType Leaf)) { throw "Inert uninstall result missing; retain $caseRoot" }
$result = @{}
foreach ($line in [IO.File]::ReadAllLines($resultPath)) {
    if ($line -match '^([^=]+)=(.*)$') { $result[$matches[1]] = $matches[2] }
}
if ($result.simple_exact -cne '1' -or $result.long_exact -cne '1' -or $result.root_exact -cne '1' -or $result.long_token_count -cne '17') {
    throw "Production RemovePathEntry failed inert Inno regression (simple=$($result.simple_exact), long=$($result.long_exact), root=$($result.root_exact), count=$($result.long_token_count)); retain $caseRoot"
}
if (Test-AppIdRegistered) { throw 'Inert test AppId remained registered after uninstall.' }

$registryBase = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::CurrentUser, $registryView)
try {
    $environmentKey = $registryBase.OpenSubKey($registryPath, $false)
    if (-not $environmentKey) { throw 'HKCU Environment key disappeared during the inert test.' }
    try {
        $nowExists = $environmentKey.GetValueNames() -contains 'Path'
        $nowKind = if ($nowExists) { $environmentKey.GetValueKind('Path').ToString() } else { $null }
        $nowValue = if ($nowExists) { [string]$environmentKey.GetValue('Path', $null, [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames) } else { $null }
    } finally { $environmentKey.Dispose() }
} finally { $registryBase.Dispose() }
$pathHashAfter = if ($nowExists) {
    $bytes = [Text.Encoding]::Unicode.GetBytes($nowValue)
    [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes))
} else { $null }
if ($nowExists -ne $pathExists -or $nowKind -cne $pathKind -or $pathHashAfter -cne $pathHashBefore) {
    throw "Inert installer modified HKCU Path; retain $caseRoot"
}

$resolvedCase = [IO.Path]::GetFullPath($caseRoot).TrimEnd('\', '/')
if ([IO.Path]::GetFullPath((Split-Path -Parent $resolvedCase)).TrimEnd('\', '/') -cne $tempRoot -or
    (Split-Path -Leaf $resolvedCase) -cnotmatch '^dagents-path-cleanup-inno-[0-9a-f]{32}$') {
    throw 'Refusing cleanup because the inert test path does not match its unique temp boundary.'
}
foreach ($item in Get-ChildItem -LiteralPath $caseRoot -Force -Recurse) {
    if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) { throw 'Refusing cleanup: inert test tree contains a reparse point.' }
}
Remove-Item -LiteralPath $caseRoot -Recurse -Force
Write-Output ([ordered]@{ passed = $true; engine = 'Inno Setup'; path_cases = 3; long_path_tokens = 17; drive_root_preserved = $true; user_path_unchanged = $true; temporary_appid_removed = $true } | ConvertTo-Json -Compress)
