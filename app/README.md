# `app/` 应用包

> **【已弃用 — Agent 运行时】** `app/harness/`（FastAPI + AgentService + turn loop）为 **遗留 Python Agent 后端**。  
> **新部署 / 本地助手** → [Go Agent Node](../node/README.md)（`go run ./node/cmd/dagents-node`）。  
> **仍维护用途** → DAgentsUI OpenAPI、A2A（`agent_peer`）、Register Center、v0.2.0 发布包。  
> 说明集中见 [`deprecated_backend.py`](deprecated_backend.py) 与 [docs/architecture/overview.md](../docs/architecture/overview.md)。

Python **命名空间包**（PEP 420），从仓库根将根目录加入 `sys.path` 后即可 `import app.*`。

## 角色

| 子目录 | 职责 | 弃用 |
|--------|------|------|
| **`cli/`** | Textual TUI（`dagents chat`），连 **Go Node** | 否（Client） |
| **`config/`** | `Settings`、环境变量 | 部分（API 栈专用） |
| **`context/`** | 会话上下文模型 | 是（Python 运行时） |
| **`core/`** | 主 Agent、summary 压缩 | 是 |
| **`harness/`** | API、service、tools、queue | 是 |
| **`deprecated_backend.py`** | 弃用文案与 `/health` 元数据 | — |

## 入口

| 用途 | 命令 | 状态 |
|------|------|------|
| FastAPI Agent API | `python run_agent_api.py` | **已弃用** |
| 开发栈 API + RC | `python run_dev_stack.py` | **已弃用**（A2A 联调） |
| Textual TUI | `python -m app.cli.main chat …` | 活跃（连 Go Node） |

## 模块索引

详见 **`REFERENCE.md`**；各子目录另有 **`README.md`**。
