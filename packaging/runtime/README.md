# `packaging/runtime/` — 预编译包内 **`.runtime/`** 占位

本目录在 **PyInstaller 发布包** 打 zip 时 **整体合并进 `bundle/.runtime/`**（`cp -a packaging/runtime/. bundle/.runtime/`），与 **`app/config/runtime_layout.py`** 中 **`.runtime/skills`** 等固定相对路径布局对齐。

| 子路径 | 说明 |
|--------|------|
| **`prompt_context/`** | 侧车 **`soul.md` / `user.md` / `custom.md`**：仓库内为 **空文件占位**（无预设文案）；详见 **`prompt_context/README.md`**。 |
| **`scripts/`** | 独立脚本区占位（与 **skills** 区分）；推荐第三方 CLI 见 **`RECOMMENDED_CLI_TOOLS.md`**；索引 **`scripts_menu.md`** |
| **`RECOMMENDED_CLI_TOOLS.md`** | 推荐 CLI 工具清单（如 OfficeCLI；需自行安装） |
| **`scripts_menu.md`** | `.runtime/scripts/` 工具索引（Agent 查阅） |
| **`scripts/serve/`** | `dagents serve` 的 **startup.d** / **shutdown.d** 钩子目录；见 **`scripts/serve/README.md`**。 |
| **`data/`** | **临时工作区（workspace）**占位：脚本输出、中间产物等；**不含** `sessions.db`。 |
| **`skills/`** | 技能资源目录；默认 **`<运行根>/.runtime/skills`**；内置 **`write-skill`**、**`write-hook`**。 |
| **`plugins/`** | 全局 Hook plugin 占位；`.so` 由 `config.yaml` → `hooks.plugins` 加载；见 **`plugins/README.md`**。 |
| **`history/`** | 原始消息 JSONL 等；默认 **`.runtime/history/YYYYMMDD/<session>.jsonl`**。 |
| **`memory/`** | 持久化：**`sessions.db`** 会话库与可选 **`long_term.md`**；默认 **`.runtime/memory`**。 |
| **`agent/`** | 如 **`agent_id`** 持久化文件等；默认 **`.runtime/agent`**。 |

进程侧：**`prompt.py`** 仍会在首次读侧车前 **补建缺失的空文件**（不覆盖已有内容），与解压包内已有占位互为兜底。
