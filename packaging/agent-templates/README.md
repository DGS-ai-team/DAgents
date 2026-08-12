# Agent 模板

v0.8+ 新建 Agent 时从此目录（及 Node 嵌入副本）加载模板。详见 [docs/design/agent-instance-model.md](../../docs/design/agent-instance-model.md)。

| 文件 | 说明 |
|------|------|
| `general.yaml` | 通用助手（含 soul / custom 预设） |
| `code-reviewer.yaml` | 代码审查（含 soul / custom 预设） |
| `ops-runner.yaml` | 运维执行（含 soul / custom 预设） |

`defaults.prompt_context.soul_md` / `custom_md` 会在创建 Agent 时写入该实例的侧车 `agent_prompt_context`；开关 `*_enabled` 仍写在 `config_snapshot`。

用户可在 `<runtime>/agent-templates/` 放置同名文件覆盖内置模板。发布包会附带本目录；Node 二进制亦 `go:embed` 同内容，安装后即使无磁盘目录也能列出模板。
