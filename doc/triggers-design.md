# 触发器（条件唤起 Agent）设计方案

本文描述在 **DAgents 现有进程内队列 + `AgentService.submit_message`** 之上，增加 **触发器（Trigger）** 能力的 **目标边界、模块划分与分阶段落地路径**。实现以本文件为设计基线；与代码冲突时以 **Git / CHANGELOG** 为准。

**相关索引**：[agent-input-output.md](./agent-input-output.md)（入队与 SSE）、[architecture-and-flows.md](./architecture-and-flows.md)（分层）、[roadmap.md](./roadmap.md) §3.4「触发器」。

---

## 1. 问题陈述与目标

### 1.1 现状

- 客户端通过 **HTTP** 创建会话并 **`POST` 提交消息**，载荷经 **`MessageIn`** 校验后调用 **`AgentService.submit_message`**（见 **`app/harness/api/app.py`**）。
- 每条会话对应一条进程内 **`MessageQueue`**，**串行消费**；**`MessageEnvelope`** 含 **`request_type` / `content` / `client_id` / `source`** 等（见 **`app/harness/queue/message_queue.py`**）。
- **SSE** 按 **`session_id` + `client_id`** 分桶；无订阅方时事件仍可产生，但无人接收（见 **`doc/agent-input-output.md`**）。

### 1.2 目标

在 **不替代** 现有 HTTP 主路径的前提下，增加 **声明式触发器**：当 **条件满足** 时，自动向指定 **`session_id`** 投递一条（或多条策略化）入队请求，从而 **唤起** Agent 执行与「用户发消息」等价的处理路径。

### 1.3 非目标（首版明确不做或降级）

- **不**在首版引入独立分布式调度器（如 Celery、K8s CronJob 作为必选依赖）；进程内 **`asyncio`** 定时与可选 **HTTP Webhook** 即可覆盖 MVP。
- **不**在首版承诺跨进程 Exactly-once；以 **at-least-once + 幂等键** 为语义基线。
- **不**在首版把触发器做成任意代码插件（Python `exec`）；条件与载荷以 **配置 + 小集合内置算子** 为主，后续再评估 **受限脚本** 或 **Webhook 签名校验 + JSON 模板**。

---

## 2. 设计原则

1. **复用编排与运行时**：触发器只做 **「谁在何时、以什么载荷调用 `submit_message` / `submit_resume`」**；不 fork 第二套 `run_turn` 循环。
2. **与 HTTP 入队对齐**：触发路径构造的 **`MessageEnvelope`** 字段语义与 HTTP 一致；**`source`** 固定前缀 **`trigger`**（或 `trigger:{id}`），便于日志与指标区分。
3. **会话边界清晰**：默认 **只向已存在会话投递**；若配置 **「自动建会话」**，必须单独开关并文档化副作用（空上下文、工具可见性、SQLite 落盘）。
4. **安全默认关闭**：未显式启用时 **零触发**；任何外部入口必须 **鉴权或签名校验 + 限流**。
5. **可观测**：触发成功/跳过/失败、去抖丢弃次数等进入 **logging**；可选复用 **`/metrics`** 计数器（与 **`doc/prometheus-metrics.md`** 扩展方式一致）。

---

## 3. 概念模型

### 3.1 核心类型（逻辑名，实现时可调整模块路径）

| 概念 | 职责 |
|------|------|
| **`TriggerDefinition`** | 静态配置：唯一 **`id`**、**启用**、**绑定 `session_id`**、**动作**（投递 `message` 的模板内容 / 或 `resume` 载荷引用）、**优先级**（默认 **`other`**，避免默认打断用户 **`human`**）、**幂等键模板**。 |
| **`TriggerCondition`** | 条件求值：如 **cron 表达式**、**interval 秒**、**HTTP Webhook 路径匹配**；求值为 bool。 |
| **`TriggerBinding`** | **`TriggerDefinition` + `TriggerCondition`** 的可运行实例；在 **`AgentService.start`** 之后注册，在 **`stop`** 时取消任务。 |
| **`TriggerDispatcher`** | 订阅各 **Source 适配器** 的 **「候选事件」**，经 **条件与策略** 后调用 **`AgentService.submit_message` / `submit_resume`**；集中做 **去抖、幂等、错误隔离**（单条触发失败不拖死全局循环）。 |

### 3.2 `client_id` 约定

