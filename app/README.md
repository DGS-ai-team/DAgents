# app — Python 包

Python **命名空间包**（PEP 420）。当前仅保留：

| 路径 | 用途 |
|------|------|
| [`config/env.py`](config/env.py) | 从仓库根 `.env` 加载环境变量（Manage / 开发脚本） |

Agent 运行时在 [`node/`](../node/README.md)（Go），仅提供本机 API；人机入口由独立 DAgents Desktop（Tauri）提供。原 `app/cli` Textual TUI 已移除。
