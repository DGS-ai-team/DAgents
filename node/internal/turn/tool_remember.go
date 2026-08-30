package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

const rememberMaxCASRetries = 3

type rememberArgs struct {
	Information string `json:"information"`
}

type rememberAnalysis struct {
	HasConflict         bool   `json:"has_conflict"`
	ConflictDescription string `json:"conflict_description"`
	Action              string `json:"action"`
	ActionContent       string `json:"action_content"`
	ReplaceTarget       string `json:"replace_target"`
	ExistingExcerpt     string `json:"existing_excerpt"`
	NewExcerpt          string `json:"new_excerpt"`
	MergedBoth          string `json:"merged_both"`
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

// SetLongTermScope updates the persistence scope for future memory reads and
// writes without changing the active Turn snapshot.
func (o *Orchestrator) SetLongTermScope(scope string) {
	if o == nil || o.longTermStore == nil {
		return
	}
	if setter, ok := o.longTermStore.(LongTermScopeSetter); ok {
		setter.SetLongTermScope(scope)
	}
}

// SetPromptContent updates the sidecar source used when the next model
// context is built. An active Turn keeps its existing ModelContextSnapshot.
func (o *Orchestrator) SetPromptContent(content promptcontext.Content) {
	if o == nil || o.promptCtx == nil {
		return
	}
	o.promptCtx.SetContent(content)
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
	_, cleaned := tools.ParseToolCallArguments(tc.Function.Arguments)
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

	for attempt := 0; attempt < rememberMaxCASRetries; attempt++ {
		snap, err := o.longTermStore.ReadLongTerm(ctx)
		if err != nil {
			msg := "ERROR: read long-term memory: " + err.Error()
			o.publishToolResult(sessionID, tc, msg, true, nil)
			o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
			return nil, nil
		}
		existingText := FormatLongTermEntries(snap.Entries)

		analysis, err := o.analyzeRememberConflict(ctx, existingText, info)
		if err != nil {
			msg := "ERROR: analyze memory conflict: " + err.Error()
			o.publishToolResult(sessionID, tc, msg, true, nil)
			o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
			return nil, nil
		}

		if analysis.HasConflict {
			meta := MemoryConflictMeta{
				ExistingContent:     firstNonEmptyString(analysis.ExistingExcerpt, existingText),
				NewInformation:      firstNonEmptyString(analysis.NewExcerpt, info),
				ConflictDescription: strings.TrimSpace(analysis.ConflictDescription),
				MergedBoth:          strings.TrimSpace(analysis.MergedBoth),
			}
			return &PendingHITLItem{ToolCall: tc, MemoryConflict: &meta}, nil
		}

		entries := ApplyRememberActionToEntries(snap.Entries, analysis.Action, analysis.ActionContent, analysis.ReplaceTarget)
		if len(entries) == len(snap.Entries) && strings.TrimSpace(analysis.ActionContent) == "" {
			entries = append(append([]LongTermEntry(nil), snap.Entries...), NewLongTermEntry(info, time.Now().UTC()))
		}
		if err := o.persistLongTermCAS(ctx, entries, snap.Version); err != nil {
			if errors.Is(err, ErrLongTermVersionConflict) {
				continue
			}
			msg := "ERROR: save long-term memory: " + err.Error()
			o.publishToolResult(sessionID, tc, msg, true, nil)
			o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
			return nil, nil
		}
		output := fmt.Sprintf("已写入长期记忆（%d 条）。", countNonEmptyEntries(entries))
		o.publishToolResult(sessionID, tc, output, false, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, output))
		return nil, nil
	}

	msg := "ERROR: long-term memory write conflict after retries; please retry remember"
	o.publishToolResult(sessionID, tc, msg, true, nil)
	o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
	return nil, nil
}

func (o *Orchestrator) analyzeRememberConflict(ctx context.Context, existing, info string) (rememberAnalysis, error) {
	if o.llm == nil {
		if existing != "" && strings.EqualFold(strings.TrimSpace(existing), strings.TrimSpace(info)) {
			return rememberAnalysis{HasConflict: false, Action: "add", ActionContent: ""}, nil
		}
		return rememberAnalysis{
			HasConflict:   false,
			Action:        "add",
			ActionContent: strings.TrimSpace(info),
		}, nil
	}
	userPrompt := fmt.Sprintf(`现有长期记忆（每条以 [条目ID] 标识）：
%s

新信息：
%s

请先对双方内容做事实归一化（统一日期/时间格式、数值与单位、称谓与同义词、缩写等），再基于归一化后的事实判断是否冲突。
无冲突时不要输出完整合并正文，只给出 add 或 replace 及对应片段；replace 时 replace_target 填条目 ID（如 lt-xxxx）。`, existingOrPlaceholder(existing), info)
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

func (o *Orchestrator) persistLongTermCAS(ctx context.Context, entries []LongTermEntry, expectedVersion time.Time) error {
	if err := o.longTermStore.SaveLongTerm(ctx, entries, expectedVersion); err != nil {
		return err
	}
	if o.hub != nil {
		o.hub.Publish(o.agentID, "memory/changed", map[string]any{
			"agent_id":      o.agentID,
			"entry_count":   countNonEmptyEntries(entries),
			"turn_boundary": "next_turn",
		})
	}
	return nil
}

func countNonEmptyEntries(entries []LongTermEntry) int {
	n := 0
	for _, e := range entries {
		if strings.TrimSpace(e.Content) != "" {
			n++
		}
	}
	return n
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

const rememberConflictSystemPrompt = `你是长期记忆冲突检测助手。记忆以条目形式存储，每条有唯一 ID（如 lt-xxxx）。
处理步骤：
1. 事实归一化：统一日期时间、数值单位、称谓/同义词、缩写后再比较。
2. 冲突判断：矛盾、互斥替换语义等视为冲突；互补、补充、细化不算冲突。

仅输出 JSON，不要 markdown 代码块，格式：
{
  "has_conflict": boolean,
  "conflict_description": "冲突说明（has_conflict 为 true 时必填）",
  "action": "add 或 replace（无冲突时必填）",
  "action_content": "要追加或替换写入的片段（无冲突时必填，不要输出完整合并正文）",
  "replace_target": "replace 时被替换的条目 ID（action 为 replace 时必填；整篇替换则填空字符串）",
  "existing_excerpt": "与冲突相关的现有记忆摘录（有冲突时）",
  "new_excerpt": "新信息摘录（有冲突时）",
  "merged_both": "用户选择全部保留时的合并结果（有冲突时必填，可按 - [id] 内容 格式）"
}
无冲突时 has_conflict=false，用 action/action_content/replace_target 描述增量写入。有冲突时 has_conflict=true，并填写 conflict_description、摘录与 merged_both。`
