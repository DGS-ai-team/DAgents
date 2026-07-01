# 根据安装向导 JSON 设置，基于 config.example.yaml 生成 config.yaml（保留注释块）。
# 须 UTF-8 BOM，供 Windows PowerShell 5.1 正确解析中文。
param(
    [Parameter(Mandatory = $true)][string]$TemplatePath,
    [Parameter(Mandatory = $true)][string]$OutputPath,
    [Parameter(Mandatory = $true)][string]$SettingsPath,
    [string]$LogPath = (Join-Path $env:TEMP 'dagents-write-install-config.log')
)

$ErrorActionPreference = 'Stop'

function Write-InstallConfigLog {
    param([string]$Message)
    $line = "[$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss')] $Message"
    Add-Content -LiteralPath $LogPath -Value $line -Encoding UTF8
}

function Set-ScalarLine {
    param([string]$Text, [string]$Pattern, [string]$Replacement)
    return [regex]::Replace($Text, $Pattern, $Replacement, 1)
}

function Bool-Yaml([bool]$v) {
    if ($v) { 'true' } else { 'false' }
}

try {
    if (-not (Test-Path -LiteralPath $TemplatePath)) {
        throw "template not found: $TemplatePath"
    }
    if (-not (Test-Path -LiteralPath $SettingsPath)) {
        throw "settings not found: $SettingsPath"
    }

    Write-InstallConfigLog "TemplatePath=$TemplatePath"
    Write-InstallConfigLog "OutputPath=$OutputPath"
    Write-InstallConfigLog "SettingsPath=$SettingsPath"

    $settings = Get-Content -LiteralPath $SettingsPath -Raw -Encoding UTF8 | ConvertFrom-Json
    $content = Get-Content -LiteralPath $TemplatePath -Raw -Encoding UTF8

    # --- llm ---
    $llm = $settings.llm
    $content = Set-ScalarLine $content '(?m)^  provider:\s*.*$' "  provider: $($llm.provider)"
    $content = Set-ScalarLine $content '(?m)^  base_url:\s*.*$' "  base_url: $($llm.base_url)"
    $content = Set-ScalarLine $content '(?m)^  model:\s*.*$' "  model: $($llm.model)"
    $content = Set-ScalarLine $content '(?m)^  api_key_env:\s*.*$' "  api_key_env: $($llm.api_key_env)"
    $content = Set-ScalarLine $content '(?m)^  mock:\s*.*$' "  mock: $(Bool-Yaml ([bool]$llm.mock))"

    # --- expose_to_peers ---
    $feat = $settings.features
    $content = Set-ScalarLine $content '(?m)^expose_to_peers:\s*.*$' "expose_to_peers: $(Bool-Yaml ([bool]$feat.expose_to_peers))"

    # --- manage ---
    $mg = $settings.manage
    if ([bool]$mg.enabled) {
        $manageBlock = @"
manage:
  enabled: true
  url: $($mg.url)
  registration:
    interval_seconds: 30
    ttl_seconds: 60
    team: $($mg.team)
"@
        if ($mg.registration_base_url -and ($mg.registration_base_url.ToString().Trim() -ne '')) {
            $manageBlock += "`n    base_url: $($mg.registration_base_url)"
        } else {
            $manageBlock += "`n    # base_url: http://192.168.1.10:18765"
        }
        $manageBlock += "`n  a2a:`n    enabled: $(Bool-Yaml ([bool]$mg.a2a_enabled))"
        $manageBlock += "`n    inbox_wait_seconds: 25"
        $manageBlock += "`n    inbox_poll_seconds: 30"
        $content = [regex]::Replace(
            $content,
            '(?ms)^manage:\r?\n(?:  .*\r?\n|# .*\r?\n)*',
            ($manageBlock + "`n"),
            1
        )
    } else {
        $disabledManage = @"
manage:
  enabled: false
  # --- Manage + A2A（见 docs/a2a-and-register-center.md、packaging/manage/README.md）---
  # url: http://127.0.0.1:8020
  # registration:
  #   base_url: http://192.168.1.10:18765
  #   interval_seconds: 30
  #   ttl_seconds: 60
  #   team: platform
  # a2a:
  #   enabled: true
  #   inbox_wait_seconds: 25
  #   inbox_poll_seconds: 30

"@
        $content = [regex]::Replace($content, '(?ms)^manage:\r?\n(?:  .*\r?\n|# .*\r?\n)*', $disabledManage, 1)
    }

    # --- feature toggles ---
    $feat = $settings.features
    $content = [regex]::Replace($content, '(?ms)^(skills:\r?\n  enabled:)\s*\S+', "`${1} $(Bool-Yaml ([bool]$feat.skills_enabled))", 1)
    $content = [regex]::Replace($content, '(?ms)^(triggers:\r?\n  enabled:)\s*\S+', "`${1} $(Bool-Yaml ([bool]$feat.triggers_enabled))", 1)
    $content = [regex]::Replace($content, '(?ms)^(child_agents:\r?\n  enabled:)\s*\S+', "`${1} $(Bool-Yaml ([bool]$feat.child_agents_enabled))", 1)
    $content = [regex]::Replace($content, '(?ms)^(ui:\r?\n  enabled:)\s*\S+', "`${1} $(Bool-Yaml ([bool]$feat.ui_enabled))", 1)

    # --- browser block ---
    $browserBlock = @"
# Browser 模式 A：browser-use 薄服务（见 docs/design/browser-remote-service-mode-a.md）
browser:
  enabled: $(Bool-Yaml ([bool]$feat.browser_enabled))
  service_url: http://127.0.0.1:18766
  headed: true
  chrome_path: ""
  cdp_url: ""
  debug_port: 9222
  default_timeout_ms: 30000
  output_dir: browser
  max_sessions: 1
  ignore_https_errors: false
"@
    if (-not [bool]$feat.browser_enabled) {
        $browserBlock = @"
# Browser 模式 A：browser-use 薄服务（见 docs/design/browser-remote-service-mode-a.md）
# browser:
#   enabled: false
#   service_url: http://127.0.0.1:18766
#   headed: true
#   chrome_path: ""
#   cdp_url: ""
#   debug_port: 9222
#   default_timeout_ms: 30000
#   output_dir: browser
#   max_sessions: 1
#   ignore_https_errors: false
"@
    }
    $content = [regex]::Replace(
        $content,
        '(?ms)^# Browser 模式 A：.*?(?=^# 多模态|\z)',
        ($browserBlock + "`n"),
        1
    )

    # --- multimodal ---
    $multiBlock = if ([bool]$feat.multimodal_enabled) {
        @"
# 多模态（vision 模型 + read_image；开启后 browser_* 自动切视觉模式）
multimodal:
  enabled: true
"@
    } else {
        @"
# 多模态（vision 模型 + read_image；开启后 browser_* 自动切视觉模式）
# multimodal:
#   enabled: false
"@
    }
    $content = [regex]::Replace(
        $content,
        '(?ms)^# 多模态.*',
        $multiBlock.TrimEnd(),
        1
    )

    # --- tools.enabled_groups ---
    $groups = @('fs', 'bash', 'hitl', 'skills', 'triggers', 'child_agents')
    if ([bool]$feat.browser_enabled) { $groups += 'browser' }
    if ([bool]$mg.enabled -and [bool]$mg.a2a_enabled) { $groups += 'a2a' }

    if ([bool]$feat.restrict_tool_groups) {
        $groupsYaml = "  enabled_groups:`n" + (($groups | ForEach-Object { "    - $_" }) -join "`n")
        $content = [regex]::Replace(
            $content,
            '(?ms)^  # enabled_groups:\r?\n(?:  #   - .*\r?\n)*',
            ($groupsYaml + "`n"),
            1
        )
    }

    $outDir = Split-Path -Parent $OutputPath
    if ($outDir -and -not (Test-Path -LiteralPath $outDir)) {
        New-Item -ItemType Directory -Path $outDir -Force | Out-Null
    }

    $utf8NoBom = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText($OutputPath, $content, $utf8NoBom)
    Write-InstallConfigLog "wrote $OutputPath"
    Write-Host "[write-install-config] wrote $OutputPath"
} catch {
    Write-InstallConfigLog $_.Exception.Message
    if ($_.ScriptStackTrace) {
        Write-InstallConfigLog $_.ScriptStackTrace
    }
    Write-Error $_.Exception.Message
    exit 1
}
