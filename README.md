# DAgents

DAgents 是一个面向多 Agent / 工具调用场景的 Python 实验项目，提供：

- 会话化 Agent 运行时（含工具调用、审批、流式输出）
- API 服务与 CLI 客户端
- 本地持久化（SQLite）与可观测性（Prometheus metrics）
- 可扩展的注册中心（Register Center）

> 当前项目处于持续迭代阶段，接口与内部实现可能发生变化。

## 功能概览

- **消息编排**：按 session 串行处理消息，支持 `human_message`、`tool_result`、`resume` 等流程。
- **工具调用**：支持 OpenAI tool calling、审批执行、异步工具结果回灌。
- **上下文压缩**：支持静默/阻塞双阈值压缩，降低长会话上下文成本。
- **流式输出**：支持事件流（SSE）返回 assistant/reasoning/usage/tool 事件。
- **持久化**：可选 SQLite 会话存储，重启后可恢复上下文。

## 项目结构

```text
DAgents/
├── run_agent.py                 # CLI 启动入口
├── run_agent_api.py             # FastAPI 启动入口
├── run_register_center.py       # Register Center 启动入口
├── requirements.txt
├── .env.example
├── scripts/                     # 辅助脚本（如 CI 用 `scripts/ci/`）
├── packaging/                   # 分发说明模板（如离线安装摘要 OFFLINE_INSTALL.md）
├── .github/workflows/           # CI（单测、PyInstaller 打包等）
├── desktop/                     # 桌面壳预留（如 Tauri）
├── prompt_context/              # 系统提示上下文侧车文件
├── register_center/              # Register Center 实现
├── app/
│   ├── config/                  # 配置与环境变量加载
│   ├── context/                 # 会话上下文模型
│   ├── core/                    # 主 Agent 与摘要压缩核心逻辑
│   ├── harness/                 # API/CLI/服务/工具/队列/存储
│   ├── observability/           # 指标与观测
│   └── schemas/                 # 协议/数据模型
└── doc/                         # 设计与实现文档
```

## 环境要求（概要）

下表按「如何拿到软件」区分；**请优先看你采用的安装方式那一列**。

| 安装方式 | Python | 其它运行时要求 | 典型场景 |
|----------|--------|----------------|----------|
| **源码 + 在线 pip** | **3.11+**（开发与 CI 常用 **3.13**） | pip，可访问 PyPI | 开发机、有外网服务器 |
| **源码 + 自下载离线 wheels** | 与 `pip download` 时使用的 **Python 次版本** 一致（如 **3.13**） | pip；wheel 与 **OS/架构** 绑定，须在目标同类环境或联网机为该平台预下载 | 内网、隔离环境 |
| **PyInstaller 单文件 / 发布压缩包** | **无需**安装 Python | **Linux**：宿主 **glibc** 需不低于构建环境（在较新 Ubuntu 上打的包通常要求 **≥ Ubuntu 20.04 / glibc 2.31 一类**，更旧系统可能报 `GLIBC_x.xx not found`）；**Windows**：对应位数系统 | 解压即用 |

**补充**：离线 wheel **与 Python 次版本、操作系统、CPU 架构绑定**；更换版本或平台需在可联网机器上用 **目标环境的 Python** 重新执行 `pip download`（步骤见下文 B）及 `packaging/OFFLINE_INSTALL.md`）。

## 安装与运行方式

### A）源码 + 在线 pip（推荐开发）

**环境**：Python **3.11+**，pip，可访问 PyPI。

```bash
python -m venv .venv
source .venv/bin/activate   # Windows: .venv\Scripts\activate
pip install -r requirements.txt
cp .env.example .env
python run_agent_api.py
```

常用配置见 `app/config/settings.py`；`API_HOST` / `API_PORT` 等可通过 `.env` 设置。

### B）源码 + 离线依赖（自行 `pip download`）

