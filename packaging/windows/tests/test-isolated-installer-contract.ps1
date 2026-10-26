$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$testDirectory = [IO.Path]::GetFullPath($PSScriptRoot)
$workspaceRoot = [IO.Path]::GetFullPath((Join-Path $testDirectory '..\..\..'))
$sourceScript = Join-Path $workspaceRoot 'packaging/windows/test-isolated-installer.ps1'
if (-not (Test-Path -LiteralPath $sourceScript -PathType Leaf)) {
    throw "Repository installer harness not found: $sourceScript"
}
$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile($sourceScript, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count) { throw ($parseErrors | Out-String) }
$functions = $ast.FindAll({ param($node) $node -is [System.Management.Automation.Language.FunctionDefinitionAst] }, $true)
foreach ($function in $functions) { Invoke-Expression $function.Extent.Text }

$script:passed = 0
function Assert-True([bool]$Condition, [string]$Name) { if (-not $Condition) { throw "FAIL: $Name" }; $script:passed++; Write-Host "PASS: $Name" }
function Assert-Throws([scriptblock]$Action, [string]$Name) { try { & $Action } catch { $script:passed++; Write-Host "PASS: $Name"; return }; throw "FAIL: $Name (did not throw)" }

$script:testArea = Join-Path ([IO.Path]::GetTempPath()) ('dagents-installer-contract-' + [guid]::NewGuid().ToString('N'))
$script:RepoRoot = Join-Path $script:testArea 'workspace'
New-Item -ItemType Directory -Force -Path $script:RepoRoot | Out-Null
$script:ExpectedInstallerName = '^dagents-local-assistant-windows-(?<platform>amd64|386)-installer-(?<version>\d+\.\d+\.\d+(?:\.\d+)?)-qa\.exe$'
$script:QaAppId = '{B5D1A7C3-2E49-4F60-A8B1-7C3D9E5F2046}'
$script:ProductionAppId = '{A3B8C2D1-9E4F-4A7B-8C6D-1E2F3A4B5C6D}'
$versionFile = Join-Path $script:RepoRoot 'VERSION'
[IO.File]::WriteAllText($versionFile, '0.11.0' + [Environment]::NewLine, [Text.Encoding]::ASCII)
$script:FixtureRoot = Join-Path $script:RepoRoot 'QA fixture'
New-Item -ItemType Directory -Force -Path $script:FixtureRoot | Out-Null
$normalMissing = Join-Path $script:FixtureRoot '新建 QA\尚不存在'
Assert-NoReparsePointsWithin -Path $normalMissing -Boundary $script:FixtureRoot
Assert-True $true 'missing leaf still walks ancestors and accepts ordinary fixture path'
Assert-Throws { Assert-NoReparsePointsWithin -Path (Join-Path $script:RepoRoot 'outside') -Boundary $script:FixtureRoot } 'path outside fixture boundary rejected'

$outside = Join-Path $script:RepoRoot 'outside'
New-Item -ItemType Directory -Force -Path $outside | Out-Null
$link = Join-Path $script:FixtureRoot 'junction'
$junctionAvailable = $false
try {
    New-Item -ItemType Junction -Path $link -Target $outside | Out-Null
    $junctionAvailable = $true
} catch {
    if ($_.Exception.Message -notmatch 'privilege|not have sufficient|access is denied|symbolic link|junction') { throw }
    Write-Host "SKIP: junction checks unavailable on this runner: $($_.Exception.Message)"
}
if ($junctionAvailable) {
  try {
    Assert-Throws { Assert-NoReparsePointsWithin -Path (Join-Path $link 'missing') -Boundary $script:FixtureRoot } 'missing leaf beneath existing junction rejected'
    $script:FixtureRoot = Join-Path $link 'new fixture'
    Assert-Throws { Assert-FixtureRoot } 'FixtureRoot creation through junction rejected before creating target'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $outside 'new fixture'))) 'junction FixtureRoot target was not created'
  } finally {
    $script:FixtureRoot = Join-Path $script:RepoRoot 'QA fixture'
    if (Test-Path -LiteralPath $link) { Remove-Item -LiteralPath $link -Force }
  }
}

