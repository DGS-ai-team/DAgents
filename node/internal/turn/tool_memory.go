package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func (o *Orchestrator) executeMemoryTool(ctx context.Context, sessionID string, history *[]llm.Message, tc llm.ToolCall) error {
	if o.memoryService == nil {
		return o.commitMemoryToolError(sessionID, history, tc, "memory service unavailable")
	}
	var payload map[string]any
	_, cleaned := tools.ParseToolCallArguments(tc.Function.Arguments)
	if err := json.Unmarshal([]byte(cleaned), &payload); err != nil {
		return o.commitMemoryToolError(sessionID, history, tc, "invalid memory arguments: "+err.Error())
	}
	scope := memory.Scope(strings.TrimSpace(fmt.Sprint(payload["scope"])))
	if scope != memory.ScopeAgent && scope != memory.ScopeGlobal {
		scope = ""
	}

	var output string
	var err error
	var forgetResult memory.WriteResult
	var forgetErr error
	switch {
	case tools.IsMemorySearch(tc.Function.Name):
		query, ok := memoryToolQuery(payload)
		if !ok {
			return o.commitMemoryToolError(sessionID, history, tc, "query is required")
		}
		limit := memoryToolInt(payload, "limit", memory.DefaultMemorySearchLimit, 1, memory.DefaultMemorySearchLimit)
		results, searchErr := o.memoryService.Search(ctx, memory.SearchRequest{Scope: scope, Query: query, Limit: limit})
		err = searchErr
		if err == nil {
			output, err = buildMemorySearchOutput(query, effectiveMemoryScope(scope, o.memoryService), results)
		}
	case tools.IsMemoryGet(tc.Function.Name):
		id := strings.TrimSpace(fmt.Sprint(payload["id"]))
		if id == "" || id == "<nil>" {
			return o.commitMemoryToolError(sessionID, history, tc, "id is required")
		}
		entry, getErr := o.memoryService.Get(ctx, scope, id, false)
		err = getErr
		if err == nil {
			offset := memoryToolInt(payload, "offset", 0, 0, 0)
			maxTokens := memoryToolInt(payload, "max_tokens", memory.DefaultMemoryGetContentBudget, 100, memory.DefaultMemoryGetContentBudget)
			output, err = buildMemoryGetOutput(entry, offset, maxTokens)
		}
	case tools.IsMemoryForget(tc.Function.Name):
		id := strings.TrimSpace(fmt.Sprint(payload["id"]))
		if id == "" || id == "<nil>" {
			return o.commitMemoryToolError(sessionID, history, tc, "id is required")
		}
		forgetResult, forgetErr = o.memoryService.Forget(ctx, scope, id, strings.TrimSpace(fmt.Sprint(payload["reason"])))
		err = forgetErr
		if err == nil {
			output = memoryForgetOutput(forgetResult)
		}
	}
	if err != nil {
		return o.commitMemoryToolError(sessionID, history, tc, err.Error())
	}
	if tools.IsMemoryForget(tc.Function.Name) && o.hub != nil {
		// Memory mutations become visible on the next Turn. They are an event
		// for projections/UI, never an InputBox or MessageQueue item.
		o.hub.Publish(o.agentID, "memory/changed", map[string]any{
			"agent_id": o.agentID, "store_revision": forgetResult.StoreRevision,
			"outcome": string(forgetResult.Outcome), "turn_boundary": "next_turn",
		})
	}
	o.publishToolResult(sessionID, tc, output, false, nil)
	o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, output))
	return nil
}

func memoryForgetOutput(result memory.WriteResult) string {
	const maxSupersededIDs = 8
	superseded := make([]string, 0, minInt(len(result.Superseded), maxSupersededIDs))
	for _, id := range result.Superseded[:minInt(len(result.Superseded), maxSupersededIDs)] {
		superseded = append(superseded, memory.BoundText(id, 40).Text)
	}
	payload := map[string]any{
		"status":         "succeeded",
		"outcome":        result.Outcome,
		"existing_id":    memory.BoundText(result.ExistingID, 80).Text,
		"superseded":     superseded,
		"store_revision": result.StoreRevision,
	}
	if len(result.Superseded) > maxSupersededIDs {
		payload["superseded_truncated"] = true
	}
	if result.Entry != nil {
		payload["entry"] = map[string]any{
			"id":       memory.BoundText(result.Entry.ID, 80).Text,
			"scope":    memory.BoundText(string(result.Entry.Scope), 20).Text,
			"tier":     memory.BoundText(string(result.Entry.Tier), 20).Text,
			"kind":     memory.BoundText(string(result.Entry.Kind), 32).Text,
			"status":   memory.BoundText(string(result.Entry.Status), 32).Text,
			"revision": result.Entry.Revision,
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return `{"status":"succeeded","outcome":"unknown"}`
	}
	return string(raw)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (o *Orchestrator) commitMemoryToolError(sessionID string, history *[]llm.Message, tc llm.ToolCall, detail string) error {
	output := "ERROR: " + strings.TrimSpace(detail)
	o.publishToolResult(sessionID, tc, output, true, nil)
	o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, output))
	return nil
}

func intField(payload map[string]any, key string, fallback int) int {
	value, ok := payload[key]
	if !ok {
		return fallback
	}
	switch number := value.(type) {
	case float64:
		if int(number) > 0 {
			return int(number)
		}
	case int:
		if number > 0 {
			return number
		}
	}
	return fallback
}

func effectiveMemoryScope(scope memory.Scope, service memory.Service) string {
	if scope == memory.ScopeGlobal {
		return string(scope)
	}
	if scope == memory.ScopeAgent {
		return string(scope)
	}
	return "configured"
}
