package tools

import "strings"

// ReadImageVisionPayload 为 read_image / browser_* 截图成功后待注入 LLM 的视觉 user 消息载荷。
type ReadImageVisionPayload struct {
	RelPath string
	Detail  string
	DataURL string
	FrameID string
	// Prompt 非空时作为视觉 follow-up 文本；空则使用 read_image 默认文案。
	Prompt string
}

func (r *Registry) stashReadImageVision(toolCallID string, payload *ReadImageVisionPayload) {
	if r == nil || payload == nil {
		return
	}
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return
	}
	r.visionMu.Lock()
	if r.readImageVision == nil {
		r.readImageVision = make(map[string]*ReadImageVisionPayload)
	}
	r.readImageVision[id] = payload
	r.visionMu.Unlock()
}

// TakeReadImageVisionForCall 取出并清除 read_image 的视觉 follow-up 载荷。
func (r *Registry) TakeReadImageVisionForCall(toolCallID string) *ReadImageVisionPayload {
	if r == nil {
		return nil
	}
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return nil
	}
	r.visionMu.Lock()
	payload := r.readImageVision[id]
	delete(r.readImageVision, id)
	r.visionMu.Unlock()
	return payload
}
