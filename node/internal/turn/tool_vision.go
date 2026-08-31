package turn

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func (o *Orchestrator) maybeAppendToolVisionUserMessage(sessionID string, history *[]llm.Message, tc llm.ToolCall) {
	o.appendToolVisionUserMessages(sessionID, history, []llm.ToolCall{tc})
}

// appendToolVisionUserMessages groups image-bearing tool results from one
// tool batch into a single user message. Chat Completions adapters generally
// accept images on user messages, so this preserves the existing wire shape
// while avoiding one extra synthetic message per image.
func (o *Orchestrator) appendToolVisionUserMessages(sessionID string, history *[]llm.Message, calls []llm.ToolCall) {
	if o == nil || !o.multimodalEnabled || o.tools == nil || history == nil {
		return
	}
	payloads := make([]*tools.ReadImageVisionPayload, 0, len(calls))
	for _, tc := range calls {
		if payload := o.tools.TakeReadImageVisionForCall(tc.ID); payload != nil {
			payloads = append(payloads, payload)
		}
	}
	if len(payloads) == 0 {
		return
	}
	userMsg, err := buildToolVisionUserMessage(payloads)
	if err != nil {
		o.logger.Warn("tool vision follow-up skipped",
			"session_id", sessionID,
			"tool_call_count", len(calls),
			"error", err,
		)
		return
	}
	o.appendHistory(sessionID, history, userMsg)
}

func buildToolVisionUserMessage(payloads []*tools.ReadImageVisionPayload) (llm.Message, error) {
	if len(payloads) == 0 {
		return llm.Message{}, fmt.Errorf("empty vision payload list")
	}
	parts := make([]llm.ContentPart, 0, len(payloads)*2)
	for idx, payload := range payloads {
		if payload == nil {
			return llm.Message{}, fmt.Errorf("nil vision payload at index %d", idx)
		}
		path := strings.TrimSpace(payload.RelPath)
		if path == "" {
			return llm.Message{}, fmt.Errorf("empty image path at index %d", idx)
		}
		dataURL := strings.TrimSpace(payload.DataURL)
		if dataURL == "" {
			return llm.Message{}, fmt.Errorf("empty image data url at index %d", idx)
		}
		prompt := strings.TrimSpace(payload.Prompt)
		if prompt == "" {
			prompt = fmt.Sprintf("read_image 已加载 %q，请根据图像内容继续任务。", path)
		}
		if frameID := strings.TrimSpace(payload.FrameID); frameID != "" && !strings.Contains(prompt, "frame_id=") {
			prompt = fmt.Sprintf("%s（frame_id=%s）", prompt, frameID)
		}
		parts = append(parts,
			llm.ContentPart{Type: "text", Text: prompt},
			llm.ContentPart{Type: "image_url", ImageURL: &llm.ImageURLPart{
				URL:    dataURL,
				Detail: strings.TrimSpace(payload.Detail),
			}},
		)
	}
	return llm.BuildUserMessage("", parts, llm.UserNameToolVision)
}
