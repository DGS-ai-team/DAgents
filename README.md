# DAgents

**本仓库为 DAgents 后端**：多 Agent / 工具调用场景下的 Python 运行时，提供 FastAPI 服务、CLI、注册中心（Register Center）与相关设计文档。  
**协议**：[MIT License](LICENSE)。**Web 前端**已独立，见 [DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI)。

- 会话化 Agent 运行时（工具调用、审批、流式输出）
- API 与 CLI、SQLite 可选持久化、Prometheus 指标（`/metrics`）
- 可独立部署的注册中心 **`register_center/`**（Agent 登记、发现、分组广播与中继）

> 项目持续迭代中，接口与实现可能变化。

## 功能概览

- **消息编排**：按 `session` 串行处理，支持 `human_message`、`tool_result`、`resume` 等。
- **工具调用**：OpenAI tool calling、审批执行、异步工具结果回灌。
- **上下文压缩**：静默 / 阻塞双阈值压缩，降低长会话成本。
- **流式输出**：SSE 返回 assistant / reasoning / usage / tool 等事件。
- **持久化**：可选 SQLite 会话存储，重启后可恢复上下文。
- **多 Agent 协作**：通过配置连接 Register Center，支持发现、广播与点对点投递（见 `app/harness/tools/`）。

## 项目结构（根目录）

```text
DAgents/
├── LICENSE                      # MIT
├── requirements.txt
├── .env.example                 # 环境变量模板（复制为 .env）
├── run_agent.py                 # CLI
├── run_agent_api.py             # FastAPI（Agent API）
├── run_register_center.py       # Register Center
├── run_dev_stack.py             # 本地同时拉起 API + Register Center
├── export_openapi_schema.py     # 导出 OpenAPI（前后端契约）
├── app/                         # 核心代码（config / core / harness / …）
├── register_center/             # Register Center 实现（FastAPI + 内存表）
├── tests/                       # 单元测试（`unittest`）
├── startup_scripts/             # 预编译包用：`start.sh`、`start_register_center.*`
├── scripts/                     # 辅助脚本（含 CI 构建）
├── packaging/                   # 离线安装说明等
├── prompt_context/              # 系统提示侧车文件
├── skills/                      # Agent / Cursor 技能等资源（按需）
├── doc/                         # 设计与实现文档
└── .github/workflows/           # CI（单测、PyInstaller 打包）
```

## 环境要求（概要）

| 安装方式 | Python | 其它 | 典型场景 |
|----------|--------|------|----------|
| **源码 + 在线 pip** | **3.11+**（CI 常用 **3.13**） | pip、可访问 PyPI | 开发 / 有外网服务器 |
| **源码 + 离线 wheels** | 与下载 wheel 时 **次版本一致** | wheel 与 OS/架构绑定 | 内网、隔离环境 |
| **PyInstaller 发布包** | **无需** Python | Linux 需兼容构建环境的 **glibc** | 解压即用 |

离线 wheel 说明见 **`packaging/OFFLINE_INSTALL.md`**。

## 安装与运行

### A）源码 + 在线 pip（推荐开发）

```bash
python -m venv .venv
source .venv/bin/activate   # Windows: .venv\Scripts\activate
pip install -r requirements.txt
cp .env.example .env
python run_agent_api.py
```

常用配置见 **`app/config/settings.py`**；监听地址可通过 **`API_HOST`** / **`API_PORT`**（默认 **`127.0.0.1:8000`**）等在 `.env` 中设置。

### B）源码 + 离线依赖

在可联网且与目标机 Python 次版本 / OS / 架构一致的环境执行 `pip download -r requirements.txt`，将源码与 `wheels/` 拷至离线机后 `pip install --no-index --find-links=wheels -r requirements.txt`。步骤细节见 **`packaging/OFFLINE_INSTALL.md`**。

### C）预编译二进制（PyInstaller）

无需 Python；Linux 注意 glibc 兼容性。解压后配置 `.env`，运行：

- **`dagents-api`** / **`dagents-api.exe`**：Agent API  
- **`dagents_register_center`** / **`dagents_register_center.exe`**：Register Center  

可选用 **`startup_scripts/linux/`**、**`startup_scripts/windows/`** 下的启动脚本。

### 常用启动命令（源码）

| 用途 | 命令 |
|------|------|
| Agent API | `python run_agent_api.py` |
| CLI | `python run_agent.py` |
| Register Center | `python run_register_center.py`（默认约 **`0.0.0.0:8010`**，见 `.env` / `REGISTER_CENTER_*`） |
| 本地联调（API + Register Center） | `python run_dev_stack.py` |

## 与前端（DAgentsUI）对接

前端仓库：**[github.com/DGS-ai-team/DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI)**。

建议流程：

1. 在本仓库根目录导出 OpenAPI：  
   `python export_openapi_schema.py --output <前端仓库路径>/openapi.json`
2. 在前端仓库生成类型（若前端已配置）：`pnpm gen:types`
3. 典型 HTTP：`POST /v1/messages`、SSE 相关路由（见下文与 **`app/harness/api/README.md`**）

## API 说明（简版）

更完整的契约与路由说明见 **`app/harness/api/README.md`** 与 OpenAPI 导出。

| 能力 | 路径（示例） |
|------|----------------|
| 健康检查 | `GET /health` |
| 创建会话 | `POST /v1/sessions` |
| 提交消息 | `POST /v1/messages` |
| 全局 SSE（按 client_id） | `GET /v1/streams?client_id=...` |
| 取消当前 turn | `POST /v1/sessions/{session_id}/cancel` |
| 释放会话 | `DELETE /v1/sessions/{session_id}` |
| 指标 | `GET /metrics`（若启用） |

Register Center 的 REST 约定见 **`register_center/README.md`** 与 **`doc/register_center-设计.md`**。

## 开发说明

- **`app/`** 与各子目录维护 **`README.md`** / **`REFERENCE.md`**（约定见 `.cursor/rules/`）。
- **`register_center/`** 由根目录 **`run_register_center.py`** 通过 `importlib` 加载 **`rc_app.py`**，便于不经安装直接运行。
- 运行测试：

```bash
python -m unittest discover -s tests -p "test_*.py" -v
```

部分用例依赖 **`OPENAI_API_KEY`** 或实时 LLM（见测试文件中的 skip 条件）；CI 安装依赖后应与发布流程一致。

- 指标实现见 **`app/observability/metrics.py`**。

## 配置与安全

- 勿将 **`.env`**、密钥、令牌提交到版本库。
- 开发环境建议使用独立凭据与隔离的 API Key。

## 文档入口

- [doc/README.md](doc/README.md)
- [app/README.md](app/README.md)
- [app/REFERENCE.md](app/REFERENCE.md)

## License

本项目采用 [MIT License](LICENSE) 开源许可。
