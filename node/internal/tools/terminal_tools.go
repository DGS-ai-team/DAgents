package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"strings"
)

const (
	terminalDefaultReadBytes = 12000
	terminalMaxReadBytes     = 65536
)

type terminalOpenArgs struct {
	ConfigID string `json:"config_id"`
	Shell    string `json:"shell"`
	CWD      string `json:"cwd"`
	Rows     int    `json:"rows"`
	Cols     int    `json:"cols"`
}

type terminalIDArgs struct {
	TerminalID string `json:"terminal_id"`
}

type terminalInputArgs struct {
	TerminalID string `json:"terminal_id"`
	Data       string `json:"data"`
}

type terminalReadArgs struct {
	TerminalID string `json:"terminal_id"`
	AfterSeq   uint64 `json:"after_seq"`
	MaxBytes   int    `json:"max_bytes"`
}

func terminalOpenToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{
		Name:        "terminal_open",
		Description: "按 terminal_config_list 返回的 config_id 打开一个可持续交互的终端会话。会话会在多次模型调用之间保留当前目录、环境和程序状态；打开后用 terminal_input 写入命令，用 terminal_read 读取输出。每个 Agent 的终端数量有限，达到上限时必须先 terminal_terminate 不再使用的终端。",
		Parameters: injectCallPurposeParam(map[string]any{
			"type": "object",
			"properties": map[string]any{
				"config_id": map[string]any{"type": "string", "description": "terminal_config_list 返回的配置 ID；不要自行猜测目标地址或通道 ID"},
				"shell":     map[string]any{"type": "string", "enum": []string{"powershell", "wsl", "cmd", "bash", "sh"}, "description": "终端 shell，可选；默认使用配置或平台默认值"},
				"cwd":       map[string]any{"type": "string", "description": "初始工作目录，可选"},
				"rows":      map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "description": "终端行数，可选，默认 24"},
				"cols":      map[string]any{"type": "integer", "minimum": 1, "maximum": 400, "description": "终端列数，可选，默认 80"},
			},
			"additionalProperties": false,
		}),
	}}
}

func terminalConfigListToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{
		Name:        "terminal_config_list",
		Description: "列出当前 Agent 已绑定、可用于打开终端的配置。结果只包含配置 ID、显示名称、主机/IP、端口、用户名和备注，不包含密码、私钥或 secret_ref。打开不同目标前必须先使用这里返回的 config_id。",
		Parameters:  injectCallPurposeParam(objectParams(map[string]any{}, "")),
	}}
}

func terminalInputToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{
		Name:        "terminal_input",
		Description: "向已打开的交互终端写入原始输入。可发送命令、回车、方向键或 Ctrl+C 等控制字符；不要用 bash_run 代替需要保持会话状态的交互操作。",
		Parameters: injectCallPurposeParam(objectParams(map[string]any{
			"terminal_id": map[string]any{"type": "string", "description": "terminal_open 返回的终端 ID"},
			"data":        map[string]any{"type": "string", "description": "要写入终端的原始文本或控制字符，命令通常需要显式包含换行符 \\n"},
		}, "terminal_id", "data")),
	}}
}

func terminalReadToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{
		Name:        "terminal_read",
		Description: "读取交互终端从指定序号之后产生的输出。首次读取 after_seq 留空；后续使用返回的 next_seq，避免重复读取。",
		Parameters: injectCallPurposeParam(objectParams(map[string]any{
			"terminal_id": map[string]any{"type": "string", "description": "terminal_open 返回的终端 ID"},
			"after_seq":   map[string]any{"type": "integer", "minimum": 0, "description": "上次读取返回的 next_seq，可选，默认 0"},
			"max_bytes":   map[string]any{"type": "integer", "minimum": 1, "maximum": terminalMaxReadBytes, "description": "最多读取的字节数，可选，默认 12000"},
		}, "terminal_id")),
	}}
}

func terminalTerminateToolDef() ToolDef {
	return simpleTerminalToolDef("terminal_terminate", "结束一个交互终端：先发送 Ctrl+C 尝试优雅退出，短暂等待后强制关闭兜底；调用结束时返回自上次 terminal_read 之后的未读输出，并移除终端会话。")
}

func terminalListToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{
		Name:        "terminal_list",
		Description: "列出当前 Agent 已打开的交互终端及其状态、目标和数量。",
		Parameters:  injectCallPurposeParam(objectParams(map[string]any{}, "")),
	}}
}

func simpleTerminalToolDef(name, description string) ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{
		Name:        name,
		Description: description,
		Parameters:  injectCallPurposeParam(objectParams(map[string]any{"terminal_id": map[string]any{"type": "string", "description": "terminal_open 返回的终端 ID"}}, "terminal_id")),
	}}
}

func objectParams(properties map[string]any, required ...string) map[string]any {
	params := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 && strings.TrimSpace(required[0]) != "" {
		params["required"] = required
	}
	return params
}

func (r *Registry) terminalBrokerOrError() (TerminalSessionBroker, error) {
	if r == nil || r.terminalBroker == nil {
		return nil, fmt.Errorf("terminal session broker is unavailable")
	}
	if strings.TrimSpace(r.agentID) == "" {
		return nil, fmt.Errorf("terminal owner agent is unavailable")
	}
	return r.terminalBroker, nil
}

func localTerminalConfig() TerminalConfigInfo {
	username := strings.TrimSpace(os.Getenv("USERNAME"))
	if username == "" {
		username = strings.TrimSpace(os.Getenv("USER"))
	}
	if current, err := user.Current(); err == nil && strings.TrimSpace(current.Username) != "" {
		username = strings.TrimSpace(current.Username)
	}
	return TerminalConfigInfo{
		ConfigID:    "local",
		DisplayName: "本机终端",
		Host:        "localhost",
		Username:    username,
		Remark:      "Node 所在主机",
		TargetKind:  executionTargetLocal,
		TargetID:    executionTargetLocal,
	}
}

func (r *Registry) listTerminalConfigs(ctx context.Context) ([]TerminalConfigInfo, error) {
	configs := make([]TerminalConfigInfo, 0, 1)
	if r != nil && r.localTerminalProvider != nil {
		configs = append(configs, localTerminalConfig())
	}
	if r == nil || r.terminalConfigResolver == nil {
		return configs, nil
	}
	remote, err := r.terminalConfigResolver.ListTerminalConfigs(ctx, r.agentID)
	if err != nil {
		return nil, err
	}
	configs = append(configs, remote...)
	return configs, nil
}

func (r *Registry) resolveTerminalConfig(ctx context.Context, configID string) (TerminalConfigInfo, error) {
	id := strings.TrimSpace(configID)
	if id == "" {
		return TerminalConfigInfo{}, fmt.Errorf("config_id is required; call terminal_config_list first")
	}
	local := localTerminalConfig()
	if id == local.ConfigID {
		if r == nil || r.localTerminalProvider == nil {
			return TerminalConfigInfo{}, fmt.Errorf("local terminal config is unavailable")
		}
		return local, nil
	}
	if r == nil || r.terminalConfigResolver == nil {
		return TerminalConfigInfo{}, fmt.Errorf("terminal config %q is not bound to this agent", id)
	}
	config, err := r.terminalConfigResolver.ResolveTerminalConfig(ctx, r.agentID, id)
	if err != nil {
		return TerminalConfigInfo{}, err
	}
	if strings.TrimSpace(config.ConfigID) == "" || strings.TrimSpace(config.TargetKind) == "" || strings.TrimSpace(config.TargetID) == "" {
		return TerminalConfigInfo{}, fmt.Errorf("terminal config %q is incomplete", id)
	}
	return config, nil
}

func decodeTerminalArgs(args json.RawMessage, dst any) error {
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(args, dst); err != nil {
		return fmt.Errorf("invalid terminal arguments: %w", err)
	}
	return nil
}

