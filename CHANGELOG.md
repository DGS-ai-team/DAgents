# 变更记录

本文档遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 的条目风格；版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [0.1.0] - 2026-05-12

首个对外标记版本（**0.x 预览**）：核心 Agent API、可选 SQLite 会话持久化、SSE 流式事件、Prometheus 指标、Register Center 与配套文档/单测基线。以下 **变更 / 文档 / 仓库维护** 与首版同批交付（尚未单独发补丁版号时，仍以 **`v0.1.0`** 与 Git tag 对齐）。

### 包含（概要）

- FastAPI Agent 服务：会话、消息提交、resume、SSE、取消 turn、会话释放等（详见 `app/harness/api/README.md`）。
- 本地联调脚本（`run_dev_stack.py` 等）；仓库根其它 `run_*.py` 入口仍在演进，交互能力不以变更记录为对外承诺。
- OpenAI 兼容运行时：工具调用、审批流、异步工具结果回灌、上下文压缩相关能力。
- `register_center/`：Agent 登记与发现（内存实现）。
- 配置：`app/config/settings.py`、`.env.example`；可观测：`/metrics`（可关）。
- 单元测试：`tests/` 下 `unittest discover`；可选联网冒烟见 `tests/integration/`。
- `doc/`：对外技术文档除 **`api-reference.md`**、**`prometheus-metrics.md`**、**`architecture-and-flows.md`**、**`agent-input-output.md`**、**`context-compression-and-state.md`** 外，另含 **`agent-turn-loop.md`**、**`a2a-and-register-center.md`**、**`built-in-tools.md`**、**`roadmap.md`** 与 **`cases/`** 案例目录索引等；索引见 **`doc/README.md`**；其余以代码与 **`app/**/README.md`** 为准。

### 变更

- **系统提示侧车**：运行时从 **`<运行根>/.runtime/prompt_context/`** 读取 **`soul.md` / `user.md` / `custom.md`**；若缺失则由 **`prompt.py`** 创建 **空 UTF-8 文件**（不覆盖已有文件），**不从其它路径拷贝文案**。**`packaging/runtime/prompt_context/`** 提供 **空文件占位**；**`packaging/runtime/`** 另含 **`scripts/`**、**`data/`** 等目录占位，随发布 zip 并入 **`bundle/.runtime/`**。
- **异步工具**：`AsyncToolResultStore.submit_coroutine` 要求非空 **`client_id`**；`OpenAIConversationContext` 新增进程内 **`sse_client_id`**（由带 **`client_id`** 的入站 `MessageEnvelope` 刷新），异步工具终态回灌的 **`MessageEnvelope.client_id`** 与该通道对齐，保证 SSE 可投递至原客户端。

### 文档

- **`doc/context-compression-and-state.md`**：补充 **SQLite 会话记忆**、侧车 Markdown、**`get_system_prompt`** 拼接顺序；索引见 **`doc/README.md`**。
- 新增 **`doc/agent-turn-loop.md`**：讲解 **队列外层 + `run_turn` 单轮内层**、**`_run_turn_and_maybe_execute_tools`**、**`tool_result` 入队** 与审批 / **`async_tool_result`** 分支。
- 新增 **`doc/a2a-and-register-center.md`**：**A2A（`agent_peer`）** 与 **Register Center**（登记、**`broadcast`/`relay`**、配置与审批 **`resume` 直连** 约束）。
- 新增 **`doc/built-in-tools.md`**：**`get_tools()`** 清单、**`@tool` / `tool()`** 装饰逻辑、**docstring → LLM**、**`parameters` 与 `parse_tool_arguments` → `invoke` 管道**、异步工具与 **`client_id`**、**`host_platform` 未注册** 等。
- 新增 **`doc/roadmap.md`**：**路线图**（已实现能力、待办、已知限制；含 **§3.4** CLI / 子 Agent / A2A 与 Register Center 增强 / 压缩 / 内置记忆书等规划方向）。
- 新增 **`doc/cases/`**：落地案例目录（见 **`doc/cases/README.md`**），用于收录各场景实践与效果供参考。
- **`doc/README.md`**：**`cases`** 入口链接格式修正；**`doc/roadmap.md`**：§3.4 **A2A 优化** 条目补全（广播 SSE、跨 NAT 等表述）。

### 仓库维护

- **`.gitignore`**：增加 **`dist/`**、**`build/`**、**`bundle/`**，避免将 PyInstaller 与本地打包中间产物误提交。
- **`tests/test_agent_service.py`**：懒加载 **`OpenAIImplicitReActRuntime`** 时 patch **`app.core.main_agent.runtime_openai.get_openai_client`**，避免新版 **OpenAI** SDK 在 **`LLM_API_KEY` 为空** 的 CI 环境于 **`AsyncOpenAI(...)`** 构造期抛错，导致 **`_session_consume_loop`** 崩掉、生命周期断言失败。

### 已知限制（0.1.0）

- **HTTP API 单测**未在仓库内全覆盖；CI 以 `requirements.txt` 安装后跑默认 `test_*.py`。
- **`test_agent_service.py`** 在缺少完整依赖（如未安装 `openai`）时相关用例会 **skip**，与 CI 全量安装行为不同。
- 破坏性 API 变更在迈向 **1.0** 前仍可能出现；见 [README.md](README.md) 中「版本与兼容性」。

（Git **tag**：`v0.1.0`；请在对应托管平台上创建 **Releases** 并与该 tag 对齐。）
