# `packaging/runtime/` — 预编译包内 **`.runtime/`** 占位

本目录在本地助手打包时整体合并进 `bundle/.runtime/`（`cp -a packaging/runtime/. bundle/.runtime/`），与 Go Node 的固定运行时布局对齐。

| 子路径 | 说明 |
|--------|------|
| **`prompt_context/`** | `soul.md` / `custom.md` 空文件占位；已有安装中的旧 `user.md` 仅作为一次性迁移来源，详见 **`prompt_context/README.md`**。 |
| **`externaltools/`** | **外置 CLI / 二进制 / shell 脚本**（与 skills 区分）；推荐清单 **`RECOMMENDED_CLI_TOOLS.md`**；索引 **`externaltools_menu.md`** |
| **`RECOMMENDED_CLI_TOOLS.md`** | 推荐 CLI 工具清单（如 OfficeCLI；需自行安装） |
| **`externaltools_menu.md`** | `.runtime/externaltools/` 工具索引（Agent 查阅） |
| **`externaltools/serve/`** | `dagents serve` 的 **startup.d** / **shutdown.d** 钩子目录；见 **`externaltools/serve/README.md`**。 |
| **`data/`** | **临时工作区（workspace）**占位：脚本输出、中间产物等；**不含** `sessions.db`。 |
| **`skills/`** | 技能资源目录；默认 **`<运行根>/.runtime/skills`**；内置 **`write-skill`**、**`write-hook`**。 |
| **`plugins/`** | 全局 Hook plugin 占位；`.so` 由 `config.yaml` → `hooks.plugins` 加载；见 **`plugins/README.md`**。 |
| **`history/`** | 原始消息 JSONL 等；默认 **`.runtime/history/YYYYMMDD/<session>.jsonl`**。 |
| **`memory/`** | 持久化：**`sessions.db`** 会话库与旧版可选 **`long_term.md`** 迁移文件；默认 **`.runtime/memory`**。 |

进程侧由 Go Node 创建 SQLite、运行时目录并执行历史数据迁移；不会再通过旧 Python 侧车读取逻辑加载这些文件。
已有安装中的 `.runtime/agent/agent_id` 会在启动时迁移到 `.runtime/node/node_id`；新安装包不再创建这个旧目录。
