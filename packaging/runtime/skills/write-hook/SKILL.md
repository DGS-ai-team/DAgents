---
name: write-hook
description: 指导编写 Go in-process Hook 插件（.so、Register、Hook/Host 契约、phase/mutation 与 config）；在用户新增或修改 Node Hook、skill hooks 目录或 config hooks.plugins 时使用。
---

## 适用场景

- 为 Node 增加 **turn 链路扩展**（改 prompt、tool 结果、session 变量、LLM 前拦截等）
- 编写 **全局 plugin**（`config.yaml` → `hooks.plugins`）或 **skill 级 plugin**（`skills/<name>/hooks/*.so`）
- 询问 Hook phase、mutation、`hook_store`、编译方式时

扩展**仅**支持 Go in-process plugin；与内置 Hook 共用同一 Registry 与 `Run(ctx, *Context, Host)` 契约。

## 架构（必读）

| 概念 | 说明 |
|------|------|
| **Hook 注册** | `.so` 经 `plugin.Open` 加载，导出 `Register` → 写入 Registry；**不是** `hook_store` |
| **`hook_store`** | session 级 KV，Hook 经 `Host.SessionStoreSet/Get` 读写；clear-context 会清空 |
| **内置 Hook** | policy、duplicate、tool_result 等，与 plugin 同接口 |

全局 plugin 与 skill plugin **仅加载路径不同**；运行时的函数签名、Context、Host 完全一致。

## 目录与交付

### 全局 plugin

```text
.runtime/plugins/<name>.so     # 或 config 中任意路径
config.yaml → hooks.plugins[]
```

### Skill 级 plugin

```text
skills/<skill-name>/hooks/<name>.so
```

- `load_skills` 加载该 skill 后自动 `plugin.Open` 目录下所有 `*.so`
- 注册名前缀：`skill/<skillName>/`（由 Node 自动加前缀）
- `unload_skills` / clear-context 后不再调用（Go plugin 无法从进程卸载）

## 编译

- 插件为 **`package main`**，与 **dagents-node 相同 Go 版本** 编译
- 导出符号：**`func Register(reg *hooks.PluginRegistrar) error`**

```bash
cd skills/my-skill/hooks/redact
go build -buildmode=plugin -o ../redact.so .
```

依赖：`github.com/DGS-ai-team/DAgents/node/internal/hooks`（与 Node 模块路径一致）。

## 最小 plugin 模板

```go
package main

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
)

func Register(reg *hooks.PluginRegistrar) error {
	reg.Register(&myHook{}, hooks.RegisterOpts{
		Priority: 100,
		Timeout:  2 * hooks.DefaultInlineHookTimeout,
		OnError:  hooks.OnErrorContinue,
	})
	return nil
}

type myHook struct{}

func (myHook) Name() string { return "my-hook" }

func (myHook) Phases() []hooks.Phase {
	return []hooks.Phase{hooks.PhaseToolAfterEach}
}

func (myHook) Run(ctx context.Context, hc *hooks.Context, host hooks.Host) (hooks.Result, error) {
	_ = ctx
	if hc.ToolAfterEach != nil {
		// 读 payload、改 mutation…
	}
	_ = host.SessionStoreSet("last_tool", hc.ToolAfterEach.ToolName)
	return hooks.Result{Action: hooks.ActionContinue}, nil
}
```

## config.yaml

```yaml
hooks:
  plugins:
    - path: .runtime/plugins/redact.so
      phases: [tool.after_each]
      priority: 100
      timeout_ms: 2000
      on_error: continue
  host:
    max_llm_calls: 2      # turn 内 Host.LLMComplete 配额
    # history_window: 100 # 可选；省略或 ≤0 时不截断 Context history
  duplicate_tool_call: …  # 内置 Hook，非 plugin
  tool_result: …
```

`phases` 在配置中用于加载器元数据；**实际挂载 phase 以 Hook.Phases() 为准**。

## Phase 列表

