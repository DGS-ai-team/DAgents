package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type writeFileArgs struct {
	Path           string  `json:"path"`
	Content        string  `json:"content"`
	Encoding       *string `json:"encoding"`
	ExpectedDigest string  `json:"expected_digest,omitempty"`
}

func writeFileToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "write_file",
			Description: "修改已有文件前须先 read_file 核对空白、换行与上下文。写入文本文件（覆盖）。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "路径（必填）",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "写入全文（必填）；覆盖已有内容",
					},
					"encoding":        fileEncodingToolProperty(),
					"expected_digest": map[string]any{"type": "string", "description": "仅 handbook/ 路径可用：read_file 返回的文件摘要；摘要变化时拒绝覆盖"},
				},
				"required":             []string{"path", "content"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execWriteFile(ctx context.Context, raw json.RawMessage) (string, error) {
	var args writeFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	path, err := r.resolvePath(args.Path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(args.ExpectedDigest) != "" && !isHandbookPath(args.Path) {
		return "", fmt.Errorf("expected_digest is only supported for handbook paths")
	}
	lease, err := r.acquireWorkspaceWrite(ctx, path)
	if err != nil {
		return "", fmt.Errorf("workspace_busy: %w", err)
	}
	defer lease.Release()
	_, err = r.handbookBeforeWrite(ctx, args.Path, path, args.ExpectedDigest)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	choice, err := r.resolveWriteEncodingChoice(args.Path, path, args.Encoding)
	if err != nil {
		return fmt.Sprintf("ERROR: write_file 失败: %v", err), nil
	}
	payload, err := encodeFileContentWithBOM(args.Content, choice.Encoding, choice.UTF8BOM)
	if err != nil {
		return fmt.Sprintf("ERROR: write_file 失败: %v", err), nil
	}
	if r.handbookFS != nil && isHandbookPath(args.Path) {
		// A byte-identical write is a successful no-op. Do not create a history
		// entry or report a handbook mutation for it.
		if current, readErr := os.ReadFile(path); readErr == nil && string(current) == string(payload) {
			return fmt.Sprintf("no changes needed for %s", args.Path), nil
		} else if readErr != nil && !os.IsNotExist(readErr) {
			return "", readErr
		}
		if _, err := r.handbookFS.Write(ctx, path, args.ExpectedDigest, payload); err != nil {
			return "", err
		}
		r.handbookMutations.Add(1)
		return fmt.Sprintf("wrote %d bytes to %s (encoding=%s)", len(payload), args.Path, choice.Encoding), nil
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", err
	}
	if info, err := os.Stat(path); err == nil {
		src := choice.Source
		if args.Encoding != nil {
			src = encSourceArgument
		}
		r.rememberPathEncoding(args.Path, choice.Encoding, info.ModTime(), src)
	}
	return fmt.Sprintf("wrote %d bytes to %s (encoding=%s)", len(payload), args.Path, choice.Encoding), nil
}
