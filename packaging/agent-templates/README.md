# Agent 模板

v0.8+ 新建 Agent 时从此目录加载模板。详见 [docs/design/agent-instance-model.md](../../docs/design/agent-instance-model.md)。

| 文件 | 说明 |
|------|------|
| `general.yaml` | 通用助手（非沙箱） |
| `code-reviewer.yaml` | 代码审查（沙箱、只读工具） |
| `ops-runner.yaml` | 运维执行（沙箱、bash） |

用户可在 `<fs_root>/agent-templates/` 放置同名文件覆盖内置模板。
