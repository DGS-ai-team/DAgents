# 上下文管理、压缩、记忆与提示词

本文说明 **会话上下文** 的双层模型、**SQLite 会话记忆**、**summary 压缩**、**系统提示词组装**（含 **`soul.md` / `user.md` / `custom.md`** 侧车），以及 **`OpenAIConversationContext`** 上各字段在运行时的典型变化。编排入口为 **`MainAgentTurnOrchestrator.handle_message`**（**`app/core/main_agent/agent.py`**）；压缩算法为 **`SummaryContextCompressionRuntime`**（**`app/core/summary_agent/agent.py`**）；提示词为 **`get_system_prompt`**（**`app/core/main_agent/prompt.py`**）。

---

## 1. 双层上下文：持久化态 vs 推理态

| 类型 | 位置 | 用途 |
|------|------|------|
| **`ConversationContext`** | **`app/context/models.py`** | **可落库**：`openai_messages`、`pending_tool_calls`（dict 列表）、`run_turn_phase`、`messages_total_tokens`、`tool_loop_count`、`loaded_skills`，以及派生 **`history`**（人类可读摘要）。 |
| **`OpenAIConversationContext`** | 同上 | **进程内推理权威**：`messages`（OpenAI 消息 dict 列表）、`pending_tool_calls`（**`PendingToolCall`** 对象列表）、`run_turn_phase`、`messages_total_tokens`、`tool_loop_count`、`loaded_skills`、`assistant_stream_buffer`、**`sse_client_id`** 等。 |

**转换**：

- **`OpenAIConversationContext.from_conversation_context`**：服务从 SQLite 加载后还原；**`session_id`** 由服务层在装入缓存时写入。
- **`OpenAIConversationContext.to_conversation_context`**：每条消息处理结束在 **`AgentService._handle_message`** 的 **`finally`** 中 **`_persist_context`** 时调用；**`run_turn_phase`** 经 **`normalized_run_turn_phase_for_persist()`** 规整（流式中阶段不落库为 **`MODEL_STREAMING`** 等）。

**不入库（仅推理态 / 编排层内存）**：

- **`OpenAIConversationContext.sse_client_id`**：见 [agent-input-output.md](./agent-input-output.md)。
- **`assistant_stream_buffer`**：流式增量缓冲；取消时用 **`flush_cancelled_turn`** 收敛进 **`messages`** 或清空。
- **压缩编排的中间态**：见 §4（**`MainAgentTurnOrchestrator`** 内 **`_session_summary_tasks`**、**`_session_pending_compression_results`**）。

**说明**：**`SummaryCompressionPhase`** 枚举在 **`models.py`** 中已定义，**当前压缩路径未挂载到 `ctx` 字段**；阶段语义由编排器任务与 **`pending` 字典** 体现。

---

## 2. `OpenAIConversationContext` 字段与变更来源

| 字段 | 典型写入方 | 说明 |
|------|------------|------|
| **`session_id`** | **`AgentService`** 装入缓存时 | 与队列 **`session_id`** 对齐。 |
| **`sse_client_id`** | **`AgentService._handle_message`** | 入站 **`env.client_id`** 非空时刷新；不入库。 |
| **`messages`** | **`runtime_openai`**、**`raw_message_journal`**、编排器（tool/user/assistant 插入）、**`_try_apply_ready_compression_result`**（区间替换） | OpenAI 请求权威列表；压缩成功时 **用单条 `role=user` 摘要替换 `[start,end]` 切片**。 |
| **`pending_tool_calls`** | **`runtime_openai`**（模型产出 tool_calls）、编排器（审批拆分、清空、执行后回填） | 与 **`tool_result` / `resume` / `async_tool_result`** 分支强相关。 |
| **`run_turn_phase`** | **`OpenAIImplicitReActRuntime.run_turn`** | **`IDLE` → `BRANCH_RESOLVING` / `MODEL_STREAMING` / `AWAITING_TOOL_EXECUTION`** 等；见 §3。 |
| **`messages_total_tokens`** | **`runtime_openai`**（流式 usage 更新） | 供压缩 **`should_compress`** 与观测；与 **`ctx.messages`** 长度/内容估算相关。 |
| **`tool_loop_count`** | **`runtime_openai`** | 每进入一轮模型流式前递增；超过 **`llm_max_tool_loops`** 时强制回到 **`IDLE`** 并结束 tool 循环。 |
| **`loaded_skills`** | skills 加载路径（若启用） | 可随 **`to_conversation_context`** 持久化。 |
| **`assistant_stream_buffer`** | **`runtime_openai`** 流式 delta | 正常结束会并入最终 assistant；**`flush_cancelled_turn`** 在取消时可能 **`append` 后清空**。 |

---

## 3. `RunTurnPhase` 与主 runtime 简要状态机

以下描述 **`OpenAIImplicitReActRuntime.run_turn`**（**`app/core/main_agent/runtime_openai.py`**）对 **`ctx.run_turn_phase`** 的主要赋值（省略部分早退分支）：

