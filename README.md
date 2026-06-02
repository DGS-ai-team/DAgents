<p align="center">
  <h1 align="center">DAgents</h1>
  <p align="center">
    本地单 Agent 助手运行时 — Go Agent Node + 双终端 Client
    <br />
    <a href="docs/architecture/local-assistant.md"><strong>快速上手 »</strong></a>
    ·
    <a href="CHANGELOG.md">变更记录</a>
    ·
    <a href="docs/architecture/agent-node-api.md">HTTP/SSE API</a>
    ·
    <a href="https://github.com/DGS-ai-team/DAgentsUI">Web UI（独立仓库，尚未适配 v0.2.0）</a>
  </p>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="MIT License"></a>
  <a href="CHANGELOG.md"><img src="https://img.shields.io/badge/release-v0.2.0-20260530-green" alt="v0.2.0"></a>
  <a href="go.work"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white" alt="Go 1.25+"></a>
  <a href="requirements.txt"><img src="https://img.shields.io/badge/Python-3.11+-3776AB?logo=python&logoColor=white" alt="Python 3.11+"></a>
  <a href="https://github.com/DGS-ai-team/DAgents/actions/workflows/pr-tests.yml"><img src="https://github.com/DGS-ai-team/DAgents/actions/workflows/pr-tests.yml/badge.svg" alt="PR Tests"></a>
  <a href="https://github.com/DGS-ai-team/DAgentsUI"><img src="https://img.shields.io/badge/Web%20UI-尚未适配%20v0.2.0-orange" alt="Web UI lagging"></a>
</p>

---

## 简介

**DAgents** 面向需要 **工具调用、人工审批（HITL）、会话持久化** 的 Agent 场景。当前版本（**v0.2.0**）以 **Go Agent Node** 为唯一运行时：单进程承载 LLM turn loop、内置工具、SQLite 会话、skills、上下文压缩与 trigger 调度。

终端交互提供 **两种 Client**，共用一份 YAML 配置，按环境任选：

| Client | 适用场景 | 命令 |
|--------|----------|------|
| **Python Textual TUI** | 现代终端（WSL、新 Linux、Windows Terminal） | `dagents chat` |
| **Go bubbletea TUI** | SSH 全屏、脚本、无 Python 环境 | `dagents-client tui` |
| **Go plain REPL** | 极老 SSH / RHEL6 / `TERM=dumb` | `dagents-client tui --plain` |
| **Web UI（DAgentsUI）** | 浏览器 · **当前未适配 v0.2.0** | 暂勿用于 Go Node 联调 |

另含 **Register Center**（Python）：Agent 登记与 A2A relay/broadcast 控制面，**不是** Agent 运行时。

> **v0.2.0 说明**：原 Python FastAPI Agent API（`run_agent_api.py`）已从仓库移除。详见 [CHANGELOG.md](CHANGELOG.md)。

> **Web UI 状态**：独立前端仓库 [DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI) **仍基于旧版 Python Agent API / OpenAPI 契约，尚未跟进 v0.2.0 的 Go Agent Node**。浏览器联调暂不可用；请优先使用 **`dagents chat`** 或 **`dagents-client tui`**。Web UI 适配计划在 DAgentsUI 仓库单独推进。

---

## 功能特性

- **LLM Turn 编排** — OpenAI 兼容 API；流式 assistant / reasoning / tool 事件
- **内置工具** — `bash_run`、文件读写与替换、skills 加载、trigger 管理、**临时子 Agent**
- **HITL** — 工具审批、`ask_user_information`；Client 侧非阻塞队列 + resume
- **Triggers** — `interval`、`fire_at`、日历 **`schedule`**（含 cmd 门控）
- **Session** — 多会话、SQLite 持久化、context 压缩（silent / blocking）
- **Policy** — `.runtime/policy/*.approval.txt` 本地审批策略
- **可观测** — SSE `usage`（prompt/completion、cache hit）；结构化 stderr 日志
- **同包发布** — Release 资产 `dagents-local-assistant-*`（Node + 双 Client + 示例配置）

---

## 架构

