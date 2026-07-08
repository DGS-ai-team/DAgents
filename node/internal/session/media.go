package session

import (
	"github.com/DGS-ai-team/DAgents/node/internal/media"
)

// mediaRegistry 返回 session 媒体索引（活跃 runtime 或 hydrate 冷缓存）。
func (m *Manager) mediaRegistry(sessionID string) *media.Registry {
	if rt := m.getRuntime(sessionID); rt != nil && rt.media != nil {
		return rt.media
	}
	if m == nil {
		return nil
	}
	m.mediaOnlyMu.Lock()
	defer m.mediaOnlyMu.Unlock()
	if m.mediaOnly == nil {
		m.mediaOnly = make(map[string]*media.Registry)
	}
	if reg, ok := m.mediaOnly[sessionID]; ok {
		return reg
	}
	reg, err := media.NewRegistry(sessionID, m.turn.FSRoot)
	if err != nil {
		return nil
	}
	m.mediaOnly[sessionID] = reg
	return reg
}

// RegisterSessionMedia 注册 session 内图片引用（F-M0）。
func (m *Manager) RegisterSessionMedia(sessionID string, opts media.RegisterOpts) (*media.Artifact, error) {
	reg := m.mediaRegistry(sessionID)
	if reg == nil {
		return nil, media.ErrNotFound
	}
	return reg.RegisterFromRelPath(opts)
}

// OpenSessionMedia 打开 session 内已注册媒体的文件（F-M1）。
func (m *Manager) OpenSessionMedia(sessionID, mediaID string) (*media.Artifact, string, error) {
	reg := m.mediaRegistry(sessionID)
	if reg == nil {
		return nil, "", media.ErrNotFound
	}
	return reg.OpenFile(mediaID)
}

// SessionMediaRegistry 返回 session 的 media registry（可能 nil）。
func (m *Manager) SessionMediaRegistry(sessionID string) *media.Registry {
	return m.mediaRegistry(sessionID)
}
