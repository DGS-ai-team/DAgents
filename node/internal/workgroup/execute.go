package workgroup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	membertools "github.com/DGS-ai-team/DAgents/shared/workgroup"
)

// ReadFileArgs 为 read_file 参数。
type ReadFileArgs struct {
	Path string `json:"path"`
}

// WorkspaceExecutableToolNames 成员工作区可执行全集（嵌入 shared/workgroup 目录；不依赖 Manage）。
func WorkspaceExecutableToolNames() []string {
	return membertools.ExecutableToolNames()
}

// WorkspaceDefaultAllowToolNames 新建成员默认白名单（嵌入目录 default=true；通常仅 fs）。
func WorkspaceDefaultAllowToolNames() []string {
	return membertools.DefaultAllowToolNames()
}

var workspaceExecutableTools = func() map[string]struct{} {
	m := make(map[string]struct{}, len(WorkspaceExecutableToolNames()))
	for _, n := range WorkspaceExecutableToolNames() {
		m[n] = struct{}{}
	}
	return m
}()

// 按 workspace 缓存 Registry，使 bash 后台 job 可跨 command 查询/取消。
var workspaceRegistries sync.Map // string -> *tools.Registry

// NewWorkspaceToolExecutor 在 binding workspace 下执行 allowlist 内的 FS/bash 工具；尊重 ctx 取消。
func NewWorkspaceToolExecutor(bindings BindingStore) CommandExecutor {
	return NewWorkspaceToolExecutorWithBackgroundJobStore(bindings, nil)
}

// NewWorkspaceToolExecutorWithBackgroundJobStore is the Node runtime variant
// that shares background bash metadata with regular Agent tools.
func NewWorkspaceToolExecutorWithBackgroundJobStore(bindings BindingStore, jobStore *tools.BackgroundJobStore) CommandExecutor {
	return func(ctx context.Context, cmd ToolCommand) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
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
			// 工作区 read_file：整文件 {path,content}；与 Agent Registry 行窗口版 schema/返回值不同，勿混用。
			return execReadFile(ctx, binding.WorkspacePath, cmd.ArgumentsJSON)
		default:
			return execViaRegistry(ctx, binding.WorkspacePath, name, cmd.ArgumentsJSON, jobStore)
		}
	}
}

func execReadFile(ctx context.Context, workspaceRoot, argumentsJSON string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
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
	if err := ctx.Err(); err != nil {
		return "", err
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

func registryForWorkspace(workspaceRoot string, jobStore *tools.BackgroundJobStore) (*tools.Registry, error) {
	key := filepath.Clean(workspaceRoot)
	sessionID := workspaceSessionID(key)
	if v, ok := workspaceRegistries.Load(key); ok {
		reg := v.(*tools.Registry)
		if jobStore != nil {
			if err := reg.WithBackgroundJobStoreForSession(jobStore, sessionID); err != nil {
				return nil, err
			}
		}
		return reg, nil
	}
	reg, err := tools.NewRegistry(workspaceRoot, 30)
	if err != nil {
		return nil, err
	}
	if jobStore != nil {
		if err := reg.WithBackgroundJobStoreForSession(jobStore, sessionID); err != nil {
			return nil, err
		}
	}
	reg.SetMultimodalEnabled(true)
	actual, _ := workspaceRegistries.LoadOrStore(key, reg)
	return actual.(*tools.Registry), nil
}

func workspaceSessionID(workspaceRoot string) string {
	return "workgroup:" + filepath.Clean(workspaceRoot)
}

func execViaRegistry(ctx context.Context, workspaceRoot, toolName, argumentsJSON string, jobStore *tools.BackgroundJobStore) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// 预校验 path / directory / cwd，避免 Registry 细节差异
	var probe map[string]any
	if err := json.Unmarshal([]byte(argumentsJSON), &probe); err != nil {
		return "", errf(CodeSchemaMismatch, "invalid arguments_json: %v", err)
	}
	switch toolName {
	case "write_file", "grep_file", "search_replace", "show_image", "read_image":
		if _, err := sanitizeWorkspaceRelPath(strArg(probe, "path")); err != nil {
			return "", err
		}
	case "glob_files", "grep_files":
		dir := strings.TrimSpace(strArg(probe, "directory"))
		if dir == "" {
			dir = "."
		}
		if dir != "." && dir != "./" {
			if _, err := sanitizeWorkspaceRelPath(dir); err != nil {
				return "", err
			}
		}
	case "bash_run":
		cwd := strings.TrimSpace(strArg(probe, "cwd"))
		if cwd != "" && cwd != "." && cwd != "./" {
			if _, err := sanitizeWorkspaceRelPath(cwd); err != nil {
				return "", err
			}
		}
	}
	reg, err := registryForWorkspace(workspaceRoot, jobStore)
	if err != nil {
		return "", errf(CodeConflict, "workspace tools: %v", err)
	}
	out, err := reg.Execute(tools.WithSession(ctx, workspaceSessionID(workspaceRoot)), toolName, argumentsJSON)
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
