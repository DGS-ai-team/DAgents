# Prometheus 观测说明

本文说明 **Prometheus 的基本工作机制**、**DAgents 中的落地方式**，以及 **如何安全地新增指标**。

---

## 1. Prometheus 在做什么

Prometheus 是一套 **主动拉取（pull）** 的时序监控系统：

1. **抓取（scrape）**：Prometheus Server 按配置的间隔访问目标的 HTTP 端点（例如 `http://agent-api:8000/metrics`），读取 **文本格式的指标快照**。
2. **存储**：将每条时间序列（由 **指标名 + label 集合** 唯一确定）按时间戳存入 TSDB。
3. **查询与告警**：通过 PromQL 查询；告警规则对查询结果判断是否触发。

典型部署里，目标进程 **不主动推送到 Prometheus**，而是 **暴露一个只读的 metrics HTTP 路由**；Prometheus 负责定时来读。

### 1.1 暴露格式（ exposition ）

抓取到的正文一般为 **Prometheus text exposition**，包含：

- `# HELP ...` / `# TYPE ...`：说明与类型；
- 若干行 **`metric_name{label="value",...} numeric_value`**。

客户端库负责生成合法格式；本仓库使用 Python 官方常用的 **`prometheus_client`**（见根目录 **`requirements.txt`** 中的 **`prometheus-client`**）。

### 1.2 指标类型（常用）

| 类型 | 语义 | 典型用法 |
|------|------|----------|
| **Counter** | 只增不减的累计量 | 请求总数、累计 token 数 |
| **Gauge** | 可升可降的快照值 | 当前队列长度、当前连接数 |
| **Histogram / Summary** | 分布与分位数 | 延迟分布（本项目暂未使用） |

命名习惯：Counter 常以 **`_total`** 结尾；Gauge 一般不使用该后缀。

当前仓库：**LLM token** 使用 Gauge（上游 **`usage`** 可能已为累计计数，本进程仅 **`set`** 快照）；**会话 messages 条数**使用 Gauge。

### 1.3 Label（标签）与基数（cardinality）

每条时间序列由 **`指标名 + 所有 label 键值对`** 唯一确定。**label 取值种类越多，时间序列条数越多**，会放大存储与查询成本，称为 **高基数问题**。

实践原则：

- **不要把用户原文、请求 ID、无界 session 全文等放进 label**；必要时只放 **规范化后的短标识**，或对正文只做 **长度统计（放在样本数值里）**。
- 对 **`session_id`**、**`model`** 等动态字符串，应用侧先做 **清洗与截断**（见下文 **`sanitize_*`**）。

---

## 2. DAgents 中的实现方式

### 2.1 路由与开关

- FastAPI 应用在 **`app/harness/api/app.py`** 中注册 **`GET /metrics`**。
- 是否挂载该路由由配置 **`METRICS_ENABLED`**（**`Settings.metrics_enabled`**）控制；为假时不注册路由（抓取端需保证配置一致）。

响应体由 **`app/observability/metrics.py`** 中的 **`metrics_text()`** 生成：内部调用 **`prometheus_client.generate_latest()`**，返回 **`bytes`** 与 **`Content-Type`**（一般为 `CONTENT_TYPE_LATEST`）。

### 2.2 指标定义集中位置

所有 Prometheus 指标对象与刷新逻辑集中在 **`app/observability/metrics.py`**，避免散落在业务模块难以维护。

当前内置两类能力：

1. **LLM token 快照（Gauge）**  
   - **`dagents_llm_prompt_tokens{model=...}`**、**`dagents_llm_completion_tokens{model=...}`**  
   - **`dagents_llm_prompt_audio_tokens`**、**`dagents_llm_prompt_cached_tokens`**（来自 **`usage.prompt_tokens_details`**）  
   - **`dagents_llm_prompt_cache_hit_tokens`**、**`dagents_llm_prompt_cache_miss_tokens`**  
   - 在收到流式响应 **`usage`** chunk 时由 **`record_llm_token_usage(..., usage=原始 usage)`** 执行 **`set`**（与 **`include_usage`** 配置配合）。若网关上报的 prompt/completion 已为累计值，此处**不再**用 Counter 二次累加。

2. **会话上下文快照（Gauge）**  
   - **`dagents_session_context_messages_count{session_id=...}`**：**`OpenAIConversationContext.messages`** 条数。  
   - 由 **`refresh_session_context_metrics(session_contexts)`** 根据 **`AgentService._session_contexts`** 更新；对已消失的 **`session_id`** 执行 **`SESSION_CONTEXT_MESSAGES_COUNT.remove`**。

### 2.3 何时刷新上下文指标

**`AgentService`**（**`app/harness/service/agent_service.py`**）在以下时机调用 **`refresh_session_context_metrics`**：

