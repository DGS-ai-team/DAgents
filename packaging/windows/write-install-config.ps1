# Create the Node bootstrap config from config.example.yaml.
# Runtime settings are managed by DAgents Desktop or the Node Setup API; the old
# install questionnaire JSON is retained as an ignored compatibility parameter.
param(
    [Parameter(Mandatory = $true)][string]$TemplatePath,
    [Parameter(Mandatory = $true)][string]$OutputPath,
    [string]$SettingsPath = '',
    [string]$LogPath = (Join-Path $env:TEMP 'dagents-write-install-config.log')
)

$ErrorActionPreference = 'Stop'

try {
    if (-not (Test-Path -LiteralPath $TemplatePath -PathType Leaf)) {
        throw "template not found: $TemplatePath"
    }
    if (Test-Path -LiteralPath $OutputPath -PathType Leaf) {
        Write-Host "[write-install-config] preserved existing Node bootstrap config at $OutputPath"
        return
    }

    $content = Get-Content -LiteralPath $TemplatePath -Raw -Encoding UTF8
    if ($content -notmatch '(?m)^listen:\s*(?:#.*)?$') {
        throw 'Node bootstrap template must contain a top-level listen block'
    }
    if ($content -match '(?m)^(?!listen:)[A-Za-z_][A-Za-z0-9_-]*:\s*(?:#.*)?$') {
        throw 'Node bootstrap template must contain only the listen block'
    }

    $outDir = Split-Path -Parent $OutputPath
    if ($outDir -and -not (Test-Path -LiteralPath $outDir)) {
        New-Item -ItemType Directory -Path $outDir -Force | Out-Null
    }
    $utf8NoBom = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText($OutputPath, $content, $utf8NoBom)
    if ($SettingsPath) {
        Add-Content -LiteralPath $LogPath -Value "Ignored legacy install settings: $SettingsPath" -Encoding UTF8
    }
    Write-Host "[write-install-config] wrote listen-only Node bootstrap to $OutputPath"
} catch {
    Add-Content -LiteralPath $LogPath -Value $_.Exception.Message -Encoding UTF8
    Write-Error $_.Exception.Message
    exit 1
}
