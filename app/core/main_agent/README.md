# `app/core/main_agent/`

> **【已弃用】** Python 主 Agent 编排。新实现见 **`node/internal/turn/`**。

主 Agent 运行时：OpenAI 隐式 ReAct 编排、模型客户端与系统提示词。

| 文件 | 说明 |
|------|------|
| **`agent.py`** | **`init_agent()`**（创建 OpenAI runtime）、**`MainAgentTurnOrchestrator`**（消息分支调度与协调器装配） |
| **`runtime_openai.py`** | OpenAI 原生 tool calling 运行时（会话状态、工具调用、审批中断、事件产出） |
| **`summary_compression.py`** | 上下文压缩协调：静默/阻塞触发、启动快照、区间指纹校验与应用结果指标 |
| **`tool_execution.py`** | 工具调用执行计划、审批 payload 构造、自动执行批处理与审批等待批处理 |
| **`tool_resume.py`** | 工具审批恢复：approve/reject/selective 决策归一化、pending 覆盖校验与执行/拒绝回灌 |
| **`model.py`** | **`get_openai_client()`**、**`get_model_config()`** |
| **`prompt.py`** | **`get_system_prompt`**（稳定前缀 + prompt context 侧车 + loaded skills + custom + session 后缀）、**`build_stable_system_prompt`** 等 |

高层行为与编排细节见本目录 **`README.md`**、**`REFERENCE.md`** 及源码；对外 HTTP/SSE 契约见 **`../../../docs/api-reference.md`**。
