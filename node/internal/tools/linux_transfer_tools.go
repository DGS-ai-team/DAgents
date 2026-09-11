package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type terminalFileTransferArgs struct {
	TerminalID string `json:"terminal_id"`
	LocalPath  string `json:"local_path"`
	RemotePath string `json:"remote_path"`
	Overwrite  bool   `json:"overwrite"`
}

func terminalFileTransferToolDefs() []ToolDef {
	base := map[string]any{
		"terminal_id": map[string]any{"type": "string", "description": "terminal_open 返回的终端 ID；必须是当前 Agent 已打开且仍在运行的 Linux 终端"},
		"local_path":  map[string]any{"type": "string", "description": "Node 工作区内的相对文件路径"},
		"remote_path": map[string]any{"type": "string", "description": "已打开 Linux 终端目标上的远程文件路径"},
		"overwrite":   map[string]any{"type": "boolean", "description": "目标文件存在时是否覆盖，默认 false"},
	}
	return []ToolDef{
		{Type: "function", Function: FunctionDef{
			Name:        "terminal_upload",
			Description: "将 Node 工作区中的单个文件上传到 terminal_id 对应的已打开 Linux 终端目标。必须先 terminal_open；传输使用同一会话绑定的 Linux 通道，任务可能排队。",
			Parameters:  injectCallPurposeParam(objectParams(base, "terminal_id", "local_path", "remote_path")),
		}},
		{Type: "function", Function: FunctionDef{
			Name:        "terminal_download",
			Description: "从 terminal_id 对应的已打开 Linux 终端目标下载单个文件到 Node 工作区。必须先 terminal_open；任务可能排队。",
			Parameters:  injectCallPurposeParam(objectParams(base, "terminal_id", "local_path", "remote_path")),
		}},
	}
}

func (r *Registry) execTerminalUpload(ctx context.Context, raw json.RawMessage) (string, error) {
	return r.execTerminalFileTransfer(ctx, raw, "upload")
}

func (r *Registry) execTerminalDownload(ctx context.Context, raw json.RawMessage) (string, error) {
	return r.execTerminalFileTransfer(ctx, raw, "download")
}

func (r *Registry) execTerminalFileTransfer(ctx context.Context, raw json.RawMessage, direction string) (string, error) {
	if r == nil || r.linuxTransferManager == nil {
		return "", fmt.Errorf("terminal file transfer is not configured")
	}
	broker, err := r.terminalBrokerOrError()
	if err != nil {
		return "", err
	}
	var args terminalFileTransferArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	id := strings.TrimSpace(args.TerminalID)
	if id == "" {
		return "", fmt.Errorf("terminal_id is required")
	}
	info, err := broker.Lookup(r.agentID, id)
	if err != nil {
		return "", err
	}
	if info.Status != "running" {
		return "", fmt.Errorf("terminal session %q is %s; open a new terminal first", id, info.Status)
	}
	if info.TargetKind != executionTargetLinuxChannel || strings.TrimSpace(info.TargetID) == "" {
		return "", fmt.Errorf("terminal_id %q is not an SSH Linux terminal; file transfer requires a remote Linux terminal", id)
	}
	return r.linuxTransferManager.Submit(ctx, LinuxTransferRequest{
		AgentID: r.agentID, ToolCallID: toolCallIDFromContext(ctx), ApprovalID: ApprovalIDFromContext(ctx),
		TerminalID: info.ID, ChannelID: info.TargetID, Direction: direction,
		WorkspaceRoot: r.workspaceRoot,
		LocalPath:     strings.TrimSpace(args.LocalPath), RemotePath: strings.TrimSpace(args.RemotePath), Overwrite: args.Overwrite,
	})
}
