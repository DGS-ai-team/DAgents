package tools

import (
	"context"
	"strings"
)

// MediaArtifactRef 为工具结果 SSE / hydrate 使用的媒体引用（F-M0）。
type MediaArtifactRef struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	MIME    string `json:"mime"`
	URL     string `json:"url"`
	Label   string `json:"label,omitempty"`
	Caption string `json:"caption,omitempty"`
}

// MediaRegisterFunc 将会话内图片注册为可 GET 的 media artifact。
type MediaRegisterFunc func(ctx context.Context, toolCallID, relPath, source, label, caption string) (*MediaArtifactRef, error)

func (r *Registry) SetMediaRegister(fn MediaRegisterFunc) {
	if r == nil {
		return
	}
	r.mediaMu.Lock()
	r.mediaRegister = fn
	r.mediaMu.Unlock()
}

func (r *Registry) registerToolMedia(ctx context.Context, toolCallID, relPath, source, label, caption string) {
	if r == nil {
		return
	}
	r.mediaMu.Lock()
	fn := r.mediaRegister
	r.mediaMu.Unlock()
	if fn == nil {
		return
	}
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return
	}
	art, err := fn(ctx, id, relPath, source, label, caption)
	if err != nil || art == nil {
		return
	}
	r.stashToolResultMedia(id, art)
}

func (r *Registry) stashToolResultMedia(toolCallID string, art *MediaArtifactRef) {
	if r == nil || art == nil {
		return
	}
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return
	}
	item := map[string]any{
		"id":   art.ID,
		"kind": art.Kind,
		"mime": art.MIME,
		"url":  art.URL,
	}
	if art.Label != "" {
		item["label"] = art.Label
	}
	if art.Caption != "" {
		item["caption"] = art.Caption
	}
	r.mediaMu.Lock()
	if r.toolResultMedia == nil {
		r.toolResultMedia = make(map[string][]map[string]any)
	}
	r.toolResultMedia[id] = append(r.toolResultMedia[id], item)
	r.mediaMu.Unlock()
}

// TakeToolResultMediaForCall 取出并清除 tool result 附带的 media[] SSE 字段。
func (r *Registry) TakeToolResultMediaForCall(toolCallID string) map[string]any {
	if r == nil {
		return nil
	}
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return nil
	}
	r.mediaMu.Lock()
	items := r.toolResultMedia[id]
	delete(r.toolResultMedia, id)
	r.mediaMu.Unlock()
	if len(items) == 0 {
		return nil
	}
	return map[string]any{"media": items}
}
