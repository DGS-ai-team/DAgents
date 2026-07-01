package turn

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func buildReadImageVisionUserMessage(payload *tools.ReadImageVisionPayload) (llm.Message, error) {
	if payload == nil {
		return llm.Message{}, fmt.Errorf("nil vision payload")
	}
	path := strings.TrimSpace(payload.RelPath)
	if path == "" {
		return llm.Message{}, fmt.Errorf("empty image path")
	}
	dataURL := strings.TrimSpace(payload.DataURL)
	if dataURL == "" {
		return llm.Message{}, fmt.Errorf("empty image data url")
	}
	prompt := strings.TrimSpace(payload.Prompt)
	if prompt == "" {
		prompt = fmt.Sprintf("read_image 已加载 %q，请根据图像内容继续任务。", path)
	}
	parts := []llm.ContentPart{
		{Type: "text", Text: prompt},
		{Type: "image_url", ImageURL: &llm.ImageURLPart{
			URL:    dataURL,
			Detail: strings.TrimSpace(payload.Detail),
		}},
	}
	return llm.BuildUserMessage("", parts, llm.UserNameToolVision)
}

func (o *Orchestrator) maybeAppendToolVisionUserMessage(sessionID string, history *[]llm.Message, tc llm.ToolCall) {
	if o == nil || !o.multimodalEnabled || o.tools == nil || history == nil {
		return
	}
	payload := o.tools.TakeReadImageVisionForCall(tc.ID)
	if payload == nil {
		return
	}
	userMsg, err := buildReadImageVisionUserMessage(payload)
	if err != nil {
		o.logger.Warn("tool vision follow-up skipped",
			"session_id", sessionID,
			"tool_call_id", tc.ID,
			"error", err,
		)
		return
	}
	o.appendHistory(sessionID, history, userMsg)
}
