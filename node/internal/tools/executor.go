package tools

import "context"

// Executor 为 turn 编排调用的工具后端；*Registry 与 childagent.RestrictedRegistry 均实现。
type Executor interface {
	Definitions() []ToolDef
	Execute(ctx context.Context, name, arguments string) (string, error)
	StartBackground(ctx context.Context, sessionID, toolName, toolCallID, cleanedArgs string) (string, error)
	TakeBashCompressStatsForCall(toolCallID string) map[string]any
	TakeToolResultMediaForCall(toolCallID string) map[string]any
	TakeReadImageVisionForCall(toolCallID string) *ReadImageVisionPayload
}

// Ensure Registry implements Executor.
var _ Executor = (*Registry)(nil)
