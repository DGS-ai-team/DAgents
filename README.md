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

### 3) 启动服务（推荐 API 模式）

```bash
python run_agent_api.py
```

默认监听 `0.0.0.0:8000`。

## 运行方式

- **API 服务**：`python run_agent_api.py`
- **CLI 客户端**：`python run_agent.py`
- **Register Center**：`python run_register_center.py`

## API 说明（简版）

- `POST /v1/messages`：提交消息
- `GET /v1/streams/{request_id}`：订阅 SSE 事件流
- 其他接口：待补充

详细接口说明请参考 `app/harness/api/README.md` 与实现代码。

## 开发说明

- 代码主入口在 `app/` 下，按目录维护 `README.md` 与 `REFERENCE.md`。
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
