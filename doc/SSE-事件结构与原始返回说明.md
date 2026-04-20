# SSE 事件结构与原始返回说明

本文用于记录当前 DAgents 在流式模式下的事件结构，及 OpenAI 原始流式 chunk 的解析规则，便于后续排查与联调。

## 1. OpenAI 原始流式返回（chunk）结构

当前脚本观测到的每个原始分片（`chat.completion.chunk`）核心字段如下：

```json
{
  "id": "019d76a229ad80559ac677368a89e20a",
  "object": "chat.completion.chunk",
  "model": "Pro/deepseek-ai/DeepSeek-V3.2",
  "choices": [
    {
      "index": 0,
      "delta": {
        "role": "assistant",
        "content": null,
        "reasoning_content": "让我",
        "tool_calls": null
      },
      "finish_reason": null
    }
  ],
  "usage": {
    "completion_tokens": 48,
    "prompt_tokens": 1162,
    "total_tokens": 1210
  }
}
```

在工具调用场景中，`delta.tool_calls` 会分片返回，例如：

```json
{
  "choices": [
    {
      "delta": {
        "role": "assistant",
        "tool_calls": [
          {
            "index": 0,
            "id": "019d76a23b1b2bf067f27d511a584766",
            "type": "function",
            "function": {
              "name": "bash_run",
              "arguments": ""
            }
          }
        ]
      },
      "finish_reason": null
    }
  ]
}
```

后续参数会继续以 `function.arguments` 片段累加，例如 `"{`、`\"command\": \"un`、`ame -`、`a\"}`，最后在 `finish_reason="tool_calls"` 收束。

## 2. 原始 chunk 到运行时类别的判定规则

当前解析规则（`runtime_openai.py`）：

1. `role=assistant` 且 `tool_calls` 非空  
   -> 归入工具调用分片，按 `index` 拼接 `id/type/function.name/function.arguments`。
2. 若 `content` 非空  
   -> 归入 assistant 文本流。
3. 若 `content` 为空且 `reasoning_content` 非空  
   -> 归入 reasoning 文本流。
4. 当流结束后，根据累积结果产出最终消息对象（含完整 `tool_calls`），进入审批或结束逻辑。
5. 若某 chunk 带 `usage` 且 `choices` 为空（或仅携带 usage），先记 Prometheus，再向 `run_turn` 产出 **`usage`** SSE 事件（与正文分片独立）。

## 3. 当前 SSE 顶层事件类型

当前对外 SSE `event` 类型如下（语义化）：

- `assistant`：模型正文流式片段（逐段）
- `reasoning`：模型思考流式片段（逐段）
- `usage`：单次 Chat Completions 流式请求末尾由提供商返回的 token 统计（需 `stream_options.include_usage`；部分网关可能无此事件）
- `tool_call`：模型提出工具调用（审批前汇总）
- `tool_result`：工具执行后的返回结果
- `approval_required`：需要用户审批后才能继续
- `error`：异常事件
- `done`：本次请求结束
- `chunk`：兜底事件（未识别类型时）

## 4. 关键事件 data 结构

### 4.1 `assistant`

```json
{
  "content": "你好，",
  "meta": {
    "session_id": "…",
    "request_id": "…",
    "model": "…"
  }
}
```

`meta` 由 **`AgentService`** 注入公共字段（`session_id` / `request_id` / `model`），并与 runtime 信封上的 **`envelope.meta`** 合并（后者当前多为空）。

### 4.2 `reasoning`

```json
{
  "content": "我需要先确认工具能力。",
  "meta": {
    "session_id": "…",
    "request_id": "…",
    "model": "…"
  }
}
```

### 4.2.1 `usage`

```json
{
  "prompt_tokens": 1162,
  "completion_tokens": 48,
  "total_tokens": 1210,
  "meta": {
    "session_id": "…",
    "request_id": "…",
    "model": "…"
  }
}
```

`total_tokens` 在提供商未返回时可能为 JSON `null`。

### 4.3 `tool_call`

```json
{
  "assistant_content": "",
  "tool_calls": [
    {
      "id": "019d76a23b1b2bf067f27d511a584766",
      "name": "bash_run",
      "arguments": {
        "command": "uname -a"
      },
      "raw_arguments": "{\"command\":\"uname -a\"}"
    }
  ],
  "meta": {
    "session_id": "…",
    "request_id": "…",
    "model": "…"
  }
}
```

### 4.4 `tool_result`

```json
{
  "tool_name": "bash_run",
  "tool_call_id": "019d76a23b1b2bf067f27d511a584766",
  "content": "Linux ...",
  "meta": {
    "session_id": "…",
    "request_id": "…",
    "model": "…"
  }
}
```

### 4.5 `approval_required`

```json
{
  "approval_type": "execute_tool",
  "content": "检测到工具调用，等待用户确认后继续执行。",
  "approval_args": {
    "tool_calls": [
      {
        "id": "019d76a23b1b2bf067f27d511a584766",
        "name": "bash_run",
        "arguments": {
          "command": "uname -a"
        }
      }
    ]
  },
  "description": "OpenAI tool calling 审批",
  "approval_id": null,
  "meta": {
    "session_id": "…",
    "request_id": "…",
    "model": "…"
  }
}
```

### 4.6 `done`

```json
{
  "meta": {
    "session_id": "…",
    "request_id": "…",
    "model": "…"
  }
}
```

## 5. 备注

- 该文档基于当前实现与日志样本（`terminals/3.txt` 指定区间）整理。
- 若后续调整解析策略或事件命名，应同步更新本文与对应目录 README/REFERENCE。