| 阶段 | 含义（概要） |
|------|----------------|
| **`IDLE`** | 初始或本轮结束。 |
| **`BRANCH_RESOLVING`** | 入口短暂分支解析。 |
| **`MODEL_STREAMING`** | 正在请求模型并消费流式 chunk。 |
| **`AWAITING_TOOL_EXECUTION`** | 模型已输出 **`tool_calls`**，等待上层执行/审批（runtime 自身不执行工具）。 |

**持久化**：**`MODEL_STREAMING` / `BRANCH_RESOLVING`** 在 **`normalized_run_turn_phase_for_persist`** 中映射为 **`IDLE`**，避免 sqlite 中悬挂「流式中」阶段。

---

## 4. 上下文压缩（summary）

### 4.1 配置阈值

| 配置项（`Settings`） | 含义 |
|----------------------|------|
| **`summary_compression_silent_trigger_tokens`** | 静默压缩：超过则 **后台 `asyncio.Task`** 跑压缩，**不阻塞** 当前 **`handle_message`** 主流程（在入口先 **`await` 已有静默任务** 的收尾逻辑见代码）。**`<=0`** 关闭该档位。 |
| **`summary_compression_blocking_trigger_tokens`** | 阻塞压缩：超过则 **先 `await` 静默任务（若有）**，再 **同步 `await` 完整压缩流程**，再继续本条消息分支。**`<=0`** 关闭该档位。 |

**判定**：**`SummaryContextCompressionRuntime.should_compress`** 使用 **`ctx.messages_total_tokens`**（外部传入）；**阻塞优先于静默**（同时满足时按 **`blocking`**）。

### 4.2 是否压缩与区间选择

1. **`build_compression_plan(ctx.messages)`**：在 **最后一条 `assistant` 之前** 选取候选前缀；剔除 **`system`**；从尾部回退直至 **assistant(tool_calls) 与 tool 成对闭合**（**`_assistant_tool_pairs_complete`**）。
2. 若选不出合法区间 → **`should_compress`** 在计划阶段失败，**不压缩**。
3. **`build_follow_content(messages, end=end)`**：构造「后续文本」供摘要模型衔接。

### 4.3 静默 vs 阻塞

- **静默（`silent`）**：若当前无在跑静默任务，**`create_task(_run_compression_flow)`**；结果写入 **`_session_pending_compression_results[session_id]`**，在后续 **`maybe_handle_summary_compression` / `_try_apply_ready_compression_result`** 入口 **替换 `ctx.messages`**。
- **阻塞（`blocking`）**：**`await _run_compression_flow`**；成功则同样写入 **`pending`** 并随即在 **`_try_apply_ready_compression_result`** 中尝试应用。
- **`_run_compression_flow`**：调用 **`summary_runtime.run_turn`**（独立 **chat.completions**，无 tools）生成摘要文本；失败记日志并返回 **`False`**。

### 4.4 应用到 `ctx.messages`

**`_try_apply_ready_compression_result`**：

- 取出 **`pending`** 中的 **`start` / `end` / `content`**（摘要正文）；
- 校验区间与 **`ctx.messages` 当前长度**；
- **替换为单条** **`{"role":"user","content": content}`**，即 **`ctx.messages = [:start] + [replacement] + [end+1:]`**。

**与编排器内存**：**`_session_summary_tasks`**、**`_session_pending_compression_results`** 挂在 **`MainAgentTurnOrchestrator`** 上，**按 `session_id` 分桶**；**服务 `stop()`** 时 **`cancel_all_summary_tasks`** 清理。

---

## 5. 消息处理入口顺序（与压缩的关系）

每条出队 **`MessageEnvelope`** 进入编排器时：

1. **`await maybe_handle_summary_compression(session_id, ctx)`**  
   - 内含 **`_try_apply_ready_compression_result`**（应用上一轮静默/阻塞产出的 **`pending`**）。  
   - 再按阈值决定是否启动 **静默 task** 或执行 **阻塞压缩**。
2. 再按 **`env.request_type`** 分发：**`resume` / `async_tool_result` / `tool_result` / 默认 human**。

因此：**压缩决策与应用优先于** 本轮业务分支（含用户新消息写入路径）。

---

## 6. 与持久化、观测的衔接

- **落盘**：**`AgentService._persist_context`** → **`ctx.to_conversation_context()`** → **`SqliteMessageStore`**（当 **`AGENT_SESSION_STORE_PATH`** 已配置时）；详见 §7。  
- **Prometheus**：**`refresh_session_context_metrics`** 在 **`_handle_message`** 的 **`finally`** 中调用，与队列积压等见 [prometheus-metrics.md](./prometheus-metrics.md)。

---

## 7. 会话记忆与持久化（`SqliteMessageStore`）

当 **`AGENT_SESSION_STORE_PATH`** 解析结果 **非空**（默认未在环境中设置该键时为 **`.runtime/memory/session.sqlite3`**；若在环境中 **显式设为空串** 则关闭 SQLite）时，**`AgentService`** 使用 **`app/harness/memory/store.py`** 中的 **`SqliteMessageStore`**，在 **`_handle_message`** 的 **`finally`** 中把 **`OpenAIConversationContext.to_conversation_context()`** 整包写入 SQLite。