```mermaid
flowchart LR
  subgraph clients [终端 Client]
    PY["Python Textual<br/>dagents chat"]
    GO["Go Client<br/>dagents-client tui"]
  end

  subgraph runtime [本机运行时]
    NODE["Agent Node (Go)<br/>HTTP + SSE · SQLite"]
  end

  subgraph optional [可选]
    RC["Register Center<br/>A2A 登记 / relay"]
  end

  PY -->|127.0.0.1:18765| NODE
  GO -->|127.0.0.1:18765| NODE
  RC -.->|relay / broadcast| NODE
```

> **Web UI**：图中未包含 [DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI)；该前端仍对接已移除的 Python API，**与当前 Go Node 不兼容**。

配置：`packaging/agent-client/config.yaml`（Node 读 `listen` / `llm`；Client 读 `local.endpoint`）。

---

## 快速开始

### 前置条件

| 组件 | 要求 |
|------|------|
| **Go** | 1.25+（见 [`go.work`](go.work)） |
| **Python**（Textual TUI / Register Center） | 3.11+；CI 验证 3.13 |
| **LLM** | 默认 `llm.mock: true`，**无需 API Key** 即可联调 |

### 1. 克隆与配置

```bash
git clone https://github.com/DGS-ai-team/DAgents.git
cd DAgents

cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
# 可选：编辑 config.yaml（真实 LLM 时设 llm.mock: false 并 export OPENAI_API_KEY）
```

### 2. 启动 Agent Node

```bash
go run ./node/cmd/dagents-node -config packaging/agent-client/config.yaml
```

探活：

```bash
curl -s http://127.0.0.1:18765/health | jq .
# 或
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml probe
```

### 3. 启动 Client（二选一）

**Textual TUI（推荐）**

```bash
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
python -m app.cli.main chat --config packaging/agent-client/config.yaml
```

**Go TUI**

```bash
# 全屏（默认）
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml tui

# 老终端行模式
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml tui --plain
```

恢复已有 session：

```bash
dagents chat --session sess-xxxxxxxx
dagents-client tui sess-xxxxxxxx
```

---

## 配置说明

| 字段 | 说明 |
|------|------|
| `listen.host` / `listen.port` | Node 监听地址（默认 `127.0.0.1:18765`） |
| `local.endpoint` | Client 连接 URL |
| `llm.mock` | `true` 时使用 mock LLM（开发默认） |
| `llm.api_key_env` | 真实 LLM 的 API Key 环境变量名 |
| `data_dir` | SQLite 与运行时数据（默认 `./.runtime/data`） |
| `fs_root` | 工具可访问文件根 |
| `triggers.enabled` | 触发器调度开关 |
| `child_agents.enabled` | 临时子 Agent 开关 |

完整示例：[packaging/agent-client/config.example.yaml](packaging/agent-client/config.example.yaml)

查找顺序：`--config` / `-config` → 环境变量 `DAGENTS_CONFIG` → 默认路径。

---

## Client 命令速查

### Python `dagents`

```bash
dagents chat [--config PATH] [--session ID] [--show-reasoning]
dagents show session [--config PATH]
dagents delete session SESSION_ID [--config PATH]
dagents register-center          # 启动 Register Center
```

TUI 内常用：`/help` `/status` `/context` `/skill` `/children` `/clear` `/cancel` `/exit`

### Go `dagents-client`

```bash
dagents-client probe
dagents-client tui [--plain] [--show-reasoning] [session_id]
dagents-client chat "你好"        # 一次性非交互
```

TUI 内常用：`/help` `/status` `/context` `/skill` `/children` `/tools verbose|brief` `/reasoning on|off`

---

## Register Center（可选）

Agent 登记、分组发现、消息 **broadcast** / **relay**（A2A 控制面）。

```bash
pip install -r requirements.txt
python run_register_center.py     # 默认 0.0.0.0:8010
# 或
python run_dev_stack.py
```

文档：[register_center/README.md](register_center/README.md)

---

## 项目结构

