# Go Hook Plugins

Node Hook 扩展仅支持 **in-process Go plugin**（`.so`），与内置 Hook 使用同一接口：

```go
type Hook interface {
    Name() string
    Phases() []Phase
    Run(ctx context.Context, hc *Context, host Host) (Result, error)
}
```

## 编译

- 插件为 `package main`，与 **dagents-node 相同 Go 版本** 编译。
- 导出符号：`func Register(reg *hooks.PluginRegistrar) error`

```bash
go build -buildmode=plugin -o redact.so ./plugins/redact/
```

## 注册

- **全局**：`config.yaml` → `hooks.plugins[]`
- **Skill 级**：`skills/<name>/hooks/*.so`，随 `load_skills` 加载

## Host 能力

Plugin 通过 `Host` 访问 session 运行时（非反射）：

- `Snapshot()` — history 窗口、system prompt、loaded skills、session store
- `SessionStoreGet/Set/Delete` — session 持久变量（SQLite `hook_store`）
- `LLMComplete` — 受 `hooks.host.max_llm_calls` 配额限制

详见 `docs/design/agent-hooks.md`。