- HTTP 路径中 **`client_id`** 参与 SSE 分桶；触发器无浏览器客户端时仍需 **非空稳定** 字符串，建议：
  - 全局默认 **`AGENT_TRIGGER_DEFAULT_CLIENT_ID`**（如 **`trigger`**），或
  - 每条触发器配置 **`client_id`**（如 **`trigger:nightly-report`**）。
- 若希望 **人机分离展示**：同一 `session_id` 下人用 `default`，触发器用独立 `client_id`，SSE 订阅方可只订阅其一。

---

## 4. 触发源（分阶段）

### 4.1 Phase A — 进程内调度（MVP）

| 源 | 说明 | 实现要点 |
|----|------|----------|
| **固定间隔** | 每 `N` 秒求值一次条件（条件可为恒真） | `asyncio.create_task` + `asyncio.sleep` 循环；与 **`run_forever`** 同生命周期。 |
| **Cron** | 与 crontab 五段或六段（含秒）对齐的表达式 | 依赖轻量库（如 **`croniter`**) 或自研子集；**时区**明确为 **UTC** 或可配置 **`AGENT_TRIGGER_TZ`**。 |

**配置载体（建议）**：单文件 **`AGENT_TRIGGERS_CONFIG_PATH`** 指向 **JSON 或 YAML**；未设置或文件不存在则 **不加载任何触发器**。便于 GitOps 与单测夹具。

### 4.2 Phase B — HTTP Webhook 触发

| 项 | 建议 |
|----|------|
| **路径** | 例如 **`POST /internal/triggers/{trigger_id}`** 或统一 **`POST /internal/triggers/ingest`** + body 内 `trigger_id`。 |
| **鉴权** | **HMAC-SHA256** 共享密钥签 **`timestamp + raw_body`**，或 **mTLS** / **Bearer**（与现有部署方式一致即可）。 |
| **载荷** | JSON：可选 **`payload`** 经 **受限模板**（如仅允许 `{{ body.field }}` 若干路径）填入 **`MessageEnvelope.content`**；禁止任意 Jinja2 全局函数。 |
| **限流** | 每 `trigger_id` + IP（若适用）令牌桶，防刷。 |

### 4.3 Phase C — 与 Register Center / A2A 的衔接（可选）

- **不**把 **`/v1/broadcast`** 直接等价于触发器；可在 **Dispatcher** 前增加 **「信封过滤 → 匹配某 `TriggerDefinition`」** 的适配层，避免与 **`agent_peer` 工具** 语义重复。
- 若做：需单独 **设计文档** 定义 **与 `discovery_group`、审批、`resume` 的交互**，本方案不强制首版交付。

---

## 5. 执行语义与队列交互

### 5.1 映射表

| 触发动作 | API | 优先级建议 |
|----------|-----|------------|
| 模拟用户发一句 | `submit_message(..., request_type="message", content=..., priority="other")` | **`other`**：排在 `human`/`resume`/`tool_result` 之后，避免抢用户；若业务要求「紧急后台任务」可配置为 **`human`** 并文档警告。 |
| 驱动审批继续 | `submit_resume(...)` | **`resume`** |

### 5.2 与 `MessageQueue` 的关系

- **不**为触发器单独建队列；**同一 `session_id`** 仍 **单消费者串行**，保证与 [agent-turn-loop.md](./agent-turn-loop.md) 一致。
- **背压**：队列满时 **`submit_message` 抛错**；Dispatcher 应 **捕获、记日志、指标 +1**，可选 **有限次重试 + 退避**，避免热循环打满日志。

### 5.3 会话不存在时

- **默认**：投递前 **`session_id in _session_contexts`**（或等价查询）不满足则 **跳过并 metrics**；不静默创建会话。
- **可选 `auto_create_session: true`**：调用与 HTTP 对齐的 **`create_session`** 再投递；需在 **`TriggerDefinition`** 上显式开启。

---

## 6. 幂等、去抖与并发

### 6.1 幂等键

- 对 **定时类**：自然键可为 **`trigger_id + scheduled_fire_time`**（cron 对齐后的理论触发时刻），在进程内 **`TTL` 缓存**（如 `cachetools.TTLCache` 或自研 dict + 定期清扫）记录「已投递」，**窗口 ≥ 2 个调度周期** 防止时钟漂移重复。
- 对 **Webhook**：优先使用调用方 **`Idempotency-Key`** header；否则 **`HMAC` 内 `event_id`**。