```text
DAgents/
├── node/                    # Go Agent Node（运行时）
├── client/                  # Go TUI Client（full + plain REPL）
├── shared/config/           # Node / Client 共用 YAML 解析
├── app/cli/                 # Python Textual TUI
├── register_center/         # Register Center（FastAPI）
├── packaging/
│   └── agent-client/        # config.example.yaml、启动脚本
├── docs/                    # 架构与 API 文档
├── tests/                   # Python CLI / RC 单测
├── go.work
├── requirements.txt
└── run_register_center.py
```

各目录 [`README.md`](node/README.md) / [`REFERENCE.md`](node/REFERENCE.md) 随源码维护。

---

## 开发与测试

```bash
# Go 全量单测（与 CI go-ac 一致）
go test ./node/... ./client/... ./shared/config/...

# Python CLI / Register Center
pip install -r requirements.txt
python -m unittest discover -s tests -p "test_*.py" -v
```

| 工作流 | 说明 |
|--------|------|
| [pr-tests.yml](.github/workflows/pr-tests.yml) | PR：Go + Python unittest |
| [go-ac.yml](.github/workflows/go-ac.yml) | Go Node / Client 持续集成 |
| [build-and-release.yml](.github/workflows/build-and-release.yml) | Release 构建与发布 |

详见 [tests/README.md](tests/README.md)。

---

## 预编译包

GitHub **Releases** 提供 **`dagents-local-assistant-*`**（Linux tarball / Windows zip）：

| 二进制 | 说明 |
|--------|------|
| `dagents-node` | Agent Node |
| `dagents-client` | Go TUI（`tui` / `tui --plain`） |
| `dagents-cli` / `dagents` | Python Textual TUI |

解压后：

```bash
# 终端 1
./bin/dagents-node -config config.yaml

# 终端 2（任选）
./bin/dagents-cli chat -config config.yaml
./bin/dagents-client -config config.yaml tui
```

打包与离线安装：[packaging/agent-client/README.md](packaging/agent-client/README.md)、[packaging/OFFLINE_INSTALL.md](packaging/OFFLINE_INSTALL.md)

---

## 文档

| 文档 | 内容 |
|------|------|
| [docs/README.md](docs/README.md) | 文档索引 |
| [docs/architecture/overview.md](docs/architecture/overview.md) | 架构总览 |
| [docs/architecture/local-assistant.md](docs/architecture/local-assistant.md) | 本地助手联调 |
| [docs/architecture/agent-node-api.md](docs/architecture/agent-node-api.md) | Go Node HTTP/SSE 契约 |
| [docs/architecture/child-agent-tools.md](docs/architecture/child-agent-tools.md) | 子 Agent 工具 |
| [docs/architecture/go-node-compatibility.md](docs/architecture/go-node-compatibility.md) | 老旧 OS / 静态构建 |
| [CHANGELOG.md](CHANGELOG.md) | 版本变更（**v0.2.0**） |
| [DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI) | Web 前端（**独立仓库，尚未适配 v0.2.0 / Go Node API**） |

> 历史文档 [docs/api-reference.md](docs/api-reference.md) 描述已移除的 Python Agent API，仅作参考；**DAgentsUI 当前仍依赖该旧契约**。

---

## 版本与兼容性

| 项 | 说明 |
|----|------|
| **当前版本** | **v0.2.0**（2026-05-30，0.x 预览） |
| **Go** | 1.25+（`node` / `client` / `shared/config`） |
| **Python** | 3.11+ 可运行；CI 验证 3.13 |
| **破坏性变更** | 1.0 前仍可能出现；见 [CHANGELOG.md](CHANGELOG.md) |
| **Web UI（DAgentsUI）** | **落后于本仓库 v0.2.0**；未对接 Go Node，暂无可用浏览器 Client |

OS 与 glibc 说明：[docs/os-compatibility.md](docs/os-compatibility.md)

---

## 反馈与安全

- **Bug / 功能请求**：[GitHub Issues](https://github.com/DGS-ai-team/DAgents/issues) — 请注明版本、OS、Client 类型与复现步骤
- **安全漏洞**：[SECURITY.md](SECURITY.md)（请勿在公开 Issue 中提交 exploit）

---

## License

[MIT License](LICENSE)