Assert-True ((Parse-QaPackageVersion '0.11.0').ToString() -eq '0.11.0') 'three-part numeric installer version accepted'
Assert-True ((Parse-QaPackageVersion '0.11.0.1').ToString() -eq '0.11.0.1') 'four-part synthetic QA version accepted'
foreach ($bad in @('0.11', '0.11.0.1.2', '0.11.0-beta', '0.11.x')) { Assert-Throws { Parse-QaPackageVersion $bad | Out-Null } "invalid package version rejected: $bad" }
Assert-QaManifestPackageVersion -PackageVersion '0.11.0' -BundleVersion '0.11.0' -SourceVersion '0.11.0' -PackageVersionKind $null
Assert-True $true 'legacy same-version manifest without packageVersionKind accepted'
Assert-QaManifestPackageVersion -PackageVersion '0.11.0' -BundleVersion '0.11.0' -SourceVersion '0.11.0' -PackageVersionKind 'bundle'
Assert-True $true 'explicit bundle packageVersionKind accepted'
Assert-QaManifestPackageVersion -PackageVersion '0.11.0.1' -BundleVersion '0.11.0' -SourceVersion '0.11.0' -PackageVersionKind 'synthetic-qa-sequence'
Assert-True $true 'same-baseline synthetic QA manifest accepted'
Assert-Throws { Assert-QaManifestPackageVersion '0.11.0.1' '0.11.0' '0.11.0' $null } 'synthetic version without synthetic kind rejected'
Assert-Throws { Assert-QaManifestPackageVersion '0.11.0.1' '0.11.0' '0.11.0' 'bundle' } 'synthetic version mislabeled as bundle rejected'
Assert-Throws { Assert-QaManifestPackageVersion '0.11.1.1' '0.11.0' '0.11.0' 'synthetic-qa-sequence' } 'synthetic version with different baseline rejected'
Assert-Throws { Assert-QaManifestPackageVersion '0.11.0.1' '0.11.0' '0.11.2' 'synthetic-qa-sequence' } 'synthetic version inconsistent with source rejected'

$installerFixtureDir = Join-Path $script:RepoRoot 'dist-installer'
New-Item -ItemType Directory -Force -Path $installerFixtureDir | Out-Null
$installFixtureRoot = Join-Path $script:RepoRoot 'installed-fixture'
$installFixtureBin = Join-Path $installFixtureRoot 'bin'
New-Item -ItemType Directory -Force -Path $installFixtureBin | Out-Null
$canonicalNames = @('dagents-node.exe', 'dagents-client.exe', 'dagents-browser.exe', 'dagents-shell.exe')
$canonicalPayloads = @()
foreach ($name in $canonicalNames) {
    $binaryPath = Join-Path $installFixtureBin $name
    [IO.File]::WriteAllText($binaryPath, "fixture-$name")
    $canonicalPayloads += [ordered]@{ name = $name; sha256 = Get-FileHashValue $binaryPath }
}
$legacyInstallerPath = Join-Path $installerFixtureDir 'dagents-local-assistant-windows-amd64-installer-0.11.0-qa.exe'
[IO.File]::WriteAllText($legacyInstallerPath, 'legacy qa installer')
$legacyManifest = [ordered]@{
    macro = 'IsolatedQaBuild=1'; appId = $script:QaAppId; version = '0.11.0'
    bundleVersion = '0.11.0'; installer = [IO.Path]::GetFileName($legacyInstallerPath)
    sha256 = Get-FileHashValue $legacyInstallerPath; payloads = $canonicalPayloads
}
[IO.File]::WriteAllText($legacyInstallerPath + '.manifest.json', ($legacyManifest | ConvertTo-Json -Depth 5), [Text.Encoding]::UTF8)
$legacyInstaller = Assert-QaInstaller -Path $legacyInstallerPath -ExpectedHash ''
Assert-True ($legacyInstaller.Version -eq '0.11.0') 'complete Assert-QaInstaller accepts legacy sidecar without packageVersion or packageVersionKind'
Assert-InstalledQaPayload $installFixtureRoot $legacyInstaller
Assert-True $true 'legacy sidecar passes exact four installed payload hash validation'

$syntheticInstallerPath = Join-Path $installerFixtureDir 'dagents-local-assistant-windows-amd64-installer-0.11.0.1-qa.exe'
[IO.File]::WriteAllText($syntheticInstallerPath, 'synthetic qa installer')
$syntheticManifest = [ordered]@{
    macro = 'IsolatedQaBuild=1'; appId = $script:QaAppId; version = '0.11.0.1'
    packageVersion = '0.11.0.1'; packageVersionKind = 'synthetic-qa-sequence'
    bundleVersion = '0.11.0'; installer = [IO.Path]::GetFileName($syntheticInstallerPath)
    sha256 = Get-FileHashValue $syntheticInstallerPath; payloads = $canonicalPayloads
}
[IO.File]::WriteAllText($syntheticInstallerPath + '.manifest.json', ($syntheticManifest | ConvertTo-Json -Depth 5), [Text.Encoding]::UTF8)
$syntheticInstaller = Assert-QaInstaller -Path $syntheticInstallerPath -ExpectedHash ''
Assert-True ($syntheticInstaller.Version -eq '0.11.0.1') 'complete Assert-QaInstaller accepts valid synthetic sidecar with packageVersion fields'
Assert-InstalledQaPayload $installFixtureRoot $syntheticInstaller
Assert-True $true 'synthetic sidecar passes exact four installed payload hash validation'

