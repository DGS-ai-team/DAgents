# 工具链上下文成本优化

> 分支：`feat/tool-context-cost-optimization`（**已落地**）  
> 范围：Go Node 内置工具、`turn` 工具结果写回、编排 dispatch  
> 正交专题：[context-compression-cache-analysis.md](./context-compression-cache-analysis.md)（history 体量 + 侧车 cache）  
> 实录索引：[major-changes.md](./major-changes.md) §2

---

## 1. 背景与痛点

Agent 账单 ≈ **LLM 往返次数** × **每次 input（含 replay）** × **单价** + **completion**。与压缩/cache 正交：cache 只降重复前缀单价，**不减少**轮询带来的 turn 与 output。

| 痛点 | 表现 |
|------|------|
| **低信息轮询** | `background_job_status`、`temporary_agent_status` 等瞬时 snapshot → 模型每 5～15s 一整轮 `StreamChat` |
| **tool 结果膨胀** | bash / read / grep / A2A 大段正文写入 history → 后续每轮 input 变长、更易触达压缩阈值（80k/100k） |
| **重复 tool call** | 同名同参短窗口内再次调用（尤其 status、重复 read） |
| **前缀漂移（搁置）** | `load_skills` enrich 随 catalog 变 → tools JSON miss；评估后 **不改 skills**，见 [skills-context-cost-analysis.md](./skills-context-cost-analysis.md) |

计费参考（[DeepSeek V4-Pro](https://api-docs.deepseek.com/zh-cn/quick_start/pricing)）：缓存命中 input **0.025 元/M**，未命中 **3 元/M**（**120×**），输出 **6 元/M**。

---

## 2. 分析

### 2.1 成本结构

```text
单次 StreamChat input ≈ system + tools schema + 全量 messages
每步 tool loop 追加 assistant + tool → history 增长（tail 恒为 miss）
```

| 类型 | 机制 | 本专题 |
|------|------|--------|
| Turn 次数 | 每次低信息 tool 仍产生 completion | **WS1、WS6** |
| History 体积 | 大 tool 结果放大后续 replay | **WS3** |
| Tools 前缀 | schema 漂移导致 P 段 miss | **WS4 搁置** |

### 2.2 触点（摘要）

| 组 | 主要触点 |
|----|----------|
| **bash** | 长输出；job status 轮询；async 回灌 vs poll |
| **fs** | 大段 read/grep；错编码重读；分页多 turn（设计取舍，不取消分页） |
| **a2a** | `agent_invoke` / `agent_discover` 全文进 history |
| **triggers** | CRUD + 长 condition schema；**已移除 `trigger_fire`**（仅调度/HTTP 触发） |
| **child_agents** | `temporary_agent_status` 瞬时（**WS2 未做**） |

### 2.3 与压缩边界

| 本专题 | 压缩专题 |
|--------|----------|
| 少 turn、控单条 tool 写入 | 控 messages 总量、侧车与主 turn 前缀对齐 |
| `tool.after_each` spill | `compression.silent/blocking_trigger_tokens` |

---

## 3. 优化思路

1. **能 push 不 poll**：bash job 靠 `async_tool_result` 自动回灌；不为 `background_job_status` 加 long-poll。
2. **poll 仍发生则拦截**：**WS6** `tool.before_each` 重复调用审批（60s 指纹，`rule`+auto 路径）。
3. **能短不长**：超长 tool 结果 **落盘 + history 头尾摘要** + `read_file` hint；禁止只截断不落盘。
4. **能稳不动**：skills schema 优化 **搁置**（单次 miss 溢价约分～角量级，机制成本更高）。
5. **可观测**：任务级 `tool_context_metrics` 验证治理效果。

---

## 4. 落地方案

| ID | 内容 | 状态 |
|----|------|------|
| **WS1** | bash ACK/status 文案引导 async；status **保持瞬时** | ✅ |
| **WS6** | `hooks.duplicate_tool_call` + HITL；详见 [tool-before-hook-duplicate-approval.md](./tool-before-hook-duplicate-approval.md) | ✅ |
| **WS3** | `hooks.tool_result` spill：bash、fs（含编码缓存）、a2a；`spill_threshold_tokens` 默认 12000 | ✅ |
| **WS5** | `tool_context_metrics` on `done` SSE + 日志 | ✅（基础） |
| **WS2** | 子 Agent status long-poll | ❌ 不做 |
| **WS4** | skills enrich 外置 | ❌ 搁置 |

### WS3 配置要点

```yaml
hooks:
  duplicate_tool_call:
    enabled: true
    window_seconds: 60
  tool_result:
    enabled: true
    spill_threshold_tokens: 12000
    tools:
      - bash_run
      - read_file
      - grep_file
      - grep_files
      - search_replace
      - glob_files
      - agent_invoke
      - agent_discover
tools:
  bash_compress:
    enabled: true   # 仅清洗，不截断
```

落盘目录固定：`{fs_root}/tool_outputs/<session>/<tool_call_id>.txt`。

### WS5 字段

`tool_loops`、`tool_calls`、`tool_calls_by_name`、`status_poll_count`、`spill_count`、`history_result_tokens`、`read_file_path_repeats`、编码相关计数等（见 `node/internal/turn/context_metrics.go`）。

### 代码索引

| 主题 | 路径 |
|------|------|
| spill / token 预算 | `node/internal/toolresult/` |
| duplicate hook | `node/internal/hooks/` |
| 编排 / 度量 | `node/internal/turn/tool_router.go`、`context_metrics.go` |
| bash job / 文案 | `node/internal/tools/job_registry.go`、`tool_job.go` |
| fs 编码 | `node/internal/tools/fs_encoding_*.go` |

### 后续可选（未在本分支）

- `enabled_groups` 按任务减面（降 tools schema 体积）
- triggers 结果 spill、WS5 Prometheus
- 子 Agent **WS2**、写审批信任链 [ux-agent-owned-file-approval.md](./ux-agent-owned-file-approval.md)

---

## 5. 文档格式说明

后续大型优化专题建议采用本文四段结构：**背景与痛点 → 分析 → 优化思路 → 落地方案**；细节实录写入 [major-changes.md](./major-changes.md)，专题文保持可扫读。