| Phase | 典型用途 |
|-------|----------|
| `message.enqueued` | 入队前审计、打标 |
| `turn.before_compress` | 压缩前策略（如 skip_compress） |
| `turn.before_step` | 步前 trace / 配额 |
| `prompt.build` | 改 system prompt |
| `llm.before_call` | StreamChat 前改 messages |
| `llm.after_call` | 改 assistant 输出 |
| `tool.before_each` | 工具参数 / 审批（policy 已占最高优先级） |
| `tool.after_each` | 改 tool 结果 for_client / for_history |
| `hitl.before_pause` / `hitl.after_resume` | HITL 前后 |
| `turn.done` / `turn.error` / `turn.cancel` | 结束 / 异常 |
| `session.lifecycle` | session 创建 |

## Context（只读输入）

Run 时 Node 注入（节选）：

- **按 phase 的 payload**：如 `hc.ToolAfterEach`、`hc.LLMAfterCall`、`hc.PromptBuild`
- **通用快照**：`hc.History`、`hc.SystemPrompt`、`hc.LoadedSkills`、`hc.Runtime`
- **`hc.SessionStore`**：当前 session 的 hook_store 快照（只读视图；写入请用 Host 或 mutation）

## Result 与 mutation

```go
return hooks.Result{
    Action: hooks.ActionContinue,
    Mutations: map[string]any{
        hooks.MutationAssistantContent: "…",
        hooks.MutationSystemPrompt:     "…",
        hooks.MutationMessages:         []llm.Message{…},
        hooks.MutationToolAfterEach: hooks.ToolAfterEachOutput{
            ForClient:  "…",
            ForHistory: "…",
        },
        hooks.MutationSessionStore: map[string]any{"key": "value"},
        hooks.MutationSkipCompress: true,
        hooks.MutationHistoryAppend: llm.UserMessage("…", ""),
    },
}, nil
```

**Action**：`continue`（默认）、`skip`（跳过后续同 phase hook）、`abort_turn`、`abort_tool`、`reject_enqueue`。

**硬边界**：不可任意 patch history 中间段；Hook 不能绕过 policy/HITL；`hook_store` 仅 session 作用域。

## Host API

| 方法 | 用途 |
|------|------|
| `Snapshot()` | 只读 history / system_prompt / loaded_skills / runtime / session_store |
| `SessionStoreGet/Set/Delete` | 读写 session 持久变量（落 SQLite `hook_store`） |
| `LLMComplete(ctx, req)` | 二次 LLM 补全；`ReuseSystemPrompt: true` 复用当前 system；受 `max_llm_calls` 限制 |

`hook_store` 的 key 建议带命名空间，避免 skill / 全局 plugin 冲突，例如 `skill/my-task/count`、`global/audit/id`。

## 编写流程

1. 选定 phase 与 priority（policy 等内置 Hook priority 更低数值 = 更先执行）
2. 在仓库外或 `hooks/<name>/` 下写 `main` 包 + `Register`
3. `go build -buildmode=plugin` 产出 `.so`
4. 全局：写入 `hooks.plugins`；skill 级：放入 `skills/<name>/hooks/`
5. 重启 Node（全局）或 `load_skills`（skill 级）验证
6. 用 `/context`、tool 调用或日志确认 Hook 执行

## 自检

- [ ] Go 版本与 dagents-node 一致，`.so` 能 `plugin.Open`
- [ ] 导出 `Register`，`Hook.Phases()` 覆盖目标 phase
- [ ] 未在 plugin 内阻塞 cancel（尊重 `ctx.Done()`）
- [ ] `hook_store` key 有命名空间，不与其他 plugin 冲突
- [ ] skill plugin 卸载或 clear-context 后行为符合预期
- [ ] 无密钥硬编码进 `.so` 或 mutation 输出

## 延伸阅读

- 设计细节：`docs/design/agent-hooks.md`
- 全局 plugin 目录说明：`packaging/runtime/plugins/README.md`
- 编写 skill 正文（非 Hook）：加载 **`write-skill`** skill
