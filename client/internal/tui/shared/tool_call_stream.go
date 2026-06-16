package shared

import "strings"

// ToolCallStreamState 跟踪流式 partial tool_call 块 ID（对齐 Python _partial_tool_index_ids / _pending_tools）。
type ToolCallStreamState struct {
	partialIndexIDs map[int]string
	activeBlocks    map[string]struct{}
}

// NewToolCallStreamState 创建空的 tool_call 流式状态。
func NewToolCallStreamState() *ToolCallStreamState {
	return &ToolCallStreamState{
		partialIndexIDs: make(map[int]string),
		activeBlocks:    make(map[string]struct{}),
	}
}

// Reset 清空（done / 切换 session）。
func (s *ToolCallStreamState) Reset() {
	if s == nil {
		return
	}
	s.partialIndexIDs = make(map[int]string)
	s.activeBlocks = make(map[string]struct{})
}

// ResolveBlockID 解析 transcript 块 ID；migrateFrom 非空时须移除旧块（对齐 Python _resolve_tool_call_id）。
func (s *ToolCallStreamState) ResolveBlockID(callID string, toolIndex int, partial bool) (blockID, migrateFrom string) {
	if s == nil {
		return strings.TrimSpace(callID), ""
	}
	callID = strings.TrimSpace(callID)
	if callID != "" {
		if toolIndex >= 0 {
			if oldID, ok := s.partialIndexIDs[toolIndex]; ok {
				delete(s.partialIndexIDs, toolIndex)
				if oldID != callID {
					migrateFrom = oldID
				}
			}
		}
		return callID, migrateFrom
	}
	if partial && toolIndex >= 0 {
		if existing, ok := s.partialIndexIDs[toolIndex]; ok {
			return existing, ""
		}
		placeholder := "partial-" + itoa(toolIndex)
		s.partialIndexIDs[toolIndex] = placeholder
		return placeholder, ""
	}
	return "", ""
}

// ClearPartialIndex 在 final tool_call 到达后清除 index 占位（对齐 Python pop on not partial）。
func (s *ToolCallStreamState) ClearPartialIndex(toolIndex int, partial bool) {
	if s == nil || partial || toolIndex < 0 {
		return
	}
	delete(s.partialIndexIDs, toolIndex)
}

// HasActiveBlock 块是否已在 transcript 中作为 pending 展示。
func (s *ToolCallStreamState) HasActiveBlock(blockID string) bool {
	if s == nil {
		return false
	}
	blockID = strings.TrimSpace(blockID)
	if blockID == "" {
		return false
	}
	_, ok := s.activeBlocks[blockID]
	return ok
}

// ForgetBlock 移除 active 标记（tool_result / 迁移后）。
func (s *ToolCallStreamState) ForgetBlock(blockID string) {
	if s == nil {
		return
	}
	blockID = strings.TrimSpace(blockID)
	if blockID == "" {
		return
	}
	delete(s.activeBlocks, blockID)
}

// UpsertToolCallLines 写入或原地替换 tool_call pending 行（partial upsert / final 不重复追加）。
func UpsertToolCallLines(tr *Transcript, state *ToolCallStreamState, blockID, migrateFrom string, lines []string) {
	if tr == nil {
		return
	}
	if migrateFrom != "" && migrateFrom != blockID {
		tr.RemoveToolPendingLines(migrateFrom)
		if state != nil {
			state.ForgetBlock(migrateFrom)
		}
	}
	if blockID == "" {
		for _, line := range lines {
			tr.Add(line)
		}
		return
	}
	if state != nil && state.HasActiveBlock(blockID) {
		tr.ReplaceToolCallLines(blockID, lines)
		return
	}
	tr.RemoveToolPendingLines(blockID)
	for _, line := range lines {
		tr.Add(line)
	}
	if state != nil {
		state.activeBlocks[blockID] = struct{}{}
	}
}

// HandleToolCallEvent 处理 tool_call SSE（对齐 Python _write_tool_call）；返回 transcript 块 ID。
func HandleToolCallEvent(tr *Transcript, state *ToolCallStreamState, data map[string]any, verbose bool, pending *ToolPendingTracker) string {
	if state == nil {
		state = NewToolCallStreamState()
	}
	partial, _ := data["partial"].(bool)
	toolIndex := ToolIndexFromEvent(data)
	callID := ToolCallIDFromEvent(data)

	if partial && toolIndex < 0 && callID == "" {
		return ""
	}

	blockID, migrateFrom := state.ResolveBlockID(callID, toolIndex, partial)
	state.ClearPartialIndex(toolIndex, partial)

	if blockID == "" {
		return ""
	}

	lines := FormatToolEventWithID("tool_call", data, blockID, verbose)
	UpsertToolCallLines(tr, state, blockID, migrateFrom, lines)

	if !partial {
		RegisterToolCallsFromEvent(data, pending)
	}
	return blockID
}

// ToolCallIDFromEvent 从 SSE payload 取首个非空 tool call id。
func ToolCallIDFromEvent(data map[string]any) string {
	for _, call := range extractToolCalls(data) {
		if id := strings.TrimSpace(call.ID); id != "" {
			return id
		}
	}
	return ToolEventID(data)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [16]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
