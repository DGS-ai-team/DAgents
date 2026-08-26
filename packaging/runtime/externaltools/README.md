# `.runtime/externaltools/` 外置工具目录

Agent 通过 **`bash_run`** 调用的 **shell 脚本、编译二进制与第三方 CLI**（与 **`skills/`** 的 Markdown 技能区分）。

| 类型 | 示例 |
|------|------|
| Shell 脚本 | `deploy.sh`、`backup.sh` |
| 编译二进制 | `officecli`、自研小工具 |
| 第三方 CLI | 从 Release 下载的 `.exe` / 无后缀可执行文件 |

执行 **`install.sh`**（Linux）或 **Windows 安装包** 后，本目录会加入 **`PATH`**，Agent 可直接用命令名调用；未进 PATH 时用 `externaltools/<name>`。

**`externaltools/serve/`** 为 `dagents serve` 的 startup/shutdown 钩子，**不是** Agent 业务工具，请勿放入业务 CLI。

推荐清单：**[`../RECOMMENDED_CLI_TOOLS.md`](../RECOMMENDED_CLI_TOOLS.md)**（含 **[OfficeCLI](OFFICECLI.md)**）。

索引文件：**[`../externaltools_menu.md`](../externaltools_menu.md)**（Agent 执行任务前应查阅；新增工具请同步更新）。

Node 启动时会将 **`externaltools_menu.md`** 与非占位可执行文件清单作为能力说明注入模型上下文（`node/internal/externaltools`）；完整工具 schema 仍通过 API `tools` 字段发送。
