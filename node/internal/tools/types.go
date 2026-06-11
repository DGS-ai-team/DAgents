package tools

import (
	"context"
	"encoding/json"
)

// FunctionDef 为 OpenAI function tool 定义。
type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolDef 为 OpenAI tools 数组项。
type ToolDef struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type handler func(ctx context.Context, args json.RawMessage) (string, error)
