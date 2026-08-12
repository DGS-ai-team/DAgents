//go:build windows

package notify

import (
	"fmt"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/webui"
	toast "github.com/go-toast/toast"
)

// PushUpdateAvailable 发送新版本 Toast（F-N9）；点击打开设置 › 关于。
func (n *Notifier) PushUpdateAvailable(endpoint, latestVersion string) error {
	if n == nil {
		return nil
	}
	latest := latestVersion
	if latest == "" {
		latest = "新版本"
	}
	target := webui.SettingsAboutURL(endpoint)
	notification := toast.Notification{
		AppID:               toastAppID,
		Title:               "DAgents 新版本可用",
		Message:             fmt.Sprintf("可升级到 %s，点击查看详情", latest),
		ActivationType:      "protocol",
		ActivationArguments: target,
		Actions: []toast.Action{
			{Type: "protocol", Label: "查看", Arguments: target},
		},
	}
	if n.iconPath != "" {
		notification.Icon = n.iconPath
	}
	return notification.Push()
}
