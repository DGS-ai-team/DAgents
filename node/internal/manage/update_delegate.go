package manage

import (
	"runtime"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/update"
)

const shellDesktopUpdatePath = "/v1/desktop/update"

// ShellDesktopAPIBase 为 Shell localhost Desktop API 基址（与 desktop/tray 默认一致）。
const ShellDesktopAPIBase = "http://127.0.0.1:18767"

// UpdateDelegatedToShell 表示 Node 不应再 poll Manage，更新检查改由 Shell 负责（F-ND2）。
func UpdateDelegatedToShell() bool {
	return runtime.GOOS == "windows"
}

// ShellDelegateUpdateStatus 为 Windows 上 deprecated 的 /v1/agent/update 响应。
func ShellDelegateUpdateStatus(channel string) update.Status {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = update.DefaultChannel
	}
	return update.Status{
		CurrentVersion:  version.Version,
		LatestVersion:   version.Version,
		ManageReachable: false,
		Channel:         channel,
		Platform:        ReleasePlatform(),
		ApplyCommand:    "dagents update",
		Deprecated:      true,
		Delegate:        "shell",
		DesktopAPI:      ShellDesktopAPIBase + shellDesktopUpdatePath,
		Message:         "Windows 桌面安装：更新检查已迁移至 Shell，请使用 dagents update 或 GET /v1/desktop/update",
	}
}
