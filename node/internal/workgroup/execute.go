package workgroup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ReadFileArgs 为 D3 单工具参数。
type ReadFileArgs struct {
	Path string `json:"path"`
}

// NewReadFileExecutor 在 binding workspace 下安全读取相对路径文件。
func NewReadFileExecutor(bindings BindingStore) func(cmd ToolCommand) (string, error) {
	return func(cmd ToolCommand) (string, error) {
		if cmd.ToolName != "read_file" {
			return "", errf(CodeConflict, "unsupported tool %q", cmd.ToolName)
		}
		binding, err := bindings.Get(cmd.MemberID)
		if err != nil {
			return "", err
		}
		if binding == nil {
			return "", errf(CodeNotFound, "worker binding not found")
		}
		var args ReadFileArgs
		if err := json.Unmarshal([]byte(cmd.ArgumentsJSON), &args); err != nil {
			return "", errf(CodeSchemaMismatch, "invalid arguments_json: %v", err)
		}
		rel := strings.TrimSpace(args.Path)
		if rel == "" {
			return "", errf(CodeSchemaMismatch, "path required")
		}
		if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
			return "", errf(CodeNotAuthorized, "path must be relative without ..")
		}
		full := filepath.Join(binding.WorkspacePath, filepath.Clean(rel))
		relCheck, err := filepath.Rel(binding.WorkspacePath, full)
		if err != nil || strings.HasPrefix(relCheck, "..") {
			return "", errf(CodeNotAuthorized, "path escapes workspace")
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			if os.IsNotExist(err) {
				return "", errf(CodeNotFound, "file not found: %s", rel)
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
}
