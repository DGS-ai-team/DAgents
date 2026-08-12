# app — Python 包

Python **命名空间包**（PEP 420）。当前仅保留：

| 路径 | 用途 |
|------|------|
| [`config/env.py`](config/env.py) | 从仓库根 `.env` 加载环境变量（Manage / 开发脚本） |

Agent 运行时在 [`node/`](../node/README.md)（Go）；人机入口为 Node 内嵌 **Web UI**（`/ui/`）。原 `app/cli` Textual TUI 已在 Phase 4 移除。