- **`_handle_message` 的 `finally`**（每轮对话处理结束，无论成功/异常/cancel）；  
- **`_resolve_context`** 首次从存储装入缓存后；  
- **`create_session`** 预热上下文后；  
- **`release_session`**（无队列分支）、**`_evict_session_for_capacity`**；  
- **`stop`** 时传入空字典以清空相关 Gauge。

这样 **`/metrics`** 反映的是 **推理上下文中的 messages 条数**（不展开单条 **`content`** 字符量），而不是进程内 **待 receive 的消息队列**。

### 2.4 Label 清洗工具

- **`sanitize_model_label`**：用于 **`model`** label。  
- **`sanitize_prometheus_label_value`**：用于 **`session_id`** 等通用字符串。

新增动态 label 时应 **尽量复用** 上述函数或同类规则，避免破坏 exposition 合法性或引入超高基数。

---

## 3. 如何新增指标（推荐步骤）

### 3.1 选型

- **事件发生后只增不减**（例如「累计调用次数」「累计字节数」）→ **`Counter`**，在业务点 **`inc(...)`** 或 **`labels(...).inc(...)`**。  
- **当前状态快照**（例如「当前活跃 session 数」「某缓存条目数」）→ **`Gauge`**，在业务点 **`set(...)`**；若 label 组合会随时间消失，需像 **`refresh_session_context_metrics`** 一样维护上一轮的 key 集合并对 **`remove`** 做差集。

### 3.2 在 `metrics.py` 中注册

1. 在模块顶层创建指标对象，指标名建议统一前缀 **`dagents_`**，避免与其它 exporter 冲突。  
2. 编写 **`HELP` 友好的字符串**（中文或中英均可，与现有风格一致）。  
3. **`labelnames`** 只列 **维度字段**，不要把大块文本当 label。

示例（仅作模板，勿直接复制业务语义）：

```python
from prometheus_client import Counter

MY_FEATURE_HITS = Counter(
    "dagents_my_feature_hits_total",
    "某特性被触发的累计次数",
    labelnames=("outcome",),
)

def record_my_feature_hit(*, outcome: str) -> None:
    MY_FEATURE_HITS.labels(outcome=sanitize_prometheus_label_value(outcome, max_len=32)).inc()
```

### 3.3 在业务路径调用

- **Counter**：在「事件确定发生」且 **副作用已提交** 的路径上调用（避免请求中途失败仍计数）。  
- **Gauge（全量刷新模式）**：参考 **`refresh_session_context_metrics`**（按 **`session_id`** 维度 **`set`** 并在 session 消失时 **`remove`**），在状态变更后传入权威数据源。  
- **避免** 在极高频路径（每条日志一次）无节制 **`inc(1)`** 前先评估是否需要聚合或采样（多数中等 QPS 场景可直接计数）。

### 3.4 验证与文档

1. 本地启动 API，确认 **`METRICS_ENABLED=true`**，访问 **`GET /metrics`**，检查 **`HELP`/`TYPE`/样本行** 是否符合预期。  
2. 为解析与注册逻辑补充 **`tests/test_*.py`**（可参考 **`tests/test_metrics_tokens.py`**、**`tests/test_session_context_metrics.py`**）。  
3. 更新 **`app/observability/REFERENCE.md`**（及必要时 **`doc/项目实现总览.md`** 可观测性小节），便于后续维护者发现新指标。

### 3.5 多进程 / 多 Worker 说明（前瞻）

当前默认 **`run_agent_api.py`** 使用 **单 worker**；**`prometheus_client` 默认 Registry 为进程内内存**。若未来使用 **多 worker Uvicorn**，每个进程有独立 Registry，抓取到的 **`/metrics`** 仅代表 **当前 worker**；需要聚合时可改用 **`prometheus_client` 的 multiprocess 模式** 或 sidecar 聚合方案。届时再单独扩展文档与启动方式。

---

## 4. 相关代码路径（速查）

| 路径 | 说明 |
|------|------|
| **`app/observability/metrics.py`** | 指标定义、**`metrics_text()`**、**`record_llm_token_usage`**、**`refresh_session_context_metrics`** |
| **`app/harness/api/app.py`** | **`GET /metrics`** 路由与 **`METRICS_ENABLED`** 门控 |
| **`app/config/settings.py`** | **`metrics_enabled` / `METRICS_ENABLED`** |
| **`app/harness/service/agent_service.py`** | 调用 **`refresh_session_context_metrics`** 的时机 |

---

## 5. 延伸阅读

- Prometheus 官方文档：[https://prometheus.io/docs/introduction/overview/](https://prometheus.io/docs/introduction/overview/)  
- Python **`prometheus_client`**：[https://github.com/prometheus/client_python](https://github.com/prometheus/client_python)
