package update

import sharedupdate "github.com/DGS-ai-team/DAgents/shared/update"

// DisabledProvider 在 Manage 未启用时返回静态空状态。
type DisabledProvider struct {
	CurrentVersion string
}

// Snapshot 实现 desktopapi.UpdateProvider。
func (d DisabledProvider) Snapshot() sharedupdate.Status {
	current := d.CurrentVersion
	if current == "" {
		current = "dev"
	}
	return sharedupdate.Status{
		CurrentVersion:  current,
		LatestVersion:   current,
		ManageReachable: false,
		Platform:        sharedupdate.ReleasePlatform(),
		Channel:         sharedupdate.DefaultChannel,
		ApplyCommand:    "dagents update",
		Message:         "Manage 未启用，无法检查更新",
	}
}
