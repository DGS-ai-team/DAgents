// Package activity 从会话 messages 推导「改过的文件」与「执行过的命令」。
package activity

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// FileChange 为被写入/编辑过的工作区文件（按 path 去重，保留最后一次操作）。
type FileChange struct {
	Path           string   `json:"path"`
	Ops            []string `json:"ops"`
	LastToolCallID string   `json:"last_tool_call_id,omitempty"`
	LastToolName   string   `json:"last_tool_name,omitempty"`
	Rejected       bool     `json:"rejected,omitempty"`
	Preview        string   `json:"preview,omitempty"`
}

// CommandExec 为 bash_run 执行记录（按时间顺序，新→旧由调用方决定）。
type CommandExec struct {
	Command        string `json:"command"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	Rejected       bool   `json:"rejected,omitempty"`
	Status         string `json:"status"` // ok | error | rejected | unknown
	ContentPreview string `json:"content_preview,omitempty"`
}

// Snapshot 为 workspace activity 聚合视图。
type Snapshot struct {
	Files     []FileChange  `json:"files"`
	Commands  []CommandExec `json:"commands"`
	FileCount int           `json:"file_count"`
	CmdCount  int           `json:"command_count"`
}

// DeriveFromMessages 扫描 assistant tool_calls + tool results，提取写文件与 bash。
func DeriveFromMessages(messages []llm.Message) Snapshot {
	type pendingCall struct {
		name string
		args map[string]any
	}
	pending := map[string]pendingCall{}
	files := map[string]*FileChange{}
	fileOrder := []string{}
	var commands []CommandExec

	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		switch role {
		case "assistant":
			for _, tc := range msg.ToolCalls {
				id := strings.TrimSpace(tc.ID)
				name := strings.TrimSpace(tc.Function.Name)
				if id == "" || name == "" {
					continue
				}
				pending[id] = pendingCall{name: name, args: parseArgs(tc.Function.Arguments)}
			}
		case "tool":
			id := strings.TrimSpace(msg.ToolCallID)
			name := strings.TrimSpace(msg.Name)
			pc, ok := pending[id]
			if ok {
				if name == "" {
					name = pc.name
				}
				delete(pending, id)
			}
			if name == "" {
				continue
			}
			args := pc.args
			if args == nil {
				args = map[string]any{}
			}
			content := strings.TrimSpace(msg.Content)
			rejected := looksRejected(content)
			switch name {
			case "write_file", "search_replace":
				path := stringArg(args, "path", "file_path")
				if path == "" {
					path = pathFromWriteResult(content)
				}
				if path == "" {
					continue
				}
				op := "write"
				if name == "search_replace" {
					op = "replace"
				}
				rec, exists := files[path]
				if !exists {
					rec = &FileChange{Path: path}
					files[path] = rec
					fileOrder = append(fileOrder, path)
				}
				if !containsStr(rec.Ops, op) {
					rec.Ops = append(rec.Ops, op)
				}
				rec.LastToolCallID = id
				rec.LastToolName = name
				rec.Rejected = rejected
				rec.Preview = truncateRunes(content, 120)
			case "bash_run":
				cmd := stringArg(args, "command")
				if cmd == "" {
					continue
				}
				status := "ok"
				if rejected {
					status = "rejected"
				} else if looksError(content) {
					status = "error"
				}
				commands = append(commands, CommandExec{
					Command:        cmd,
					ToolCallID:     id,
					Rejected:       rejected,
					Status:         status,
					ContentPreview: truncateRunes(content, 160),
				})
			}
		}
	}

	outFiles := make([]FileChange, 0, len(fileOrder))
	for _, p := range fileOrder {
		outFiles = append(outFiles, *files[p])
	}
	// 命令按时间倒序（最近在前）
	for i, j := 0, len(commands)-1; i < j; i, j = i+1, j-1 {
		commands[i], commands[j] = commands[j], commands[i]
	}
	return Snapshot{
		Files:     outFiles,
		Commands:  commands,
		FileCount: len(outFiles),
		CmdCount:  len(commands),
	}
}

func parseArgs(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return map[string]any{}
	}
	return m
}

func stringArg(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := args[k]; ok {
			switch t := v.(type) {
			case string:
				if s := strings.TrimSpace(t); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func pathFromWriteResult(content string) string {
	// wrote N bytes to PATH (encoding=…)
	const marker = " to "
	idx := strings.Index(content, marker)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(marker):]
	if i := strings.Index(rest, " (encoding="); i >= 0 {
		rest = rest[:i]
	}
	return strings.TrimSpace(rest)
}

func looksRejected(content string) bool {
	c := strings.ToLower(content)
	return strings.Contains(c, "rejected") || strings.Contains(c, "approval denied") || strings.HasPrefix(strings.TrimSpace(content), "ERROR: tool rejected")
}

func looksError(content string) bool {
	c := strings.TrimSpace(content)
	if strings.HasPrefix(c, "ERROR:") || strings.HasPrefix(c, "error:") {
		return true
	}
	lower := strings.ToLower(c)
	return strings.Contains(lower, "exit code") && (strings.Contains(lower, "exit code 1") || strings.Contains(lower, "non-zero"))
}

func containsStr(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func truncateRunes(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}