function Get-ItemProperty {
    param($LiteralPath, $Name, $ErrorAction)
    if ($LiteralPath -like '*Uninstall*') {
        return [pscustomobject]@{
            DisplayName = 'Production DAgents'
            DisplayVersion = '0.10.9'
            InstallLocation = 'C:\Production'
            UninstallString = 'C:\Production\unins000.exe'
            DisplayIcon = 'C:\Production\assets\dagents.ico'
        }
    }
    throw 'Test registry value is absent.'
}
$env:APPDATA = $script:RepoRoot
$partialUninstallSnapshot = Get-ProductionSnapshot
Assert-True ($partialUninstallSnapshot.productionUninstallKeySha256 -match '^[0-9a-f]{64}$') 'partial production uninstall key hashes successfully with StrictMode enabled'
Assert-True ($partialUninstallSnapshot.Values -notcontains 'Production DAgents') 'production uninstall snapshot exposes only hashes, not values'

$payloadRoot = Join-Path $script:RepoRoot 'payload-case'
$bin = Join-Path $payloadRoot 'bin'
New-Item -ItemType Directory -Force -Path $bin | Out-Null
$names = @('dagents-node.exe','dagents-client.exe','dagents-browser.exe','dagents-shell.exe')
$payloads = @()
foreach ($name in $names) { [IO.File]::WriteAllText((Join-Path $bin $name), "payload-$name"); $payloads += [pscustomobject]@{name=$name; sha256=(Get-FileHashValue (Join-Path $bin $name))} }
$installer = [pscustomobject]@{ Manifest = @{ payloads = $payloads } }
Assert-InstalledQaPayload $payloadRoot $installer
Assert-True $true 'canonical four payload manifest hashes match installed files'
[IO.File]::WriteAllText((Join-Path $bin 'dagents.exe'), 'alias')
Assert-Throws { Assert-InstalledQaPayload $payloadRoot $installer } 'installed executable alias rejected'
Remove-Item -LiteralPath (Join-Path $bin 'dagents.exe') -Force
$installer.Manifest.payloads = @($payloads | Where-Object name -ne 'dagents-shell.exe')
Assert-Throws { Assert-InstalledQaPayload $payloadRoot $installer } 'missing canonical payload rejected'
$installer.Manifest.payloads = @($payloads) + @($payloads[0])
Assert-Throws { Assert-InstalledQaPayload $payloadRoot $installer } 'duplicate canonical payload rejected'
$installer.Manifest.payloads = @($payloads[0..2]) + @([pscustomobject]@{name='dagents.exe';sha256=$payloads[3].sha256})
Assert-Throws { Assert-InstalledQaPayload $payloadRoot $installer } 'legacy alias rejected'
$installer.Manifest.payloads = @($payloads)
[IO.File]::WriteAllText((Join-Path $bin 'dagents-client.exe'), 'tampered')
Assert-Throws { Assert-InstalledQaPayload $payloadRoot $installer } 'installed payload hash mismatch rejected'
Write-Host "PASS: $script:passed isolated installer contract assertions"
$separatorChars = [char[]]@([char]92, [char]47)
$tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd($separatorChars)
$testAreaFull = [IO.Path]::GetFullPath($script:testArea).TrimEnd($separatorChars)
$expectedLeaf = [IO.Path]::GetFileName($testAreaFull)
$actualParent = [IO.Path]::GetFullPath((Split-Path -Parent $testAreaFull)).TrimEnd($separatorChars)
if (-not [string]::Equals($actualParent, $tempRoot, [StringComparison]::OrdinalIgnoreCase) -or
    $expectedLeaf -notmatch '^dagents-installer-contract-[0-9a-f]{32}$' -or
    [string]::Equals($testAreaFull, $tempRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Refusing cleanup because the test path is not the exact isolated contract temp directory.'
}
Assert-NoReparsePointsWithin -Path $testAreaFull -Boundary $tempRoot
$cleanupPending = New-Object 'System.Collections.Generic.Stack[string]'
$cleanupPending.Push($testAreaFull)
while ($cleanupPending.Count -gt 0) {
    $cleanupCurrent = $cleanupPending.Pop()
    foreach ($cleanupItem in @(Get-ChildItem -LiteralPath $cleanupCurrent -Force -ErrorAction Stop)) {
        if (($cleanupItem.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw 'Refusing cleanup because the test tree contains a reparse point.'
        }
        if ($cleanupItem.PSIsContainer) { $cleanupPending.Push($cleanupItem.FullName) }
    }
}
Remove-Item -LiteralPath $script:testArea -Recurse -Force