func (r *Registry) execTerminalOpen(ctx context.Context, raw json.RawMessage) (string, error) {
	broker, err := r.terminalBrokerOrError()
	if err != nil {
		return "", err
	}
	var args terminalOpenArgs
	if err := decodeTerminalArgs(raw, &args); err != nil {
		return "", err
	}
	config, err := r.resolveTerminalConfig(ctx, args.ConfigID)
	if err != nil {
		return "", err
	}
	info, err := broker.Open(ctx, r.agentID, TerminalRequest{
		Target:   ExecutionTarget{Kind: config.TargetKind, ID: config.TargetID},
		ConfigID: config.ConfigID,
		Context:  ExecutionContext{AgentID: r.agentID, SessionID: SessionIDFromContext(ctx), Target: ExecutionTarget{Kind: config.TargetKind, ID: config.TargetID}},
		CWD:      strings.TrimSpace(args.CWD),
		Shell:    strings.TrimSpace(args.Shell),
		Rows:     args.Rows,
		Cols:     args.Cols,
	})
	if err != nil {
		return "", err
	}
	return marshalTerminalResult(map[string]any{"terminal": info, "message": "terminal opened; use terminal_input and terminal_read to interact"})
}

func (r *Registry) execTerminalConfigList(ctx context.Context, _ json.RawMessage) (string, error) {
	configs, err := r.listTerminalConfigs(ctx)
	if err != nil {
		return "", err
	}
	return marshalTerminalResult(map[string]any{"configs": configs, "count": len(configs)})
}

func (r *Registry) execTerminalInput(ctx context.Context, raw json.RawMessage) (string, error) {
	broker, err := r.terminalBrokerOrError()
	if err != nil {
		return "", err
	}
	var args terminalInputArgs
	if err := decodeTerminalArgs(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.TerminalID) == "" {
		return "", fmt.Errorf("terminal_id is required")
	}
	if err := broker.Input(ctx, r.agentID, strings.TrimSpace(args.TerminalID), []byte(args.Data)); err != nil {
		return "", err
	}
	return marshalTerminalResult(map[string]any{"terminal_id": strings.TrimSpace(args.TerminalID), "written_bytes": len([]byte(args.Data))})
}

func (r *Registry) execTerminalRead(ctx context.Context, raw json.RawMessage) (string, error) {
	broker, err := r.terminalBrokerOrError()
	if err != nil {
		return "", err
	}
	var args terminalReadArgs
	if err := decodeTerminalArgs(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.TerminalID) == "" {
		return "", fmt.Errorf("terminal_id is required")
	}
	maxBytes := args.MaxBytes
	if maxBytes <= 0 {
		maxBytes = terminalDefaultReadBytes
	}
	if maxBytes > terminalMaxReadBytes {
		maxBytes = terminalMaxReadBytes
	}
	out, err := broker.ReadOutput(ctx, r.agentID, strings.TrimSpace(args.TerminalID), args.AfterSeq, maxBytes)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, chunk := range out.Chunks {
		b.Write(chunk.Data)
	}
	result := map[string]any{
		"terminal_id": strings.TrimSpace(args.TerminalID),
		"output":      b.String(),
		"next_seq":    out.NextSeq,
		"exited":      out.Exited,
	}
	if out.ReplayGap {
		result["replay_gap"] = true
	}
	if out.Exit != nil {
		result["exit"] = out.Exit
	}
	return marshalTerminalResult(result)
}

func (r *Registry) execTerminalTerminate(ctx context.Context, raw json.RawMessage) (string, error) {
	broker, err := r.terminalBrokerOrError()
	if err != nil {
		return "", err
	}
	var args terminalIDArgs
	if err := decodeTerminalArgs(raw, &args); err != nil {
		return "", err
	}
	id := strings.TrimSpace(args.TerminalID)
	if id == "" {
		return "", fmt.Errorf("terminal_id is required")
	}
	out, err := broker.Terminate(ctx, r.agentID, id)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, chunk := range out.Chunks {
		b.Write(chunk.Data)
	}
	result := map[string]any{
		"terminal_id": id,
		"status":      "terminated",
		"output":      b.String(),
		"next_seq":    out.NextSeq,
		"exited":      out.Exited,
		"graceful":    out.Graceful,
		"forced":      out.Forced,
	}
	if out.ReplayGap {
		result["replay_gap"] = true
	}
	if out.Exit != nil {
		result["exit"] = out.Exit
	}
	return marshalTerminalResult(result)
}

func (r *Registry) execTerminalList(_ context.Context, _ json.RawMessage) (string, error) {
	broker, err := r.terminalBrokerOrError()
	if err != nil {
		return "", err
	}
	items := broker.List(r.agentID)
	return marshalTerminalResult(map[string]any{"terminals": items, "count": len(items)})
}

func marshalTerminalResult(value any) (string, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