在无 PyPI 的机器上运行前，请在 **可联网**、且 **Python 次版本 / OS / 架构与目标机一致**（或兼容）的环境下预先下载依赖 wheel。

**1）联网机上下载 wheel（示例以 Linux + Python 3.13，仓库根目录为准）：**

```bash
python3.13 -m venv .venv
source .venv/bin/activate   # Windows: .venv\Scripts\activate
pip install --upgrade pip wheel
mkdir -p wheels
pip download -r requirements.txt -d wheels
```

**2）将本仓库源码（可用 `git clone` / 拷贝，排除 `.git` 与 `.venv` 亦可）与 `wheels/` 目录一并拷到离线机。**

**3）离线机安装与启动：**

```bash
python3.13 -m venv .venv && source .venv/bin/activate
pip install --no-index --find-links=wheels -r requirements.txt
cp .env.example .env
python run_agent_api.py
```

若目标为 **其它 Python 版本**（如 3.12）或 **Windows/macOS**，不可用为他平台下载的 wheel，须在对应环境的联网机上用该环境的 Python **重新执行** `pip download`。更完整的说明见 **`packaging/OFFLINE_INSTALL.md`**。

### C）预编译二进制（PyInstaller 发布包）

**环境**：无需 Python；Linux 用户需满足 **与构建机兼容的 glibc**（过旧发行版会出现 `libpython… GLIBC_x.xx not found`）。详见发布页说明；启动可使用 `startup_scripts/` 下脚本。

解压后配置 `.env`，运行目录中的 `dagents-api` / `dagents_register_center`（或对应 `.exe`）。

### 启动命令（源码方式）

- **API 服务**：`python run_agent_api.py`
- **CLI 客户端**：`python run_agent.py`
- **Register Center**：`python run_register_center.py`
- **一键开发栈（后端）**：`python run_dev_stack.py`（默认同时启动 API + Register Center）

默认 API 监听 `127.0.0.1:8000`。

## 前后端分仓说明

- **后端**：本仓库负责 API、Agent Runtime、`register_center/`（Register Center）与相关文档。
- **前端**：已独立为 **[DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI)**（React UI、SSE 消费与审批交互）；请在浏览器打开该仓库查看开发与对接说明。
- **前后端对接流程（建议保留在前端仓库文档或 CI 中）**：
  - 从后端导出 OpenAPI：`python export_openapi_schema.py --output <前端仓库路径>/openapi.json`
  - 在前端仓库生成 TS 类型：`pnpm gen:types`
  - 对接 API：`POST /v1/messages`、`GET /v1/streams?client_id=...`

## API 说明（简版）

- `POST /v1/messages`：提交消息
- `GET /v1/streams/{request_id}`：订阅 SSE 事件流
- 其他接口：待补充

详细接口说明请参考 `app/harness/api/README.md` 与实现代码。

## 开发说明

- 代码主入口在 `app/` 下，按目录维护 `README.md` 与 `REFERENCE.md`。
- `register_center/` 与根目录 `run_register_center.py` 配套；启动脚本通过 `importlib` 按文件路径加载 `rc_app.py`（便于在未安装为 site-package 时运行）。
- 运行测试（需先 `pip install -r requirements.txt`）：

```bash
python -m unittest discover -s tests -p "test_*.py" -v
```

- 指标相关可参考 `app/observability/metrics.py` 与 `/metrics` 暴露逻辑。

## 配置与安全

- 请勿提交 `.env`、密钥、令牌等敏感信息。
- 建议在开发环境使用独立数据库与隔离 API Key。

## 路线图（Roadmap）

- [ ] 完整 API 文档与错误码说明
- [ ] 更细粒度的压缩策略与回归测试
- [ ] 更完善的部署脚本（Docker/Compose，待补充）
- [ ] 权限与多租户能力（待补充）

## 文档入口

- [doc/README.md](doc/README.md)
- [app/README.md](app/README.md)
- [app/REFERENCE.md](app/REFERENCE.md)

## License

待补充
