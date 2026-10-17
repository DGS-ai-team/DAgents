#Requires -Version 7.0
# This QA runner uses PowerShell 7 APIs; this does not raise the product's Windows/PowerShell requirements.
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Install', 'Seed', 'Upgrade', 'Uninstall', 'Verify')]
    [string]$Stage,

    [Parameter(Mandatory = $true)]
    [string]$FixtureRoot,

    [string]$InstallerPath,
    [string]$InstallRoot
)

# Run one stage at a time. Install and Seed are followed by manual Node setup;
# Upgrade and Uninstall intentionally let the real installer lifecycle hooks
# quit Desktop and stop the fixture-owned Node.

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$QaAppId = '{B5D1A7C3-2E49-4F60-A8B1-7C3D9E5F2046}'
$ProductionAppId = '{A3B8C2D1-9E4F-4A7B-8C6D-1E2F3A4B5C6D}'
$QaEndpoint = 'http://127.0.0.1:18972'
$ExpectedInstallerName = '^dagents-local-assistant-windows-(?<platform>amd64|386)-installer-(?<version>\d+\.\d+\.\d+\.\d+)-qa\.exe$'
$RepoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$FixtureRoot = [IO.Path]::GetFullPath($FixtureRoot)

function Test-PathWithin {
    param([string]$Parent, [string]$Child)
    $relative = [IO.Path]::GetRelativePath($Parent, $Child)
    return -not [IO.Path]::IsPathRooted($relative) -and
        $relative -ne '..' -and
        -not $relative.StartsWith('..' + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase) -and
        -not $relative.StartsWith('..' + [IO.Path]::AltDirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)
}

function Get-TextHash {
    param([AllowNull()][string]$Value)
    if ($null -eq $Value) { return 'missing' }
    $bytes = [Text.Encoding]::UTF8.GetBytes($Value)
    $sha = [Security.Cryptography.SHA256]::Create()
    try { return ([BitConverter]::ToString($sha.ComputeHash($bytes))).Replace('-', '').ToLowerInvariant() }
    finally { $sha.Dispose() }
}

function Get-FileHashValue {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return 'missing' }
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function Get-ProductionSnapshot {
    $runValue = $null
    try {
        $runValue = (Get-ItemProperty -LiteralPath 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' -Name 'DAgents Shell' -ErrorAction Stop).'DAgents Shell'
    } catch { }

    $userPath = $null
    try {
        $userPath = (Get-ItemProperty -LiteralPath 'HKCU:\Environment' -Name 'Path' -ErrorAction Stop).Path
    } catch { }

    $desktopRoot = [Environment]::GetFolderPath([Environment+SpecialFolder]::Desktop)
    $desktopShortcut = Join-Path $desktopRoot 'DAgents Desktop.lnk'
    $legacyDesktopShortcut = Join-Path $desktopRoot 'DAgents Desktop（系统托盘）.lnk'
    $productionSettings = Join-Path $env:APPDATA 'DAgents\desktop\settings.json'
    return [ordered]@{
        runValueSha256 = Get-TextHash $runValue
        userPathSha256 = Get-TextHash $userPath
        desktopShortcutSha256 = Get-FileHashValue $desktopShortcut
        legacyDesktopShortcutSha256 = Get-FileHashValue $legacyDesktopShortcut
        desktopSettingsSha256 = Get-FileHashValue $productionSettings
    }
}

function Assert-ProductionSnapshot {
    param($Expected)
    $current = Get-ProductionSnapshot
    foreach ($key in @('runValueSha256', 'userPathSha256', 'desktopShortcutSha256', 'legacyDesktopShortcutSha256', 'desktopSettingsSha256')) {
        if ($current[$key] -ne $Expected[$key]) {
            throw "Production-side-effect guard failed: $key changed. Snapshot stores hashes only."
        }
    }
}

function Get-FixtureStatePath { return Join-Path $FixtureRoot '.isolated-installer-qa-state.json' }

function Read-State {
    $path = Get-FixtureStatePath
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "QA state file not found: $path" }
    return Get-Content -LiteralPath $path -Raw -Encoding UTF8 | ConvertFrom-Json -AsHashtable
}

function Write-State {
    param($State)
    $json = $State | ConvertTo-Json -Depth 8
    [IO.File]::WriteAllText((Get-FixtureStatePath), $json + [Environment]::NewLine, (New-Object Text.UTF8Encoding $false))
}