| 概念 | 说明 |
|------|------|
| **存储粒度** | 按 **`session_id`** 一行；**`content`** BLOB 为 UTF-8 JSON，含 **`history`**、**`openai_messages`**、**`pending_tool_calls`**、**`run_turn_phase`**（已规整）、**`messages_total_tokens`**、**`tool_loop_count`**、**`loaded_skills`** 等。 |
| **`history`** | 由 **`openai_messages`** 派生的 **`MessageRecord`** 列表，便于人类可读摘要；**`append_message`** 路径可仅追加 **`history`**（读-改-写单行）。 |
| **与推理态差异** | **`sse_client_id`**、**`assistant_stream_buffer`**、编排器内压缩 task 等 **不写入** 该 JSON；重启进程后 **`sse_client_id`** 需由下一次带 **`client_id`** 的入站消息重新建立（见 [agent-input-output.md](./agent-input-output.md)）。 |
| **默认路径** | 未显式配置时常见默认指向 **`.runtime/memory/`** 下 sqlite 文件（以 **`Settings`** 与 **`resolve_runtime_root()`** 解析为准）。 |

**`read_memory_file_cached`**（**`prompt.py`**）：当前实现为 **占位**，恒返回空串；未来若接入「外部记忆文件」再拼入 **`get_system_prompt`**，需在此集中实现缓存与 mtime 策略。

---

## 8. 系统提示词组装与侧车 Markdown

### 8.1 侧车路径与种子

- **运行时目录**：**`<resolve_runtime_root()>/.runtime/prompt_context/`**（与 **`.gitignore`** 下的 **`.runtime/`** 一致，**不提交 git**）。  
- **仓库内种子**：**`packaging/prompt_context/`**（含 **`soul.md` / `user.md` / `custom.md`** 与 **`README.md`**）。  
- **首次读取**：**`_read_prompt_context_markdown`** 会调用 **`_ensure_prompt_context_seeded`**：若运行时目录中 **缺少** 某文件名且种子中存在，则 **`shutil.copy2`** 拷贝一份；**已存在的文件不会被覆盖**，便于部署侧长期定制。

### 8.2 `custom.md` 与其它侧车

| 文件 | 拼入条件 | 在整条 system 中的大致位置 |
|------|----------|---------------------------|
| **`soul.md`** | `strip()` 后非空 | 静态规则之后 → **「以下是你的设定」** |
| **`user.md`** | 非空 | → **「以下是用户信息与偏好」** |
| **`custom.md`** | 非空 | **`.runtime` 目录约定** 与 **JSONL 记录说明**（若启用）之后 → **「以下是用户侧追加的临时/专项指令」** |
| **（无独立文件）** | — | **`session_id`** 段落在 **最末**（**`_session_id_from_context`**） |

侧车 **不解析 YAML front matter**；全文 **`strip()`** 后作为 Markdown 正文拼入。修改文件后依赖 **mtime** 使进程内 **`_prompt_context_file_cache`** 失效并重读。

### 8.3 `get_system_prompt` 完整拼接顺序（与代码一致）

1. **`get_static_system_prompt()`**（代码内嵌的最高优先级规则与行为准则等）。  
2. **`soul.md`**（可选）。  
3. **`user.md`**（可选）。  
4. **Skills**（若 **`agent_skills_enabled`**）：先 **可用技能目录**（元数据），再按 **`context.loaded_skills`** 与上限注入 **已加载技能正文**。  
5. 若 **`agent_skills_allow_create`**：追加 **自主创建 skills** 说明（含 skills 根路径）。  
6. **`get_host_snapshot()`** → **「以下是当前运行环境」**。  
7. **「`.runtime` 工作目录约定」**（含 **`memory/`**、**`prompt_context/`**、**`agent/`**、**`skills/`**、**`data/`**、**`scripts/`**、**`scripts_menu.md`** 等说明文本）。  
8. 若启用 **`agent_raw_message_history_enabled`**：追加 **JSONL 原始消息记录** 说明（路径来自 **`agent_raw_message_history_dir`**，默认 **`.runtime/history/`**）。  
9. **`custom.md`**（可选）。  
10. **「会话环境信息」**（含 **`session_id`**）。

实际 **`messages`** 发往模型时，**`OpenAIImplicitReActRuntime`** 将 **system** 与 **`ctx.messages`** 组合（见 **`runtime_openai.py`**）。

---

## 9. 相关文档

| 文档 | 内容 |
|------|------|
| [architecture-and-flows.md](./architecture-and-flows.md) | 主流程、工具与 SSE |
| [agent-input-output.md](./agent-input-output.md) | 入队与 SSE、`sse_client_id` |
| [api-reference.md](./api-reference.md) | HTTP 契约 |
| **`packaging/prompt_context/README.md`** | 侧车种子说明（仓库内路径） |

---

**0.x 提示**：阻塞压缩失败后的用户可见错误策略在源码中仍有 **TODO** 占位；以 **`app/core/main_agent/agent.py`** 与 **CHANGELOG** 为准。
