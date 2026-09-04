package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type rememberArgs struct {
	Information string            `json:"information"`
	Kind        memory.Kind       `json:"kind"`
	Tier        memory.Tier       `json:"tier"`
	SemanticKey string            `json:"semantic_key"`
	Subject     string            `json:"subject"`
	Predicate   string            `json:"predicate"`
	Cardinality string            `json:"cardinality"`
	Importance  int               `json:"importance"`
	Confidence  int               `json:"confidence"`
	Value       any               `json:"value"`
	Qualifiers  map[string]string `json:"qualifiers"`
	Sensitivity string            `json:"sensitivity"`
	ValidFrom   string            `json:"valid_from"`
	ValidTo     string            `json:"valid_to"`
	ExpiresAt   string            `json:"expires_at"`
}

// MemoryConflictMeta 为 remember 冲突时 HITL 展示与 resume 决策所需元数据。
type MemoryConflictMeta struct {
	ConflictID          string `json:"conflict_id,omitempty"`
	Scope               string `json:"scope,omitempty"`
	ExistingContent     string `json:"existing"`
	NewInformation      string `json:"new_information"`
	ConflictDescription string `json:"conflict_description"`
	MergedBoth          string `json:"merged_both"`
}

// SetMemoryScope updates the persistence scope for future memory operations
// without changing the active Turn snapshot.
func (o *Orchestrator) SetMemoryScope(scope string) {
	if o == nil || o.memoryService == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(scope), string(memory.ScopeGlobal)) {
		o.memoryService.SetScope(memory.ScopeGlobal)
		return
	}
	o.memoryService.SetScope(memory.ScopeAgent)
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
	if o.memoryService == nil {
		msg := "ERROR: memory service unavailable"
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		return nil, nil
	}

	var args rememberArgs
	cleaned := tools.ParseToolCallArguments(tc.Function.Arguments)
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
	return o.executeRememberWithMemoryService(ctx, sessionID, history, tc, args, info)
}

func (o *Orchestrator) executeRememberWithMemoryService(ctx context.Context, sessionID string, history *[]llm.Message, tc llm.ToolCall, args rememberArgs, info string) (*PendingHITLItem, error) {
	request := memory.RememberRequest{
		Information:   info,
		Kind:          args.Kind,
		Tier:          args.Tier,
		SemanticKey:   args.SemanticKey,
		Subject:       args.Subject,
		Predicate:     args.Predicate,
		Value:         args.Value,
		Qualifiers:    args.Qualifiers,
		Cardinality:   args.Cardinality,
		Importance:    args.Importance,
		Confidence:    args.Confidence,
		Sensitivity:   args.Sensitivity,
		SourceType:    "model_remember",
		SourceSession: sessionID,
		SourceMessage: tc.ID,
	}
	request.ValidFrom = parseOptionalTime(args.ValidFrom)
	request.ValidTo = parseOptionalTime(args.ValidTo)
	request.ExpiresAt = parseOptionalTime(args.ExpiresAt)
	result, err := o.memoryService.Remember(ctx, request)
	if err != nil {
		msg := "ERROR: remember memory: " + err.Error()
		o.publishToolResult(sessionID, tc, msg, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		return nil, nil
	}
	if result.Outcome == memory.WritePendingConflict && result.Conflict != nil {
		var existing strings.Builder
		for _, entry := range result.Conflict.Existing {
			if text := strings.TrimSpace(entry.Content); text != "" {
				if existing.Len() > 0 {
					existing.WriteString("\n")
				}
				existing.WriteString("- ")
				existing.WriteString(text)
			}
		}
		return &PendingHITLItem{ToolCall: tc, MemoryConflict: &MemoryConflictMeta{
			ConflictID:          result.Conflict.ID,
			Scope:               string(result.Conflict.Candidate.Scope),
			ExistingContent:     existing.String(),
			NewInformation:      info,
			ConflictDescription: result.Conflict.Description,
		}}, nil
	}
	output := memoryWriteOutcomeMessage(result)
	o.publishToolResult(sessionID, tc, output, false, nil)
	o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, output))
	if o.hub != nil {
		o.hub.Publish(o.agentID, "memory/changed", map[string]any{
			"agent_id": o.agentID, "store_revision": result.StoreRevision,
			"outcome": string(result.Outcome), "turn_boundary": "next_turn",
		})
	}
	return nil, nil
}

func parseOptionalTime(raw string) *time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

func memoryWriteOutcomeMessage(result memory.WriteResult) string {
	switch result.Outcome {
	case memory.WriteDuplicate:
		return "长期记忆已存在（未重复写入）。"
	case memory.WriteSuperseded:
		return fmt.Sprintf("已写入长期记忆，并替代 %d 条旧记忆。", len(result.Superseded))
	default:
		return "已写入长期记忆。"
	}
}
