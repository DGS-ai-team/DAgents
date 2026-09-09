package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DGS-ai-team/DAgents/node/internal/events"
)

func eventSourceListToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{Name: "event_source_list", Description: "列出当前可信 Agent 自己已注册的只读事件源及健康状态；不会读取文件内容或其他 Agent 配置。", Parameters: injectCallPurposeParam(map[string]any{"type": "object", "additionalProperties": false})}}
}
func (r *Registry) SetEventSourceStore(s *events.Store) { r.eventStore = s }
func (r *Registry) execEventSourceList(ctx context.Context, _ json.RawMessage) (string, error) {
	if r.eventStore == nil {
		return "", fmt.Errorf("event source store unavailable")
	}
	if r.agentID == "" {
		return "", fmt.Errorf("trusted agent identity unavailable")
	}
	type view struct {
		SourceID  string `json:"source_id"`
		Root      string `json:"root"`
		Revision  int64  `json:"revision"`
		Enabled   bool   `json:"enabled"`
		LastError string `json:"last_error,omitempty"`
	}
	out := []view{}
	for _, x := range r.eventStore.ListRegistrationsForOwner(r.agentID) {
		st, _ := r.eventStore.Get(x.SourceID)
		out = append(out, view{x.SourceID, x.Root, x.Revision, x.Enabled, st.LastError})
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}
