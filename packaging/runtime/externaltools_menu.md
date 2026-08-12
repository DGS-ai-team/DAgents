# `.runtime/externaltools/` 外置工具索引

Agent 执行任务前可查阅本文件。目录 **`externaltools/`** 可放置 **shell 脚本、编译二进制、第三方 CLI**（不仅是 `.sh` 脚本）。

发布包**不内置**第三方 CLI。推荐工具见 **[`RECOMMENDED_CLI_TOOLS.md`](RECOMMENDED_CLI_TOOLS.md)**。

| 名称 | 类型 | 命令 | 说明 |
|------|------|------|------|
| （用户自行安装） | binary / script | — | 将可执行文件或脚本放入 `externaltools/`；例如 **[OfficeCLI](RECOMMENDED_CLI_TOOLS.md#officecli-安装摘要)** |

**调用方式**

- 已加入 `PATH`：直接 `officecli --version`
- 未加入 `PATH`：`externaltools/officecli --version` 或 `.runtime/externaltools/officecli`

**与 skills 的区别**：复杂用法、参数约定、审批策略等写在 **`skills/<name>/SKILL.md`**；本目录只放可执行产物本身。
