package session

import (
	"github.com/DGS-ai-team/DAgents/node/internal/media"
)

// OpenSessionMedia 打开 session 内已注册媒体的文件（F-M1）。
func (m *Manager) OpenSessionMedia(sessionID, mediaID string) (*media.Artifact, string, error) {
	rt := m.getRuntime(sessionID)
	if rt == nil || rt.media == nil {
		return nil, "", media.ErrNotFound
	}
	return rt.media.OpenFile(mediaID)
}

// SessionMediaRegistry 返回活跃 session 的 media registry（可能 nil）。
func (m *Manager) SessionMediaRegistry(sessionID string) *media.Registry {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return nil
	}
	return rt.media
}