function Resolve-InstallRoot {
    if ($InstallRoot) {
        $resolved = [IO.Path]::GetFullPath($InstallRoot)
    } else {
        $resolved = [IO.Path]::GetFullPath((Join-Path $FixtureRoot 'DAgents QA 安装 Fixture'))
    }
    if (-not (Test-PathWithin $FixtureRoot $resolved) -or $resolved -eq $FixtureRoot) {
        throw 'InstallRoot must be a child of FixtureRoot.'
    }
    if ($resolved -notmatch '[ ]' -or $resolved -notmatch '[\u3400-\u9FFF]') {
        throw 'InstallRoot must contain both a space and Chinese characters.'
    }
    return $resolved
}

function Assert-FixtureRoot {
    if (-not (Test-PathWithin $RepoRoot $FixtureRoot) -or $FixtureRoot -eq $RepoRoot) {
        throw "FixtureRoot must be a child of this workspace: $RepoRoot"
    }
    if (-not (Test-Path -LiteralPath $FixtureRoot -PathType Container)) {
        New-Item -ItemType Directory -Path $FixtureRoot -Force | Out-Null
    }
    Assert-NoReparsePointsWithin $FixtureRoot
}

function Assert-NoReparsePointsWithin {
    param([string]$Path)
    $current = [IO.Path]::GetFullPath($Path)
    while (Test-Path -LiteralPath $current) {
        $item = Get-Item -LiteralPath $current -Force
        if (($item.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            throw "Refusing fixture path containing a symbolic link or junction: $current"
        }
        if ($current.TrimEnd('\', '/') -eq $RepoRoot.TrimEnd('\', '/')) { break }
        $parent = Split-Path -Parent $current
        if (-not $parent -or -not (Test-PathWithin $RepoRoot $parent)) { break }
        $current = $parent
    }
}

function Assert-QaInstaller {
    param([string]$Path, [string]$ExpectedHash)
    if ([string]::IsNullOrWhiteSpace($Path)) { throw 'InstallerPath is required for this stage.' }
    $resolved = (Resolve-Path -LiteralPath $Path -ErrorAction Stop).Path
    if (-not (Test-Path -LiteralPath $resolved -PathType Leaf)) { throw "Installer not found: $Path" }
    if (-not (Test-PathWithin $RepoRoot $resolved)) { throw 'Installer must be a build artifact inside this workspace.' }
    Assert-NoReparsePointsWithin $resolved
    $name = [IO.Path]::GetFileName($resolved)
    if ($name -notmatch $ExpectedInstallerName) { throw "Refusing non-QA or unexpected installer name: $name" }
    $nameVersion = $Matches.version
    $namePlatform = $Matches.platform

    $manifestPath = $resolved + '.manifest.json'
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw "QA build manifest required beside installer: $manifestPath" }
    Assert-NoReparsePointsWithin $manifestPath
    $manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json -AsHashtable
    if ($manifest.macro -ne 'IsolatedQaBuild=1' -or $manifest.appId -ne $QaAppId -or
        [string]$manifest.version -cne $nameVersion -or
        $manifest.installer -ne $name -or $manifest.appId -eq $ProductionAppId) {
        throw 'Installer manifest does not identify the isolated QA build and expected QA AppId.'
    }
    $actualHash = Get-FileHashValue $resolved
    if ($actualHash -ne ([string]$manifest.sha256).ToLowerInvariant()) {
        throw 'Installer SHA-256 does not match its QA build manifest.'
    }
    if ($ExpectedHash -and $actualHash -ne $ExpectedHash) {
        throw 'Installer changed since the isolated fixture was installed.'
    }
    $version = [Version]::Parse($nameVersion)
    return [pscustomobject]@{ Path = $resolved; Name = $name; Sha256 = $actualHash; Version = $version.ToString(); Platform = $namePlatform; ManifestPath = $manifestPath }
}

function Get-QaUninstallEntry {
    param([string]$Root)
    $key = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\' + $QaAppId + '_is1'
    if (-not (Test-Path -LiteralPath $key)) { throw 'The expected isolated QA uninstall registry entry is missing.' }
    $entry = Get-ItemProperty -LiteralPath $key
    $location = [IO.Path]::GetFullPath([string]$entry.InstallLocation).TrimEnd('\', '/')
    $expectedRoot = [IO.Path]::GetFullPath($Root).TrimEnd('\', '/')
    if (-not [string]::Equals($location, $expectedRoot, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'The QA uninstall registry entry points outside this fixture install root.'
    }
    $uninstaller = Join-Path $expectedRoot 'unins000.exe'
    if (-not (Test-Path -LiteralPath $uninstaller -PathType Leaf)) { throw 'The fixture uninstaller is missing.' }
    $uninstallString = [string]$entry.UninstallString
    if ($uninstallString -notmatch '(?i)"(?<quoted>[^"]*unins000\.exe)"|(?<bare>\S*unins000\.exe)') {
        throw 'The QA uninstall registry entry does not identify this fixture uninstaller.'
    }
    $uninstallerInRegistry = if ($Matches.quoted) { $Matches.quoted } else { $Matches.bare }
    if (-not [string]::Equals([IO.Path]::GetFullPath($uninstallerInRegistry), $uninstaller, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'The QA uninstall registry entry does not identify this fixture uninstaller.'
    }
    return [ordered]@{
        registryKey = $key
        displayVersion = [string]$entry.DisplayVersion
        uninstallStringSha256 = Get-TextHash $uninstallString
        uninstallerSha256 = Get-FileHashValue $uninstaller
    }
}

function Assert-QaUninstallIdentity {
    param([string]$Root, $ExpectedVersion, $ExpectedUninstallerHash)
    $entry = Get-QaUninstallEntry $Root
    if ($entry.displayVersion -ne $ExpectedVersion) { throw 'The installed QA version no longer matches fixture state.' }
    if ($entry.uninstallerSha256 -ne $ExpectedUninstallerHash) { throw 'The installed fixture uninstaller changed since state was recorded.' }
    return $entry
}

function Get-NodeSettingsLogicalHash {
    param([string]$Root)
    $database = Join-Path $Root '.runtime\node_settings.db'
    if (-not (Test-Path -LiteralPath $database -PathType Leaf)) { throw 'Node must create .runtime/node_settings.db through its real setup flow before upgrade or uninstall.' }
    $python = Get-Command py, python3, python -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $python) { throw 'A Python runtime with the standard-library sqlite3 module is required for a read-only logical database comparison; no tools will be installed.' }
    $scriptPath = Join-Path $FixtureRoot ('.qa-sqlite-digest-' + [Guid]::NewGuid().ToString('N') + '.py')
    $stdout = $scriptPath + '.out'
    $stderr = $scriptPath + '.err'
    $script = @'
import hashlib, json, sqlite3, sys
uri = "file:" + __import__("urllib.parse", fromlist=["quote"]).quote(sys.argv[1].replace("\\", "/"), safe="/:") + "?mode=ro"
conn = sqlite3.connect(uri, uri=True)
try:
    conn.execute("BEGIN")
    tables = [row[0] for row in conn.execute("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")]
    payload = []
    for table in tables:
        quoted = '"' + table.replace('"', '""') + '"'
        schema = conn.execute("SELECT sql FROM sqlite_master WHERE type='table' AND name=?", (table,)).fetchone()[0]
        rows = []
        for row in conn.execute("SELECT * FROM " + quoted):
            rows.append([{"bytes": value.hex()} if isinstance(value, bytes) else value for value in row])
        rows.sort(key=lambda row: json.dumps(row, sort_keys=True, separators=(",", ":"), ensure_ascii=False))
        payload.append({"name": table, "schema": schema, "rows": rows})
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode("utf-8")
    print(hashlib.sha256(encoded).hexdigest())
finally:
    conn.close()
'@
    [IO.File]::WriteAllText($scriptPath, $script, (New-Object Text.UTF8Encoding $false))
    try {
        $pythonArgs = if ($python.Name -ieq 'py.exe' -or $python.Name -ieq 'py') { '-3 "' + $scriptPath + '" "' + $database + '"' } else { '"' + $scriptPath + '" "' + $database + '"' }
        $process = Start-Process -FilePath $python.Source -ArgumentList $pythonArgs -Wait -PassThru -WindowStyle Hidden -RedirectStandardOutput $stdout -RedirectStandardError $stderr
        if ($process.ExitCode -ne 0) { throw 'Read-only SQLite logical dump failed; database contents were not printed.' }
        $digest = (Get-Content -LiteralPath $stdout -Raw -Encoding ASCII).Trim()
        if ($digest -notmatch '^[0-9a-f]{64}$') { throw 'SQLite logical dump did not return a SHA-256 digest.' }
        return $digest
    } finally {
        Remove-Item -LiteralPath $scriptPath, $stdout, $stderr -Force -ErrorAction SilentlyContinue
    }
}

function Assert-InstalledQaFiles {
    param([string]$Root)
    Assert-NoReparsePointsWithin $Root
    foreach ($relative in @('bin\dagents-node.exe', 'bin\dagents-client.exe', 'bin\dagents-browser.exe', 'bin\dagents-shell.exe', 'dagents.cmd', 'config.yaml', 'lifecycle-protocol-v1.marker', 'unins000.exe')) {
        if (-not (Test-Path -LiteralPath (Join-Path $Root $relative) -PathType Leaf)) {
            throw "Expected QA installer payload is missing: $relative"
        }
    }
    if (-not (Test-Path -LiteralPath (Join-Path $Root '.runtime\policy\tool.approval.txt') -PathType Leaf)) {
        throw 'Installer did not seed the runtime policy file.'
    }
}

function Assert-QaBootstrapConfig {
    param([string]$Root)
    $path = Join-Path $Root 'config.yaml'
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw 'The QA Node bootstrap config is missing.' }
    $text = Get-Content -LiteralPath $path -Raw -Encoding UTF8
    $meaningful = @($text -split '\r?\n' | Where-Object { $_.Trim() -and $_ -notmatch '^\s*#' })
    $topLevel = @($meaningful | Where-Object { $_ -notmatch '^\s' })
    if ($topLevel.Count -ne 1 -or $topLevel[0] -notmatch '^listen:\s*(?:#.*)?$') {
        throw 'QA config must remain a listen-only bootstrap with no additional top-level settings.'
    }
    $values = @{}
    foreach ($line in @($meaningful | Where-Object { $_ -match '^\s' })) {
        if ($line -notmatch '^\s+(host|port):\s*([^#]+?)\s*(?:#.*)?$') {
            throw 'QA listen bootstrap contains an unexpected or nested setting.'
        }
        $key = $Matches[1]
        if ($values.ContainsKey($key)) { throw "QA listen bootstrap contains duplicate $key settings." }
        $values[$key] = $Matches[2].Trim().Trim('"', "'")
    }
    if ($values.Count -ne 2 -or $values.host -ne '127.0.0.1' -or $values.port -ne '18972') {
        throw 'QA listen bootstrap must keep host 127.0.0.1 and port 18972.'
    }
}

function Get-PersistedFixtureHashes {
    param([string]$Root, [bool]$IncludeBootstrapConfig = $true)
    $relativePaths = @(
        '.runtime\qa-installer-sentinel.txt',
        '.runtime\policy\tool.approval.txt',
        'qa-desktop-settings.json'
    )
    if ($IncludeBootstrapConfig) { $relativePaths = @('config.yaml') + $relativePaths }
    $hashes = [ordered]@{}
    foreach ($relative in $relativePaths) {
        $path = Join-Path $Root $relative
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "Expected persistent fixture file is missing: $relative" }
        $key = $relative.Replace('\', '/')
        $hashes[$key] = Get-FileHashValue $path
    }
    return $hashes
}

function Assert-HashesEqual {
    param($Expected, $Actual, [string]$Operation)
    foreach ($key in $Expected.Keys) {
        if (-not $Actual.Contains($key) -or $Actual[$key] -ne $Expected[$key]) {
            throw "$Operation changed or removed preserved fixture data: $key"
        }
    }
}

function Invoke-HiddenInstaller {
    param([string]$Path, [string]$Arguments, [string]$FixtureHome, [string]$DesktopConfig)
    $oldHome = $env:DAGENTS_HOME
    $oldDesktop = $env:DAGENTS_DESKTOP_CONFIG
    try {
        $env:DAGENTS_HOME = $FixtureHome
        $env:DAGENTS_DESKTOP_CONFIG = $DesktopConfig
        $process = Start-Process -FilePath $Path -ArgumentList $Arguments -Wait -PassThru -WindowStyle Hidden
        if ($process.ExitCode -ne 0) { throw "QA installer/uninstaller exited with code $($process.ExitCode)." }
    } finally {
        $env:DAGENTS_HOME = $oldHome
        $env:DAGENTS_DESKTOP_CONFIG = $oldDesktop
    }
}

function Invoke-ShellJson {
    param([string]$Root, [string]$Command)
    $shell = Join-Path $Root 'bin\dagents-shell.exe'
    $config = Join-Path $Root 'config.yaml'
    $desktop = Join-Path $Root 'qa-desktop-settings.json'
    $token = [Guid]::NewGuid().ToString('N')
    $stdout = Join-Path $FixtureRoot ".qa-cli-$token.out"
    $stderr = Join-Path $FixtureRoot ".qa-cli-$token.err"
    $oldHome = $env:DAGENTS_HOME
    $oldDesktop = $env:DAGENTS_DESKTOP_CONFIG
    try {
        $env:DAGENTS_HOME = $Root
        $env:DAGENTS_DESKTOP_CONFIG = $desktop
        $arguments = "$Command --config `"$config`" --desktop-config `"$desktop`""
        $process = Start-Process -FilePath $shell -ArgumentList $arguments -Wait -PassThru -WindowStyle Hidden -RedirectStandardOutput $stdout -RedirectStandardError $stderr
        $output = if (Test-Path -LiteralPath $stdout) { Get-Content -LiteralPath $stdout -Raw -Encoding UTF8 } else { '' }
        if ($process.ExitCode -ne 0) { throw "Shell $Command failed with exit code $($process.ExitCode)." }
        return ($output | ConvertFrom-Json)
    } finally {
        $env:DAGENTS_HOME = $oldHome
        $env:DAGENTS_DESKTOP_CONFIG = $oldDesktop
        Remove-Item -LiteralPath $stdout, $stderr -Force -ErrorAction SilentlyContinue
    }
}

function Assert-OwnedNodeRunning {
    param([string]$Root)
    $status = Invoke-ShellJson $Root '--owned-node-status'
    if (-not $status.owned_process -or -not $status.endpoint_ready -or -not $status.managed) {
        throw 'Expected a live Node owned by this QA install at the fixture endpoint. Start it manually with this install config before continuing.'
    }
}

function Assert-DesktopRunning {
    param([string]$Root)
    $status = Invoke-ShellJson $Root '--desktop-status'
    if (-not $status.running) {
        throw 'Start the fixture Desktop before continuing so the installer exercises its real quit hook.'
    }
}

function Assert-DesktopStopped {
    param([string]$Root)
    $status = Invoke-ShellJson $Root '--desktop-status'
    if ($status.running) { throw 'The installer did not quit this fixture Desktop.' }
}

function Assert-OwnedNodeStopped {
    param([string]$Root)
    $status = Invoke-ShellJson $Root '--owned-node-status'
    if ($status.owned_process -or $status.managed) { throw 'The installer did not stop this fixture-owned Node.' }
    if ($status.endpoint_ready) { throw 'The fixture endpoint still responds after the installer stopped its owned Node.' }
}

Assert-FixtureRoot
$targetRoot = Resolve-InstallRoot
if (Test-Path -LiteralPath $targetRoot) { Assert-NoReparsePointsWithin $targetRoot }
$statePath = Get-FixtureStatePath

switch ($Stage) {
    'Install' {
        $installer = Assert-QaInstaller $InstallerPath ''
        $previousAttempt = $null
        if (Test-Path -LiteralPath $statePath -PathType Leaf) {
            $previousAttempt = Read-State
            if ($previousAttempt.phase -ne 'failed-after-attempt' -or $previousAttempt.attemptedStage -ne 'Install') {
                throw "Fixture state already exists; refusing to overwrite: $statePath"
            }
            if (Test-Path -LiteralPath $targetRoot) {
                throw "Previous Install failed and InstallRoot still exists. Inspect its install-inno.log, then remove only this verified empty fixture directory manually before retrying: $targetRoot"
            }
        } elseif (Test-Path -LiteralPath $targetRoot) {
            throw "InstallRoot already exists; refusing to install over existing data: $targetRoot"
        }
        $qaUninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\' + $QaAppId + '_is1'
        if (Test-Path -LiteralPath $qaUninstallKey) { throw 'An isolated QA package is already registered for this user; refusing to let Inno treat this as an upgrade.' }
        $snapshot = Get-ProductionSnapshot
        $installLog = Join-Path $FixtureRoot 'install-inno.log'
        $argumentList = '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /DIR="' + $targetRoot + '" /LOG="' + $installLog + '"'
        try {
            Invoke-HiddenInstaller $installer.Path $argumentList $targetRoot (Join-Path $targetRoot 'qa-desktop-settings.json')
            Assert-InstalledQaFiles $targetRoot
            $uninstallEntry = Get-QaUninstallEntry $targetRoot
            if ($uninstallEntry.displayVersion -ne $installer.Version) { throw 'Installed QA DisplayVersion does not match the selected installer package version.' }
            Assert-ProductionSnapshot $snapshot
        } catch {
            $attemptHistory = @()
            if ($previousAttempt -and $previousAttempt.attemptHistory) { $attemptHistory += @($previousAttempt.attemptHistory) }
            $attemptHistory += [ordered]@{ stage = 'Install'; installerName = $installer.Name; installerSha256 = $installer.Sha256; version = $installer.Version; logPath = $installLog; failure = $_.Exception.Message }
            Write-State ([ordered]@{
                phase = 'failed-after-attempt'
                attemptedStage = 'Install'
                fixtureRoot = $FixtureRoot
                installRoot = $targetRoot
                qaAppId = $QaAppId
                attemptHistory = $attemptHistory
                productionSnapshotHashes = $snapshot
            })
            throw
        }
        $state = [ordered]@{
            phase = 'installed'
            fixtureRoot = $FixtureRoot
            installRoot = $targetRoot
            installerName = $installer.Name
            installerSha256 = $installer.Sha256
            installedVersion = $installer.Version
            installedPlatform = $installer.Platform
            installedUninstallerSha256 = $uninstallEntry.uninstallerSha256
            qaAppId = $QaAppId
            productionSnapshotHashes = $snapshot
        }
        if ($previousAttempt -and $previousAttempt.attemptHistory) { $state.attemptHistory = @($previousAttempt.attemptHistory) }
        Write-State $state
        Write-Host "Installed isolated QA package into the fixture: $targetRoot"
    }

    'Seed' {
        $state = Read-State
        if ($state.phase -ne 'installed' -or [IO.Path]::GetFullPath($state.installRoot) -ne $targetRoot) { throw 'Seed requires the matching freshly installed QA fixture.' }
        Assert-InstalledQaFiles $targetRoot
        $configPath = Join-Path $targetRoot 'config.yaml'
        $configText = Get-Content -LiteralPath $configPath -Raw -Encoding UTF8
        if ($configText -notmatch '(?m)^listen:\s*(?:#.*)?$' -or $configText -match '(?m)^(?!listen:)[A-Za-z_][A-Za-z0-9_-]*:\s*(?:#.*)?$') {
            throw 'Installed config.yaml is not a listen-only bootstrap config.'
        }
        $updatedConfig = [regex]::Replace($configText, '(?m)^(\s*port:\s*)\d+(\s*(?:#.*)?)$', '${1}18972${2}', 1)
        if ($updatedConfig -eq $configText -or $updatedConfig -notmatch '(?m)^\s*port:\s*18972\s*$') { throw 'Could not set the fixture-only Node port to 18972.' }
        [IO.File]::WriteAllText($configPath, $updatedConfig, (New-Object Text.UTF8Encoding $false))
        Assert-QaBootstrapConfig $targetRoot

        $sentinel = Join-Path $targetRoot '.runtime\qa-installer-sentinel.txt'
        [IO.File]::WriteAllText($sentinel, "isolated-installer-qa`n", (New-Object Text.UTF8Encoding $false))
        $policyPath = Join-Path $targetRoot '.runtime\policy\tool.approval.txt'
        $policyText = Get-Content -LiteralPath $policyPath -Raw -Encoding UTF8
        $policyMarker = '# Isolated installer QA policy edit; preserve across upgrade and uninstall.'
        if ($policyText -notlike "*$policyMarker*") {
            [IO.File]::AppendAllText($policyPath, "`r`n$policyMarker`r`n", (New-Object Text.UTF8Encoding $false))
        }
        $desktopSettings = [ordered]@{
            connection_mode = 'local'
            node_endpoint = $QaEndpoint
            notification_enabled = $false
            update_check_enabled = $false
            update_check_interval_seconds = 21600
        } | ConvertTo-Json
        [IO.File]::WriteAllText((Join-Path $targetRoot 'qa-desktop-settings.json'), $desktopSettings + [Environment]::NewLine, (New-Object Text.UTF8Encoding $false))

        $state.phase = 'seeded'
        $state.seedHashes = Get-PersistedFixtureHashes $targetRoot $false
        Assert-ProductionSnapshot $state.productionSnapshotHashes
        Write-State $state
        Write-Host 'Seeded fixture sentinel, modified policy, listen-only bootstrap, and isolated Desktop JSON (no credentials).'
        Write-Host 'Start and initialize the QA Node manually before Upgrade; keep its endpoint at 127.0.0.1:18972.'
    }

    'Upgrade' {
        $state = Read-State
        $installer = Assert-QaInstaller $InstallerPath ''
        if ($state.phase -ne 'seeded' -or [IO.Path]::GetFullPath($state.installRoot) -ne $targetRoot) { throw 'Upgrade requires the matching seeded QA fixture.' }
        if ($state.qaAppId -ne $QaAppId) { throw 'Fixture state does not belong to the isolated QA AppId.' }
        if ($state.installedPlatform -ne $installer.Platform) { throw 'Upgrade candidate architecture differs from the installed fixture package.' }
        if ([Version]::Parse($installer.Version) -le [Version]::Parse($state.installedVersion)) {
            throw 'Upgrade candidate must have a strictly greater four-part QA version than the currently installed package.'
        }
        Assert-InstalledQaFiles $targetRoot
        Assert-QaUninstallIdentity $targetRoot $state.installedVersion $state.installedUninstallerSha256 | Out-Null
        Assert-OwnedNodeRunning $targetRoot
        Assert-DesktopRunning $targetRoot
        Assert-QaBootstrapConfig $targetRoot
        $before = Get-PersistedFixtureHashes $targetRoot
        $fixedSeedKeys = @('.runtime/qa-installer-sentinel.txt', '.runtime/policy/tool.approval.txt', 'qa-desktop-settings.json')
        $fixedSeedHashes = [ordered]@{}
        foreach ($key in $fixedSeedKeys) {
            if (-not $state.seedHashes.Contains($key)) { throw "Fixture state is missing seeded persistence hash: $key" }
            $fixedSeedHashes[$key] = $state.seedHashes[$key]
        }
        Assert-HashesEqual $fixedSeedHashes $before 'Pre-upgrade verification'
        $databaseBefore = Get-NodeSettingsLogicalHash $targetRoot
        $snapshot = $state.productionSnapshotHashes
        $upgradeLog = Join-Path $FixtureRoot 'upgrade-inno.log'
        $argumentList = '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /DIR="' + $targetRoot + '" /LOG="' + $upgradeLog + '"'
        try {
            Invoke-HiddenInstaller $installer.Path $argumentList $targetRoot (Join-Path $targetRoot 'qa-desktop-settings.json')
            Assert-InstalledQaFiles $targetRoot
            Assert-DesktopStopped $targetRoot
            Assert-OwnedNodeStopped $targetRoot
            $after = Get-PersistedFixtureHashes $targetRoot
            Assert-HashesEqual $before $after 'Upgrade'
            $databaseAfter = Get-NodeSettingsLogicalHash $targetRoot
            if ($databaseBefore -ne $databaseAfter) { throw 'Upgrade changed the logical Node settings database contents.' }
            $uninstallEntry = Get-QaUninstallEntry $targetRoot
            if ($uninstallEntry.displayVersion -ne $installer.Version) { throw 'Upgrade did not update the QA uninstall registry version to the candidate package.' }
            Assert-ProductionSnapshot $snapshot
        } catch {
            $state.phaseBeforeFailure = $state.phase
            $state.phase = 'failed-after-attempt'
            $state.attemptedStage = 'Upgrade'
            $state.upgradeInstallerName = $installer.Name
            $state.upgradeInstallerSha256 = $installer.Sha256
            $state.attemptLogPath = $upgradeLog
            $state.failure = $_.Exception.Message
            Write-State $state
            throw
        }
        $state.phase = 'upgraded'
        $state.upgradeHashes = $after
        $state.upgradeDatabaseLogicalSha256 = $databaseAfter
        $state.upgradeInstallerName = $installer.Name
        $state.upgradeInstallerSha256 = $installer.Sha256
        $state.installedVersion = $installer.Version
        $state.installedUninstallerSha256 = $uninstallEntry.uninstallerSha256
        $state.installerName = $installer.Name
        $state.installerSha256 = $installer.Sha256
        Write-State $state
        Write-Host 'QA upgrade passed: a newer manifest-verified package ran installer-owned quit/stop, fixture files and logical Node database persisted, and production settings stayed unchanged.'
        Write-Host 'Manually restart the QA-owned Node and Desktop before running Stage Uninstall.'
    }

    'Uninstall' {
        $state = Read-State
        if ($state.phase -ne 'upgraded' -or [IO.Path]::GetFullPath($state.installRoot) -ne $targetRoot) { throw 'Uninstall requires the matching upgraded QA fixture.' }
        if ($state.qaAppId -ne $QaAppId) { throw 'Fixture state does not belong to the isolated QA AppId.' }
        Assert-InstalledQaFiles $targetRoot
        Assert-QaUninstallIdentity $targetRoot $state.installedVersion $state.installedUninstallerSha256 | Out-Null
        Assert-OwnedNodeRunning $targetRoot
        Assert-DesktopRunning $targetRoot
        $before = Get-PersistedFixtureHashes $targetRoot
        Assert-HashesEqual $state.upgradeHashes $before 'Pre-uninstall verification'
        $databaseBefore = Get-NodeSettingsLogicalHash $targetRoot
        if ($databaseBefore -ne $state.upgradeDatabaseLogicalSha256) { throw 'Node settings logical contents changed after upgrade verification.' }
        Assert-ProductionSnapshot $state.productionSnapshotHashes
        $uninstaller = Join-Path $targetRoot 'unins000.exe'
        $uninstallLog = Join-Path $FixtureRoot 'uninstall-inno.log'
        try {
            Invoke-HiddenInstaller $uninstaller ('/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /LOG="' + $uninstallLog + '"') $targetRoot (Join-Path $targetRoot 'qa-desktop-settings.json')
            foreach ($relative in @('bin\dagents-shell.exe', 'bin\dagents-node.exe', 'bin\dagents-client.exe', 'bin\dagents-browser.exe', 'dagents.cmd', 'unins000.exe')) {
                if (Test-Path -LiteralPath (Join-Path $targetRoot $relative)) { throw "Uninstall left application binary behind: $relative" }
            }
            $uninstallKey = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\' + $QaAppId + '_is1'
            if (Test-Path -LiteralPath $uninstallKey) { throw 'Uninstall left the isolated QA product registry entry behind.' }
            $after = Get-PersistedFixtureHashes $targetRoot
            Assert-HashesEqual $before $after 'Uninstall'
            $databaseAfter = Get-NodeSettingsLogicalHash $targetRoot
            if ($databaseBefore -ne $databaseAfter) { throw 'Uninstall changed the logical Node settings database contents.' }
            Assert-ProductionSnapshot $state.productionSnapshotHashes
        } catch {
            $state.phaseBeforeFailure = $state.phase
            $state.phase = 'failed-after-attempt'
            $state.attemptedStage = 'Uninstall'
            $state.attemptLogPath = $uninstallLog
            $state.failure = $_.Exception.Message
            Write-State $state
            throw
        }
        $state.phase = 'uninstalled'
        $state.uninstallHashes = $after
        $state.uninstallDatabaseLogicalSha256 = $databaseAfter
        Write-State $state
        Write-Host 'QA uninstall passed: binaries were removed while bootstrap, .runtime database/policy/sentinel, and local Desktop JSON remained unchanged.'
        Write-Host 'For a final persistence check, run Stage Verify.'
    }

    'Verify' {
        $state = Read-State
        if ($state.phase -ne 'uninstalled' -or [IO.Path]::GetFullPath($state.installRoot) -ne $targetRoot) { throw 'Verify requires a completed QA uninstall.' }
        foreach ($relative in @('bin\dagents-shell.exe', 'bin\dagents-node.exe', 'dagents.cmd', 'unins000.exe')) {
            if (Test-Path -LiteralPath (Join-Path $targetRoot $relative)) { throw "Uninstalled fixture still contains an application file: $relative" }
        }
        $after = Get-PersistedFixtureHashes $targetRoot
        Assert-HashesEqual $state.uninstallHashes $after 'Final verification'
        $databaseAfter = Get-NodeSettingsLogicalHash $targetRoot
        if ($databaseAfter -ne $state.uninstallDatabaseLogicalSha256) { throw 'Final verification found changed logical Node settings contents.' }
        Assert-ProductionSnapshot $state.productionSnapshotHashes
        Write-Host 'Final isolated installer verification passed; production settings were compared by hash only.'
    }
}
