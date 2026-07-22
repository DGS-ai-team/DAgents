package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type rememberArgs struct {
	Information string `json:"information"`
}

type rememberAnalysis struct {
	HasConflict         bool   `json:"has_conflict"`
	ConflictDescription string `json:"conflict_description"`
	MergedContent       string `json:"merged_content"`
	ExistingExcerpt     string `json:"existing_excerpt"`
	NewExcerpt          string `json:"new_excerpt"`
}

// MemoryConflictMeta 为 remember 冲突时 HITL 展示与 resume 决策所需元数据。
type MemoryConflictMeta struct {
	ExistingContent     string `json:"existing"`
	NewInformation      string `json:"new_information"`
	ConflictDescription string `json:"conflict_description"`
	MergedBoth          string `json:"merged_both"`
}

func (o *Orchestrator) SetLongTermStore(store LongTermStore) {
	if o == nil {
		return
	}
	o.longTermStore = store
}

func (o *Orchestrator) executeRememberTool(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	tc llm.ToolCall,
) (*PendingHITLItem, error) {
	if o.isChildSession {
		msg := "rejected: remember_forbidden_for_child"
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		return nil, nil
	}
	if o.longTermStore == nil {
		msg := "ERROR: long-term memory store unavailable"
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		return nil, nil
	}

	var args rememberArgs
	_, cleaned := tools.ParseRunInBackground(tc.Function.Arguments)
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		msg := "ERROR: invalid remember arguments: " + err.Error()
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		return nil, nil
	}
	info := strings.TrimSpace(args.Information)
	if info == "" {
		msg := "ERROR: information is required"
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		return nil, nil
	}

	existing, err := o.longTermStore.ReadLongTerm(ctx)
	if err != nil {
		msg := "ERROR: read long-term memory: " + err.Error()
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		return nil, nil
	}
	existing = strings.TrimSpace(existing)

	analysis, err := o.analyzeRememberConflict(ctx, existing, info)
	if err != nil {
		msg := "ERROR: analyze memory conflict: " + err.Error()
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		return nil, nil
	}

	if analysis.HasConflict {
		meta := MemoryConflictMeta{
			ExistingContent:     firstNonEmptyString(analysis.ExistingExcerpt, existing),
			NewInformation:      firstNonEmptyString(analysis.NewExcerpt, info),
			ConflictDescription: strings.TrimSpace(analysis.ConflictDescription),
			MergedBoth:          strings.TrimSpace(analysis.MergedContent),
		}
		return &PendingHITLItem{ToolCall: tc, MemoryConflict: &meta}, nil
	}

	merged := strings.TrimSpace(analysis.MergedContent)
	if merged == "" {
		merged = mergeRememberNoConflict(existing, info)
	}
	if err := o.persistLongTerm(ctx, merged); err != nil {
		msg := "ERROR: save long-term memory: " + err.Error()
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		return nil, nil
	}
	output := fmt.Sprintf("已写入长期记忆（%d 字符）。", len([]rune(merged)))
	o.publishToolResult(sessionID, tc, output, false, nil)
	o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, output))
	return nil, nil
}

func (o *Orchestrator) analyzeRememberConflict(ctx context.Context, existing, info string) (rememberAnalysis, error) {
	if o.llm == nil {
		if existing != "" && strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(info)) {
			return rememberAnalysis{HasConflict: false, MergedContent: existing}, nil
		}
		return rememberAnalysis{HasConflict: false, MergedContent: mergeRememberNoConflict(existing, info)}, nil
	}
	userPrompt := fmt.Sprintf(`现有长期记忆：
%s

新信息：
%s

请判断新信息是否与现有记忆冲突（矛盾、重复替换语义等）。若无冲突，给出合并后的完整长期记忆正文。`, existingOrPlaceholder(existing), info)
	text, err := o.llm.CompleteText(ctx, llm.CompleteRequest{
		SystemPrompt: rememberConflictSystemPrompt,
		UserPrompt:   userPrompt,
	})
	if err != nil {
		return rememberAnalysis{}, err
	}
	var out rememberAnalysis
	if err := json.Unmarshal([]byte(extractJSONObject(text)), &out); err != nil {
		return rememberAnalysis{}, fmt.Errorf("parse llm json: %w (raw=%q)", err, truncateRunes(text, 200))
	}
	return out, nil
}

func (o *Orchestrator) persistLongTerm(ctx context.Context, content string) error {
	content = strings.TrimSpace(content)
	if err := o.longTermStore.SaveLongTerm(ctx, content); err != nil {
		return err
	}
	if o.promptCtx != nil {
		o.promptCtx.UpdateLongTerm(content)
	}
	return nil
}

func mergeRememberNoConflict(existing, info string) string {
	existing = strings.TrimSpace(existing)
	info = strings.TrimSpace(info)
	if existing == "" {
		return info
	}
	if info == "" {
		return existing
	}
	return existing + "\n\n" + info
}

func existingOrPlaceholder(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "（空）"
	}
	return s
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

const rememberConflictSystemPrompt = `你是长期记忆冲突检测助手。根据「现有长期记忆」与「新信息」判断是否冲突。
仅输出 JSON，不要 markdown 代码块，格式：
{
  "has_conflict": boolean,
  "conflict_description": "冲突说明（has_conflict 为 true 时必填）",
  "merged_content": "合并后的完整长期记忆正文（无冲突时必填；有冲突时填写若用户选择全部保留的合并结果）",
  "existing_excerpt": "与冲突相关的现有记忆摘录（有冲突时）",
  "new_excerpt": "新信息摘录（有冲突时）"
}
无冲突时 has_conflict=false，merged_content 为合并后的完整正文。有冲突时 has_conflict=true，并填写 conflict_description 与摘录。`
