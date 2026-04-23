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
├── frontend/                    # React 前端（新增，Web UI）
├── desktop/                     # 桌面壳预留（如 Tauri）
├── prompt_context/              # 系统提示上下文侧车文件
├── register-center/             # Register Center 实现
├── app/
│   ├── config/                  # 配置与环境变量加载
│   ├── context/                 # 会话上下文模型
│   ├── core/                    # 主 Agent 与摘要压缩核心逻辑
│   ├── harness/                 # API/CLI/服务/工具/队列/存储
│   ├── observability/           # 指标与观测
│   └── schemas/                 # 协议/数据模型
└── doc/                         # 设计与实现文档
```

## 环境要求

- Python 3.11+（推荐）
- pip
- Node.js 20.19+（推荐使用 `.nvmrc` 固定的 `v24.15.0`）
- pnpm 9+
- Linux / macOS / WSL（Windows 原生未完整验证）

## 快速开始

### 1) 安装依赖

```bash
python -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

### 2) 配置环境变量

```bash
cp .env.example .env
```

常用配置项与默认值可参考 `app/config/settings.py` 中的 `Settings`。

前后端连接建议统一通过 `.env` 配置：

- `API_HOST` / `API_PORT`：后端 API 监听地址（`run_agent_api.py` 使用）
- `VITE_API_BASE_URL`：前端请求后端地址（`ChatWorkbench` 使用）

### 3) 启动服务（推荐 API 模式）

```bash
python run_agent_api.py
```

默认监听 `127.0.0.1:8000`。

## 运行方式

- **API 服务**：`python run_agent_api.py`
- **CLI 客户端**：`python run_agent.py`
- **Register Center**：`python run_register_center.py`
- **一键开发栈（推荐本地联调）**：`python run_dev_stack.py`（默认同时启动 API + Register Center + Frontend）
- **前端入口（自动检查后端）**：`python run_frontend_with_backend.py`（后端未启动时先拉起 API，再启动前端）
- **Web 前端（开发中）**：
  ```bash
  cd frontend
  pnpm install
  pnpm dev
  ```

## 前端开发（React + Vite）

- 前端代码位于 `frontend/`，当前已完成基础工程初始化与 `ChatWorkbench` 页面骨架。
- 运行后默认地址为 `http://localhost:5173/`。
- 前后端通信目标为：
  - `POST /v1/messages`
  - `GET /v1/streams/{request_id}`（SSE 流式事件）
- 前后端契约建议以后端 OpenAPI 为单一来源：
  - 导出：`python export_openapi_schema.py`
  - 输出：`frontend/openapi.json`
  - 生成 TS 类型：`cd frontend && pnpm gen:types`
  - 生成文件：`frontend/src/api/types.ts`
- 详细结构说明见 `frontend/README.md` 与 `frontend/src/README.md`。

## API 说明（简版）

- `POST /v1/messages`：提交消息
- `GET /v1/streams/{request_id}`：订阅 SSE 事件流
- 其他接口：待补充

详细接口说明请参考 `app/harness/api/README.md` 与实现代码。

## 开发说明

- 代码主入口在 `app/` 下，按目录维护 `README.md` 与 `REFERENCE.md`。
- `register-center/` 目录名含连字符，`run_register_center.py` 当前通过 `importlib` 按文件路径加载 `rc_app.py`；后续如统一为 Python 包命名，可迁移为 `register_center/`。
- 前端构建产物位于 `frontend/dist/`，本地依赖位于 `frontend/node_modules/`，默认不作为源码提交对象。
- 运行测试（如果本地已安装测试依赖）：

```bash
pytest -q
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
