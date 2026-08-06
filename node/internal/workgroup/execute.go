package workgroup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// ReadFileArgs 为 read_file 参数。
type ReadFileArgs struct {
	Path string `json:"path"`
}

var workspaceExecutableTools = map[string]struct{}{
	"read_file":  {},
	"glob_files": {},
	"write_file": {},
}

// NewWorkspaceToolExecutor 在 binding workspace 下执行 allowlist 内的 FS 工具。
func NewWorkspaceToolExecutor(bindings BindingStore) func(cmd ToolCommand) (string, error) {
	return func(cmd ToolCommand) (string, error) {
		name := strings.TrimSpace(cmd.ToolName)
		if _, ok := workspaceExecutableTools[name]; !ok {
			return "", errf(CodeConflict, "unsupported tool %q", name)
		}
		binding, err := bindings.Get(cmd.MemberID)
		if err != nil {
			return "", err
		}
		if binding == nil {
			return "", errf(CodeNotFound, "worker binding not found")
		}
		switch name {
		case "read_file":
			return execReadFile(binding.WorkspacePath, cmd.ArgumentsJSON)
		case "glob_files", "write_file":
			return execViaRegistry(binding.WorkspacePath, name, cmd.ArgumentsJSON)
		default:
			return "", errf(CodeConflict, "unsupported tool %q", name)
		}
	}
}

// NewReadFileExecutor 兼容旧名。
func NewReadFileExecutor(bindings BindingStore) func(cmd ToolCommand) (string, error) {
	return NewWorkspaceToolExecutor(bindings)
}

func execReadFile(workspaceRoot, argumentsJSON string) (string, error) {
	var args ReadFileArgs
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return "", errf(CodeSchemaMismatch, "invalid arguments_json: %v", err)
	}
	rel, err := sanitizeWorkspaceRelPath(args.Path)
	if err != nil {
		return "", err
	}
	full := filepath.Join(workspaceRoot, filepath.Clean(rel))
	relCheck, err := filepath.Rel(workspaceRoot, full)
	if err != nil || strings.HasPrefix(relCheck, "..") {
		return "", errf(CodeNotAuthorized, "path escapes member workspace: %s", rel)
	}
	raw, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", errf(CodeNotFound, "file not found in member workspace: %s", rel)
		}
		return "", errf(CodeConflict, "read failed: %v", err)
	}
	out, err := json.Marshal(map[string]any{
		"path":    rel,
		"content": string(raw),
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func execViaRegistry(workspaceRoot, toolName, argumentsJSON string) (string, error) {
	// 预校验 path / directory，避免 Registry 细节差异
	var probe map[string]any
	if err := json.Unmarshal([]byte(argumentsJSON), &probe); err != nil {
		return "", errf(CodeSchemaMismatch, "invalid arguments_json: %v", err)
	}
	switch toolName {
	case "write_file":
		if _, err := sanitizeWorkspaceRelPath(strArg(probe, "path")); err != nil {
			return "", err
		}
	case "glob_files":
		dir := strings.TrimSpace(strArg(probe, "directory"))
		if dir == "" {
			dir = "."
		}
		if dir != "." && dir != "./" {
			if _, err := sanitizeWorkspaceRelPath(dir); err != nil {
				return "", err
			}
		}
	}
	reg, err := tools.NewRegistry(workspaceRoot, 30)
	if err != nil {
		return "", errf(CodeConflict, "workspace tools: %v", err)
	}
	out, err := reg.Execute(context.Background(), toolName, argumentsJSON)
	if err != nil {
		return "", errf(CodeConflict, "%s failed: %v", toolName, err)
	}
	return out, nil
}

func sanitizeWorkspaceRelPath(path string) (string, error) {
	rel := strings.TrimSpace(path)
	if rel == "" {
		return "", errf(CodeSchemaMismatch, "path required")
	}
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", errf(CodeNotAuthorized, "path must be relative to member workspace (no absolute host paths or ..): %s", rel)
	}
	// Windows 盘符形式（在 IsAbs 未命中时）
	norm := strings.ReplaceAll(rel, "\\", "/")
	if len(norm) >= 2 && norm[1] == ':' {
		return "", errf(CodeNotAuthorized, "path must be relative to member workspace (no absolute host paths or ..): %s", rel)
	}
	if strings.HasPrefix(norm, "/") {
		return "", errf(CodeNotAuthorized, "path must be relative to member workspace (no absolute host paths or ..): %s", rel)
	}
	return rel, nil
}

func strArg(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
