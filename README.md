# DAgents

多 Agent / 工具调用场景下的 **Agent 运行时**仓库：含 **Go 本地助手栈**（Agent Node + 终端 Client）与 **Python FastAPI 栈**（Web API、A2A、Register Center）。  
**协议**：[MIT License](LICENSE)。**Web 前端**独立仓库：[DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI)。

**标记版本：`v0.2.0`（2026-05-30）** · [变更记录](CHANGELOG.md)

## 两条运行时路径

| 场景 | 栈 | 快速开始 |
|------|-----|----------|
| **本地终端助手**（推荐开发主线） | Go Node + Textual/Go Client | 见 [本地助手](#本地助手-go-node) |
| **Web UI / OpenAPI / A2A**（遗留） | Python FastAPI + Register Center **（已弃用 Agent 运行时）** | 见 [Python API](#python-api已弃用--agent-运行时) |

架构选型说明：[docs/architecture/overview.md](docs/architecture/overview.md)。

---

## 本地助手（Go Node）

单进程 Agent：**LLM turn**、工具、SQLite 会话、审批、skills、compression、triggers（含日历 schedule）。

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
# 编辑 config.yaml（默认 llm.mock=true，无需 API Key）

# 终端 1 — Node
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml

# 终端 2 — Client（二选一）
python -m app.cli.main chat --config packaging/agent-client/config.yaml   # Textual TUI（首选）
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml tui  # Go REPL（兜底）
```

| 目录 | 说明 |
|------|------|
| [`node/`](node/README.md) | Agent Node |
| [`client/`](client/README.md) | Go REPL Client |
| [`shared/config/`](shared/config/README.md) | 共用 YAML 配置 |
| [`app/cli/`](app/cli/README.md) | Python Textual TUI（连 Go Node） |

文档：[docs/architecture/local-assistant.md](docs/architecture/local-assistant.md)。

---

## Python API（**已弃用 — Agent 运行时**）

> 仍维护用于 **DAgentsUI**、**OpenAPI**、**A2A**、**Register Center** 与 v0.2.0 发布包。  
> 本地助手与新功能请使用 [Go Node](#本地助手-go-node)。启动时会打 WARNING，API 响应含 `Deprecation` 头。

FastAPI 服务、Prometheus、Register Center、**agent_peer** A2A。

```bash
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
cp .env.example .env
python run_agent_api.py          # 默认 127.0.0.1:8000
python run_register_center.py    # 可选，默认 :8010
python run_dev_stack.py          # API + Register Center
```

| 能力 | 说明 |
|------|------|
| HTTP / SSE | 会话、消息、流式、审批 |
| SQLite | 可选会话持久化 |
| Register Center | Agent 登记、广播、中继 |
| 指标 | `GET /metrics`（可选） |

文档：[docs/architecture/python-runtime.md](docs/architecture/python-runtime.md)、[docs/api-reference.md](docs/api-reference.md)。

---

## 项目结构

```text
DAgents/
├── node/                 # Go Agent Node
├── client/               # Go REPL Client
├── shared/config/        # Node + Client 共用 YAML
├── app/                  # Python：API、harness、cli TUI
├── register_center/      # Register Center（Python FastAPI）
├── packaging/            # 配置示例、PyInstaller runtime、离线安装
├── docs/                 # 技术文档（见 docs/README.md）
├── tests/                # Python unittest
├── run_agent_api.py
├── run_register_center.py
└── requirements.txt
```

---

## 测试

```bash
# Go（node/client 变更时 CI 同样执行）
go test ./node/... ./client/... ./shared/config/...

# Python（与 .github/workflows/pr-tests.yml 一致）
pip install -r requirements.txt
python -m unittest discover -s tests -p "test_*.py" -v
```

详见 [tests/README.md](tests/README.md)。

---

## 版本与兼容性

- **Python**：3.11+ 可运行；**CI 验证 3.13**。见 [docs/os-compatibility.md](docs/os-compatibility.md)。
- **Go**：见 `go.work`（`node`、`client`、`shared/config`）。
- **0.x 预览**：不兼容变更写入 [CHANGELOG.md](CHANGELOG.md)。

---

## 预编译包（Python）

无需 Python 环境；Linux 注意 glibc。资产见 GitHub Releases：

- **`dagents-backend-*`**：api + register_center + cli + `.runtime`
- **`dagents serve`**：后台启动 Python Agent API（**非** Go Node）
- **`dagents chat`**：Textual TUI，连 **Go Node**（需单独启动 `dagents-node`）

离线安装：[packaging/OFFLINE_INSTALL.md](packaging/OFFLINE_INSTALL.md)。

---

## OpenAPI 与前端

```bash
python scripts/ci/export_openapi_for_frontend.py --frontend ../DAgentsUI
```

契约针对 **Python API**；Go Node API 见 [docs/architecture/agent-node-api.md](docs/architecture/agent-node-api.md)。

---

## 问题反馈与安全

- **Issues**：注明栈（Go Node / Python API）、版本、OS、复现步骤。
- **安全漏洞**：[SECURITY.md](SECURITY.md)（勿在公开 Issue 贴 exploit）。

---

## 文档入口

| 文档 | 说明 |
|------|------|
| [docs/README.md](docs/README.md) | 技术文档索引 |
| [docs/architecture/overview.md](docs/architecture/overview.md) | 双栈架构 |
| [docs/roadmap.md](docs/roadmap.md) | 路线图 |
| [register_center/README.md](register_center/README.md) | Register Center API |
| [CHANGELOG.md](CHANGELOG.md) | 版本变更 |

各代码目录 **`README.md`** / **`REFERENCE.md`** 随源码维护。

---

## License

[MIT License](LICENSE)
