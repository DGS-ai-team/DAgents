package session

import (
	"github.com/DGS-ai-team/DAgents/node/internal/media"
)

// RegisterSessionMedia 注册 session 内图片引用（F-M0）。
func (m *Manager) RegisterSessionMedia(sessionID string, opts media.RegisterOpts) (*media.Artifact, error) {
	rt := m.getRuntime(sessionID)
	if rt == nil || rt.media == nil {
		return nil, media.ErrNotFound
	}
	return rt.media.RegisterFromRelPath(opts)
}

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
