# Session 持久化与恢复

本文记录 architecture-v2 关于 **会话上下文落库与恢复** 的架构决策与分阶段范围。**详细表结构、存储选型与多 Backend 实现细则在 Phase 2（共享状态多 Backend）展开**；Phase 1 继续沿用 v1 的 SQLite 路径。

## 1. 核心决策（必须保留）

**v2 保留 session 恢复能力**，不把「Redis 仅存上下文、过期即丢弃」作为默认或多 Backend 的唯一方案。

| 决策 | 说明 |
|------|------|
| **可恢复** | Backend 进程重启、滚动升级后，在策略允许的时间窗内，用户可用同一 `session_id` 继续对话（上下文、`pending_tool_calls`、已加载 skills 等推理态可还原）。 |
| **非 ephemeral 默认** | 共享状态层（Redis）承担 **路由元数据、presence、TTL 索引**；**会话正文**须写入 **durable session store**（Phase 1：SQLite；Phase 2+：见 §4）。 |
| **与 v1 对齐** | 现网 `SqliteMessageStore` + `AGENT_SESSION_STORE_ENABLED` 语义在 v2 中延续为 **`persistence_mode=durable`** 的基准行为。 |
| **显式放弃才丢** | 仅当用户/管理员 **删除 session**、**clear-context**、或 **超过配置的保留 TTL** 时，才视为不可恢复；不因 Redis flush 单独导致正文丢失。 |

**非目标（当前阶段不写实现）**：

- 不把 MessageQueue 中的 `MessageEnvelope` 逐条落库（队列仍为 owner 内存结构）。
- Phase 1 不要求跨 Backend **热接管** 进行中的 turn（见 §5）。

## 2. 持久化分层

```text
Layer C（owner 内存，不恢复）
  Session Queue、流式 buffer、sse_connection_id / connection 绑定的运行时字段

Layer A（共享状态，Redis 等，可 TTL）
  SessionRecord 元数据、owning_backend_instance_id、connection 映射、
  body/proxy presence、ExecutionRecord、SSE routing

Layer B（durable session store，须可恢复）
  ConversationContext 快照：
    openai_messages、pending_tool_calls、history、loaded_skills、
    run_turn_phase（规整后）、messages_total_tokens、tool_loop_count、
    first_request_message 等
```

**不变量**：

1. 同一 `session_id` 在任意时刻 **只有一个 writer**（owner Backend 的 session consumer）。
2. Layer B 的写入发生在 **Brain turn 处理完成后**（与 v1 `_persist_context` / `finally` 时机一致），不是每条 HTTP 入站单独一行。
3. Layer A 的 session TTL 用于 **资源回收与 discover**；**Layer B 的保留策略独立配置**，二者不得混为一谈。

## 3. 与 v1 实现的对应

| v1 | v2 定位 |
|----|---------|
| `SqliteMessageStore`（`.runtime/memory/session.sqlite3`） | Phase 1 **durable store** 默认实现 |
| `ConversationContext` JSON BLOB 整包 upsert | Phase 2+ 可保留「快照」模型，或演进为快照 + 追加日志 |
| `agent_session_store_enabled=false` | 等价 **`persistence_mode=memory`**，仅开发/单测；**不是 v2 多 Backend 生产默认** |
| `raw_message_journal`（JSONL，可选） | 与 Layer B 并列的审计侧车；多 Backend 时路径与归属 **Phase 2+ 补充** |

详见 [archive/python-agent-runtime/context-compression-and-state.md](../archive/python-agent-runtime/context-compression-and-state.md) §7（Python 历史）。

## 4. 分阶段范围（后续扩展）

### Phase 1：单 Backend

- **存储**：继续 SQLite（或注入式 `SessionContentStore` 接口，默认 SQLite 实现）。
- **恢复**：进程重启后，按 `session_id` 从 store 加载 `ConversationContext` 到 owner 缓存。
- **实现任务**：抽象 `SessionContentStore`，与 `AgentService._persist_context` / `_load_context_from_store_sync` 解耦（代码层可在 Phase 1 末或 Phase 2 初做）。

### Phase 2：共享状态多 Backend（**本文档详细设计在此阶段补充**）

计划补充内容包括（TODO，实现前写入本节）：

- [ ] Layer A（Redis）与 Layer B（durable store）字段划分与 key 设计
- [ ] durable store 选型：**PostgreSQL `jsonb` 为推荐目标**；过渡方案 owner 本地 SQLite + sticky owner
- [ ] `content_version` 乐观锁与 owner 单写规则
- [ ] 请求落到非 owner 时：仅转发 envelope，**不在非 owner 上写 Layer B**
- [ ] session 列表 / `show session` 跨实例查询
- [ ] 与 HITL（`pending_tool_calls`、审批中 execution）恢复相关的 TTL 下限
- [ ] trigger / A2A 回灌对 **已持久化 session** 的依赖约定

### Phase 3：session takeover 与集中历史

- owner Backend 失联后，由健康实例 **抢锁接管** session consumer，从 Layer B 加载上下文继续服务。
- 集中历史查询、备份、合规保留策略（可能与 Layer B 分库或冷归档）。

### Phase 4：大规模 Agent 网络

- 按 `agent_id` / `organization_id` 分区 session store；与 RC 分区策略对齐（**待多 Agent 扩展时与本节合并修订**）。

## 5. 恢复边界（产品语义）

以下情况 **不要求** 恢复「进行中的一轮 turn」，但 **应尽量** 恢复已落盘的对话态：

| 场景 | 期望 |
|------|------|
| Backend 重启 | 已 persist 的 context 可加载；进行中的 LLM 流、未落盘队列消息可丢 |
| Redis 元数据丢失但 Layer B  intact | 可凭 `session_id` 从 durable store 重建 SessionRecord（Phase 2+ 设计） |
| Layer B 丢失 | 会话不可恢复；对用户返回明确错误，提示新建 session |
| `clear-context` / `DELETE session` | 显式不可恢复 |
| 临时子 Agent session | 默认 **不** 长期持久化（见 [temporary-child-agents.md](./temporary-child-agents.md)） |

## 6. 曾考虑的替代方案（不采用为默认）

**仅 Redis 存全文、TTL 过期即丢弃**：

- 优点：多 Backend 实现简单。
- 不采用原因：与 **session 恢复** 产品能力冲突；Redis 故障或 flush 会导致集体失会话；长审批、trigger 回灌依赖不稳定。

若未来需要 **短生命周期 session**，可通过 `SessionRecord.persistence_mode=ephemeral` 显式声明（Phase 2+ 可选），与默认 `durable` 并存；**全局默认仍为 durable**。

## 7. 相关文档

| 文档 | 内容 |
|------|------|
| [identity-and-session.md](./identity-and-session.md) | `SessionRecord`、`persisted` 字段 |
| [message-queue-and-execution-control.md](./message-queue-and-execution-control.md) | Session Queue 与持久化职责边界 |
| [cross-backend-coordination.md](./cross-backend-coordination.md) | owner 路由、Phase 2 共享状态 |
| [migration-from-v1.md](./migration-from-v1.md) | v1 SQLite 迁移 |
| [context-compression-and-state.md](../archive/python-agent-runtime/context-compression-and-state.md) | 当前 `SqliteMessageStore` 字段说明（Python 历史） |
