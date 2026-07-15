# app — Python 包

Python **命名空间包**（PEP 420）。当前仅保留：

| 路径 | 用途 |
|------|------|
| [`cli/`](cli/README.md) | Textual TUI Client（连接 **Go Agent Node**） |
| [`config/env.py`](config/env.py) | 从仓库根 `.env` 加载环境变量（Manage / 开发脚本） |

Agent 运行时已迁移至 [`node/`](../node/README.md)（Go）。

```bash
python -m app.cli.main chat --config packaging/agent-client/config.yaml
```
