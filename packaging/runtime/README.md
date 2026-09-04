# `packaging/runtime/` — 预编译包内 **`.runtime/`** 占位

本目录在本地助手打包时整体合并进 `bundle/.runtime/`（`cp -a packaging/runtime/. bundle/.runtime/`），与 Go Node 的固定运行时布局对齐。

| 子路径 | 说明 |
|--------|------|
| **`prompt_context/`** | `soul.md` / `custom.md` 空文件占位；运行时正文以 `agents.db` 为权威，详见 **`prompt_context/README.md`**。 |
| **`externaltools/`** | **外置 CLI / 二进制 / shell 脚本**（与 skills 区分）；推荐清单 **`RECOMMENDED_CLI_TOOLS.md`**；索引 **`externaltools_menu.md`** |
| **`RECOMMENDED_CLI_TOOLS.md`** | 推荐 CLI 工具清单（如 OfficeCLI；需自行安装） |
| **`externaltools_menu.md`** | `.runtime/externaltools/` 工具索引（Agent 查阅） |
| **`externaltools/serve/`** | `dagents serve` 的 **startup.d** / **shutdown.d** 钩子目录；见 **`externaltools/serve/README.md`**。 |
| **`data/`** | 旧安装可能存在的兼容目录；新安装包不再包含或创建它，也不再把它作为 Agent workspace。 |
| **`skills/`** | 技能资源目录；默认 **`<运行根>/.runtime/skills`**；内置 **`write-skill`**、**`write-hook`**。 |
| **`plugins/`** | 全局 Hook plugin 占位；`.so` 由 `config.yaml` → `hooks.plugins` 加载；见 **`plugins/README.md`**。 |
| **`history/`** | Node 级历史占位；当前 Agent 的原始消息审计写入 **`<workspace>/.dagents/<agent_id>/history/YYYYMMDD/<session>.jsonl`**。 |
| **`memory/`** | Node 控制面持久化：**`sessions.db`** 与全局记忆 **`global.db`**；Agent 级记忆位于 **`<workspace>/.dagents/<agent_id>/memory/memory.db`**。 |

进程侧由 Go Node 按需创建 SQLite 和运行时目录；不会通过 Python 侧车读取会话或提示词数据。旧安装中的 `data/`、Node 根目录历史和旧长期记忆文件不会被自动删除或导入。
