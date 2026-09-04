package turn

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

// TurnContextMetrics WS5：单次用户任务（human_message → turn_complete）内的工具链上下文指标。
type TurnContextMetrics struct {
	mu                   sync.RWMutex
	ToolLoops            int
	ToolCalls            int
	HistoryResultTokens  float64
	HistoryResultChars   int
	SpillCount           int
	ReadFileCalls        int
	ReadFilePathRepeats  int
	EncodingSourceDetect int
	EncodingSourceCache  int
	EncodingGarbledHints int
	ToolCallsByName      map[string]int
	readPaths            map[string]int
}

func newTurnContextMetrics() *TurnContextMetrics {
	return &TurnContextMetrics{ToolCallsByName: make(map[string]int)}
}

type contextMetricsStore struct {
	mu   sync.Mutex
	data map[string]*TurnContextMetrics
}

func newContextMetricsStore() *contextMetricsStore {
	return &contextMetricsStore{data: make(map[string]*TurnContextMetrics)}
}

func (s *contextMetricsStore) reset(sessionID string) {
	if s == nil || sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[sessionID] = newTurnContextMetrics()
}

func (s *contextMetricsStore) get(sessionID string) *TurnContextMetrics {
	if s == nil || sessionID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.data[sessionID]
	if !ok {
		m = newTurnContextMetrics()
		s.data[sessionID] = m
	}
	return m
}

func (s *contextMetricsStore) delete(sessionID string) {
	if s == nil || sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, sessionID)
}

// resetContextMetrics 新 user 消息 turn 开始时清零工具链指标，供
// turn_finished 的 tool_context_metrics 使用。
func (o *Orchestrator) resetContextMetrics(sessionID string) {
	if o == nil || o.ctxMetrics == nil {
		return
	}
	o.ctxMetrics.reset(sessionID)
}

func (o *Orchestrator) recordToolLoop(sessionID string, loop int) {
	m := o.contextMetrics(sessionID)
	if m == nil || loop <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if loop > m.ToolLoops {
		m.ToolLoops = loop
	}
}

func (o *Orchestrator) recordToolCall(sessionID, toolName string) {
	m := o.contextMetrics(sessionID)
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return
	}
	m.ToolCalls++
	m.ToolCallsByName[name]++
}

func (o *Orchestrator) recordToolResult(
	sessionID, toolName, rawArgs, forHistory, spillPath string,
	rejected bool,
) {
	if rejected {
		return
	}
	m := o.contextMetrics(sessionID)
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.HistoryResultChars += len(forHistory)
	m.HistoryResultTokens += tokens.Estimate(forHistory)
	if strings.TrimSpace(spillPath) != "" {
		m.SpillCount++
	}
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "read_file" {
		m.ReadFileCalls++
		if path := toolArgPath(rawArgs); path != "" {
			if m.readPaths == nil {
				m.readPaths = make(map[string]int)
			}
			m.readPaths[path]++
			if m.readPaths[path] == 2 {
				m.ReadFilePathRepeats++
			} else if m.readPaths[path] > 2 {
				m.ReadFilePathRepeats++
			}
		}
		recordEncodingMetricsFromOutput(m, forHistory)
	}
	if name == "grep_file" || name == "grep_files" {
		recordEncodingMetricsFromOutput(m, forHistory)
	}
}

func recordEncodingMetricsFromOutput(m *TurnContextMetrics, content string) {
	if m == nil || content == "" {
		return
	}
	if strings.Contains(content, "编码来源: 检测") {
		m.EncodingSourceDetect++
	}
	if strings.Contains(content, "编码来源: 缓存") {
		m.EncodingSourceCache++
	}
	if strings.Contains(content, "编码提示:") {
		m.EncodingGarbledHints++
	}
}

func toolArgPath(rawArgs string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(rawArgs)), &args); err != nil {
		return ""
	}
	path, _ := args["path"].(string)
	return strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
}

func (o *Orchestrator) contextMetrics(sessionID string) *TurnContextMetrics {
	if o == nil || o.ctxMetrics == nil {
		return nil
	}
	return o.ctxMetrics.get(sessionID)
}

func (m *TurnContextMetrics) snapshot() map[string]any {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]any{
		"tool_loops":             m.ToolLoops,
		"tool_calls":             m.ToolCalls,
		"history_result_tokens":  int(m.HistoryResultTokens + 0.5),
		"history_result_chars":   m.HistoryResultChars,
		"spill_count":            m.SpillCount,
		"read_file_calls":        m.ReadFileCalls,
		"read_file_path_repeats": m.ReadFilePathRepeats,
		"encoding_source_detect": m.EncodingSourceDetect,
		"encoding_source_cache":  m.EncodingSourceCache,
		"encoding_garbled_hints": m.EncodingGarbledHints,
	}
	if len(m.ToolCallsByName) > 0 {
		byName := make(map[string]any, len(m.ToolCallsByName))
		for k, v := range m.ToolCallsByName {
			byName[k] = v
		}
		out["tool_calls_by_name"] = byName
	}
	return out
}

func (o *Orchestrator) logTurnContextMetrics(sessionID, finishReason string) {
	m := o.contextMetrics(sessionID)
	if m == nil || o.logger == nil {
		return
	}
	attrs := m.logAttrs(sessionID, finishReason)
	if snapshot := o.ModelContextSnapshot(sessionID); snapshot != nil {
		attrs = append(attrs,
			"runtime_revision", snapshot.RuntimeRevision,
			"runtime_digest", snapshot.RuntimeDigest,
			"prompt_digest", snapshot.PromptDigest,
			"tool_digest", snapshot.ToolDigest,
			"context_injection_digest", snapshot.ContextInjectionDigest,
			"context_injection_count", len(snapshot.ContextInjections),
		)
	}
	o.logger.Info("turn context metrics", attrs...)
}

func (m *TurnContextMetrics) logAttrs(sessionID, finishReason string) []any {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return []any{
		"session_id", sessionID,
		"finish_reason", finishReason,
		"tool_loops", m.ToolLoops,
		"tool_calls", m.ToolCalls,
		"history_result_tokens", int(m.HistoryResultTokens + 0.5),
		"history_result_chars", m.HistoryResultChars,
		"spill_count", m.SpillCount,
		"read_file_calls", m.ReadFileCalls,
		"read_file_path_repeats", m.ReadFilePathRepeats,
		"encoding_source_detect", m.EncodingSourceDetect,
		"encoding_source_cache", m.EncodingSourceCache,
		"encoding_garbled_hints", m.EncodingGarbledHints,
		slog.Group("tool_calls_by_name", toolCallsByNameLogAttrs(m.ToolCallsByName)...),
	}
}

func toolCallsByNameLogAttrs(m map[string]int) []any {
	if len(m) == 0 {
		return nil
	}
	out := make([]any, 0, len(m)*2)
	for name, count := range m {
		out = append(out, name, count)
	}
	return out
}
