// Package membertools 提供成员工具目录（仓库内单一 JSON，Go embed）。
//
// 设计约束：Agent Node 可单机运行、不连接 Manage。因此目录不得依赖运行时 HTTP 拉 Manage；
// Executor / provision 默认白名单 / manifest schema 一律读本包嵌入数据。
// Manage 与 Console 读同一 JSON 文件（Python 侧加载），保证联调与单机一致。
package membertools

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed member_tool_catalog.json
var catalogJSON []byte

type catalogFile struct {
	SchemaVersion int           `json:"schema_version"`
	Groups        []CatalogGroup `json:"groups"`
	Tools         []CatalogTool `json:"tools"`
}

// CatalogGroup 工具分组（UI）。
type CatalogGroup struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// CatalogTool 单条成员可执行工具。
type CatalogTool struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Group       string         `json:"group"`
	GroupLabel  string         `json:"group_label"`
	Hint        string         `json:"hint"`
	Default     bool           `json:"default"`
	SideEffect  string         `json:"side_effect"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

var (
	loadOnce sync.Once
	loaded   catalogFile
	loadErr  error
)

func load() {
	loadOnce.Do(func() {
		if err := json.Unmarshal(catalogJSON, &loaded); err != nil {
			loadErr = fmt.Errorf("member_tool_catalog.json: %w", err)
			return
		}
		if len(loaded.Tools) == 0 {
			loadErr = fmt.Errorf("member_tool_catalog.json: empty tools")
		}
	})
}

func mustLoad() catalogFile {
	load()
	if loadErr != nil {
		panic(loadErr)
	}
	return loaded
}

// ExecutableToolNames 成员工作区 Executor 可执行全集（fs + bash）。
func ExecutableToolNames() []string {
	c := mustLoad()
	out := make([]string, 0, len(c.Tools))
	for _, t := range c.Tools {
		out = append(out, t.ID)
	}
	return out
}

// DefaultAllowToolNames 新建成员默认白名单（通常仅 fs；bash 默认不勾）。
func DefaultAllowToolNames() []string {
	c := mustLoad()
	out := make([]string, 0, len(c.Tools))
	for _, t := range c.Tools {
		if t.Default {
			out = append(out, t.ID)
		}
	}
	return out
}

// SideEffectClasses 工具名 → side_effect。
func SideEffectClasses() map[string]string {
	c := mustLoad()
	out := make(map[string]string, len(c.Tools))
	for _, t := range c.Tools {
		out[t.ID] = t.SideEffect
	}
	return out
}

// ToolSchemas 供 Workgroup manifest 使用的 JSON Schema（parameters 对象）。
func ToolSchemas() map[string]map[string]any {
	c := mustLoad()
	out := make(map[string]map[string]any, len(c.Tools))
	for _, t := range c.Tools {
		if t.Parameters == nil {
			out[t.ID] = map[string]any{"type": "object"}
			continue
		}
		// 浅拷贝顶层，避免调用方改写嵌入解析结果
		cp := make(map[string]any, len(t.Parameters))
		for k, v := range t.Parameters {
			cp[k] = v
		}
		out[t.ID] = cp
	}
	return out
}

// Tools 返回目录条目副本。
func Tools() []CatalogTool {
	c := mustLoad()
	out := make([]CatalogTool, len(c.Tools))
	copy(out, c.Tools)
	return out
}

// Groups 返回分组副本。
func Groups() []CatalogGroup {
	c := mustLoad()
	out := make([]CatalogGroup, len(c.Groups))
	copy(out, c.Groups)
	return out
}

// APICatalog 与 Manage/Node `GET …/meta/member-tools` 响应对齐（供单机 Node 本地提供）。
func APICatalog() map[string]any {
	c := mustLoad()
	tools := make([]map[string]any, 0, len(c.Tools))
	defaults := make([]string, 0)
	for _, t := range c.Tools {
		tools = append(tools, map[string]any{
			"id":          t.ID,
			"label":       t.Label,
			"group":       t.Group,
			"group_label": t.GroupLabel,
			"hint":        t.Hint,
			"default":     t.Default,
			"side_effect": t.SideEffect,
		})
		if t.Default {
			defaults = append(defaults, t.ID)
		}
	}
	groups := make([]map[string]any, 0, len(c.Groups))
	for _, g := range c.Groups {
		groups = append(groups, map[string]any{"id": g.ID, "label": g.Label})
	}
	return map[string]any{
		"tools":               tools,
		"default_allow_names": defaults,
		"groups":              groups,
	}
}
