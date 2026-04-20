# DAgents

多 Agent 运行时相关实验：实现代码在 **`app/`**；**设计文档**在 **`doc/`**。

## 目录结构（概览）

```
DAgents/
├── run_agent.py          # CLI 入口：将根目录加入 sys.path，调用 app.harness.cli.main
├── run_agent_api.py      # FastAPI 入口：统一客户端 HTTP 接入
├── run_agent_service.py  # 独立服务入口：常驻消费消息队列
 ├── run_register_center.py # Register Center 入口：Agent 注册与发现目录服务
├── requirements.txt
├── .env.example          # 环境变量模板（复制为 .env，勿提交密钥）
├── prompt_context/       # 系统提示侧车：soul.md、user.md、custom.md（自定义，拼在最后），见目录内 README
 ├── register-center/      # Register Center 实现（FastAPI + 内存存储）
├── app/                  # 应用包（无 __init__.py，PEP 420 命名空间包）
│   ├── config/           # Settings、load_env（.env）
│   ├── core/             # main_agent：agent、model、prompt
│   └── harness/          # tools、memory、cli、ui
└── doc/                  # 规划与设计（索引见 doc/README.md）
```

- **依赖**：在仓库根执行 `pip install -r requirements.txt`；建议使用虚拟环境（如 `.venv/`，已列入 `.gitignore`）。
- **配置**：复制 `.env.example` 为 `.env`；变量含义与默认值见 **`app/config/settings.py`** 中 **`Settings`**。
- **运行（API，推荐）**：在仓库根执行 **`python run_agent_api.py`**（FastAPI 对外提供 `/v1/messages` 与 SSE `/v1/streams/{request_id}`）。
- **运行（独立服务）**：在仓库根执行 **`python run_agent_service.py`**（常驻进程，直接消费本地队列）。
- **运行（CLI）**：在仓库根执行 **`python run_agent.py`**（默认：**stdin -> FastAPI**；当前仅支持 `AGENT_CLI_MODE=api/http`）。
- **运行（Register Center）**：在仓库根执行 **`python run_register_center.py`**（默认监听 `0.0.0.0:8010`，提供 `/v1/agents` 与 `/health`）。
- **运行（等价）**：`PYTHONPATH=. python -m app.harness.cli.main`（需从仓库根执行，或保证 `PYTHONPATH` 含仓库根）。

更细的模块说明见 **`app/README.md`**、**`app/REFERENCE.md`**。

## 文档入口

- [doc/README.md](doc/README.md)（索引）
- [doc/agent-设计.md](doc/agent-设计.md)（主 Agent 行为与能力，**待补充**）
