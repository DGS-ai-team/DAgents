package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type writeFileArgs struct {
	Path     string  `json:"path"`
	Content  string  `json:"content"`
	Encoding *string `json:"encoding"`
}

func writeFileToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "write_file",
			Description: descFSPathConvention + " " + descReadBeforeWrite + " 写入文本文件（覆盖）。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "工作区内相对 fs_root 的路径（必填）",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "写入全文（必填）；覆盖已有内容",
					},
					"encoding": fileEncodingToolProperty(),
				},
				"required":             []string{"path", "content"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execWriteFile(_ context.Context, raw json.RawMessage) (string, error) {
	var args writeFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	path, err := r.resolvePath(args.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	enc := r.resolveFileEncoding(args.Encoding)
	payload, err := encodeFileContent(args.Content, enc)
	if err != nil {
		return fmt.Sprintf("ERROR: write_file 失败: %v", err), nil
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s (encoding=%s)", len(payload), args.Path, enc), nil
}
