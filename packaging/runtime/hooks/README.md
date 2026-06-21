# 外部 Hook 示例脚本

与 [docs/design/agent-hooks.md](../../../docs/design/agent-hooks.md) Phase B 配套。

## 安装

```bash
mkdir -p .runtime/hooks
cp packaging/runtime/hooks/redaction.sh .runtime/hooks/
chmod +x .runtime/hooks/redaction.sh
```

## redaction.sh

在 `llm.after_call` 阶段对 assistant 正文做简单脱敏（OpenAI 风格 `sk-…` token、`Bearer …`、`api_key=` 等）。

### config.yaml 片段

```yaml
hooks:
  enabled: true
  entries:
    - name: llm-redact
      type: command
      command: ["/path/to/.runtime/hooks/redaction.sh"]   # 建议使用绝对路径
      phases: [llm.after_call]
      allowed_paths:
        - /path/to/.runtime/hooks
      on_error: continue
```

`hooks.enabled: true` 仅对 `http` / `command` 必填；`journal` 类型不受此限制。

## 契约

- **stdin**：`HookContext` JSON（字段 snake_case，如 `llm_after_call.assistant_content`）
- **stdout**：`Result` JSON，例如 `{"action":"continue","mutations":{"assistant_content":"..."}}`

详见 `node/internal/hooks/context_json.go` 与 `node/internal/hooks/external_command.go`。