### 6.2 去抖

- 可选字段 **`debounce_seconds`**：同一键在窗口内多次满足条件只投递 **最后一次** 或 **第一次**（二选一文档写死，建议 **leading**：第一次立即投，窗口内忽略）。

### 6.3 并发

- **单 Dispatcher 协程** 串行处理候选事件足够 MVP；高负载时再拆 **per-session 串行、全局并行** 的 worker 池，且保持 **同 session 有序**。

---

## 7. 配置 JSON Schema（示意）

以下为 **逻辑结构**；字段名实现时可微调，但建议在 **CHANGELOG** 中记录破坏性变更。

```yaml
version: 1
triggers:
  - id: nightly_digest
    enabled: true
    session_id: "cron-demo"
    schedule:
      cron: "0 8 * * *"   # 每天 08:00（时区见全局配置）
    action:
      type: message
      content: "请根据昨夜日志生成摘要。"
      priority: other
    client_id: "trigger:nightly_digest"
    idempotency:
      mode: cron_window   # 或 webhook_header
```

```yaml
  - id: deploy_hook
    enabled: false
    session_id: "ops"
    source: webhook       # Phase B
    webhook:
      path_segment: "deploy"
    action:
      type: message
      content_template: "生产发布完成：{{ body.version }}，请健康检查。"
```

---

## 8. 代码落点（建议）

| 层次 | 建议路径 / 钩子 |
|------|-----------------|
| 配置模型 | 新建 **`app/harness/triggers/`**（或 **`app/triggers/`**）：`models.py`（Pydantic）、`loaders.py`（读文件 + 校验）。 |
| 调度与分发 | `dispatcher.py`：`TriggerDispatcher.start(service: AgentService)` / `stop()`。 |
| HTTP（Phase B） | **`app/harness/api/app.py`**：`lifespan` 内 `dispatcher` 与 `app` 共享 **`AgentService`** 引用；路由挂在 **`/internal/...`** 并中间件鉴权。 |
| 生命周期 | **`create_app` lifespan**：在 **`await service.start()`** 之后 **`dispatcher.start()`**，关闭顺序相反。 |
| 设置项 | **`app/config/settings.py`**：`agent_triggers_config_path`、`agent_trigger_default_client_id`、`agent_trigger_time_zone` 等。 |

**单测**：`dispatcher` 使用 **注入的假 `AgentService`** 或 mock **`submit_message`**，固定 **冻结时间** 测 cron；Webhook 测签名校验与 401。

---

## 9. 里程碑建议

| 里程碑 | 交付物 |
|--------|--------|
| **M1** | 配置加载 + YAML/JSON 校验失败即启动失败或跳过并 ERROR（策略二选一文档化）；**interval** 触发 + **`submit_message`**；指标/日志。 |
| **M2** | **Cron** + 幂等窗口；**`client_id` / `source`** 约定落地文档与 **`api-reference.md`** 交叉引用。 |
| **M3** | **Webhook** + HMAC + 限流；**`content_template`** 受限字段。 |
| **M4**（可选） | Register Center 适配、**`auto_create_session`**、Prometheus 计数器。 |

---

## 10. 开放问题（实现前需拍板）

1. **定时默认时区**：UTC 还是服务器本地时区？
2. **触发消息默认优先级**：是否允许配置为 **`human`**（会插队到 `resume` 前但仍不自动 `cancel_current_turn`**）？
3. **配置热更**：首版仅 **启动时加载** 是否可接受？若热更，是否 **`SIGHUP` / 管理 API** 刷新？
4. **多实例部署**：多进程各自跑 cron 会 **重复触发**；是否要求 **MVP 仅单副本**，或多副本时 **必须关内置 cron** 改由外部调度调 Webhook？

---

## 11. 文档与路线图

- 实现过程中：**`doc/api-reference.md`** 增加 Webhook 小节；**`doc/agent-input-output.md`** 增加「触发器 `client_id`」脚注。
- **`doc/roadmap.md`** §3.4 已列方向；首版合并后可在 §2 增加「已实现：进程内触发器」一行。

**最后更新**：初稿 — 与当前仓库 **`AgentService` / `MessageQueue` / HTTP 网关** 对齐；随实现迭代修订 §8～§10 的拍板结论。
