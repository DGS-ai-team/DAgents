# Prometheus 观测说明

> **现网**：Manage 暴露 `GET /metrics`（`manage/platform/metrics.py`）。Go Agent Node **尚未**暴露 Prometheus 端点。

本文说明 Prometheus 拉取机制、Manage 现网指标，以及如何安全新增指标。

---

## 1. Prometheus 机制

Prometheus **主动拉取（pull）** 目标的 `GET /metrics` 端点，将时间序列存入 TSDB，再用 PromQL 查询与告警。

响应为 **text exposition** 格式：`# HELP` / `# TYPE` 元数据 + `metric_name{label="value"} numeric_value`。

| 类型 | 语义 | 典型用法 |
|------|------|----------|
| **Counter** | 只增不减 | 操作次数、累计 token |
| **Gauge** | 可升可降 | 当前队列长度、快照值 |
| **Histogram** | 分布 | 延迟分布 |

**Label 基数**：不要把用户原文、无界 session ID 全文放进 label；动态字符串须清洗与截断。

---

## 2. Manage 指标（现网）

**路由**：`GET /metrics`（`manage/manage_app.py`）  
**实现**：`manage/platform/metrics.py`

| 指标 | 说明 |
|------|------|
| `dagents_manage_registry_operations_total{operation,status}` | Registry 注册 / 心跳等操作计数 |
| Workgroup 指标 | 当前版本尚未提供稳定的 Workgroup 专用指标；不要用旧 A2A 指标推断工作组状态 |

新增 Manage 指标：在 `metrics.py` 定义 Counter/Gauge，在对应 `routes.py` / `store.py` 调用 `record_*`，前缀统一 `dagents_manage_`。

---

## 3. Go Agent Node（待做）

Node 侧 turn / session / 工具链指标尚未接入 Prometheus。后续优先补充 turn 延迟、工具结果、HITL 等低基数指标，路线以 [docs/roadmap.md](../../roadmap.md) 为准。

---

## 4. 新增指标（推荐步骤）

1. **选型**：累计事件 → Counter；快照状态 → Gauge。  
2. **注册**：在对应 `metrics.py` 模块顶层创建指标对象，label 只列维度字段。  
3. **调用**：在副作用已提交的路径上 `inc()` / `set()`。  
4. **验证**：本地启动服务，`curl /metrics` 检查 HELP/TYPE/样本行。  
5. **测试**：补充单测或集成断言。

---

## 5. 相关路径

| 路径 | 说明 |
|------|------|
| `manage/platform/metrics.py` | Manage 指标定义与 `metrics_text()` |
| `manage/manage_app.py` | `GET /metrics` 路由 |

---

## 延伸阅读

- [Prometheus 官方文档](https://prometheus.io/docs/introduction/overview/)
- [prometheus_client（Python）](https://github.com/prometheus/client_python)
