package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/wecom"
)

func wecomSendMarkdownToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "wecom_send_markdown",
			Description: "向已配置的企业微信群「消息推送」发送 markdown_v2 消息。" +
				"内容须为企微 markdown_v2 子集（标题/加粗斜体/列表/引用/链接/图片/分割线/代码/表格）；不支持字体颜色与 @成员。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "markdown_v2 正文（UTF-8，最长约 4096 字节）",
					},
				},
				"required":             []string{"content"},
				"additionalProperties": false,
			}),
		},
	}
}

func wecomSendFileToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "wecom_send_file",
			Description: "将工作区内的文件上传并推送到企业微信群「消息推送」。" +
				"内部自动完成 media 上传与 file 消息发送；单文件不超过 20MB。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "工作区相对路径，或工作区内绝对路径",
					},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) wecomToolDefs() []ToolDef {
	if !r.wecomToolsEnabled() {
		return nil
	}
	return []ToolDef{
		wecomSendMarkdownToolDef(),
		wecomSendFileToolDef(),
	}
}

func (r *Registry) wecomToolsEnabled() bool {
	return r != nil && r.wecom != nil && r.wecom.Enabled()
}

// SetWeComClient 注入企业微信 webhook 客户端；nil 时不暴露 wecom_* 工具。
func (r *Registry) SetWeComClient(client *wecom.Client) {
	if r == nil {
		return
	}
	r.wecom = client
	r.registerWeComTools()
}

func (r *Registry) registerWeComTools() {
	if r == nil {
		return
	}
	if !r.wecomToolsEnabled() {
		delete(r.handlers, "wecom_send_markdown")
		delete(r.handlers, "wecom_send_file")
		return
	}
	r.handlers["wecom_send_markdown"] = r.execWeComSendMarkdown
	r.handlers["wecom_send_file"] = r.execWeComSendFile
}

func (r *Registry) execWeComSendMarkdown(ctx context.Context, raw json.RawMessage) (string, error) {
	if !r.wecomToolsEnabled() {
		return formatWeComResult(false, "企业微信消息推送未启用（config wecom.enabled + webhook）", nil), nil
	}
	var args struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if err := r.wecom.SendMarkdownV2(ctx, args.Content); err != nil {
		return formatWeComResult(false, err.Error(), nil), nil
	}
	return formatWeComResult(true, "markdown_v2 已发送", map[string]any{
		"bytes": len([]byte(strings.TrimSpace(args.Content))),
	}), nil
}

func (r *Registry) execWeComSendFile(ctx context.Context, raw json.RawMessage) (string, error) {
	if !r.wecomToolsEnabled() {
		return formatWeComResult(false, "企业微信消息推送未启用（config wecom.enabled + webhook）", nil), nil
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	abs, err := r.resolvePath(args.Path)
	if err != nil {
		return formatWeComResult(false, err.Error(), nil), nil
	}
	mediaID, err := r.wecom.SendFilePath(ctx, abs, filepath.Base(abs))
	if err != nil {
		return formatWeComResult(false, err.Error(), map[string]any{
			"path": args.Path,
		}), nil
	}
	return formatWeComResult(true, "文件已发送", map[string]any{
		"path":     args.Path,
		"filename": filepath.Base(abs),
		"media_id": mediaID,
	}), nil
}

func formatWeComResult(ok bool, message string, extra map[string]any) string {
	out := map[string]any{
		"ok":      ok,
		"message": strings.TrimSpace(message),
	}
	for k, v := range extra {
		out[k] = v
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf(`{"ok":%v,"message":%q}`, ok, message)
	}
	return string(raw)
}
