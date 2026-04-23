# DAgents API 文档（后端代码整理版）

本文档基于当前后端实现整理，面向前端接入与联调。  
代码来源：`app/harness/api/app.py`、`app/harness/service/agent_service.py`、`app/harness/streaming/events.py`、`app/harness/queue/message_queue.py`。

## 1. 基本信息

- 服务启动入口：`run_agent_api.py`
- 默认监听地址：`127.0.0.1:8000`
- API 前缀：`/v1`
- 认证：当前无鉴权
- CORS：由 `API_CORS_ALLOW_ORIGINS` 控制，默认允许本地 Vite 来源

## 2. 统一约定

- 所有普通 HTTP 接口返回 JSON。
- 流式接口使用 SSE（`text/event-stream`）。
- 提交消息后不返回 `request_id`，前端按 `session_id` 组织会话展示。
- SSE 通道通过 `client_id` 标识，同一前端实例应复用同一 `client_id`。
- 事件总线标准字段（SSE data 的 envelope）：
  - `client_id: string`
  - `session_id: string`
  - `type: string`
  - `seq: number`
  - `ts: string`（ISO 时间）
  - `data: object`（事件载荷）

## 3. HTTP 接口

### 3.1 健康检查

- `GET /health`
- 用途：进程存活检查
- 响应示例：

```json
{"status":"ok"}
```

---

### 3.2 创建会话

- `POST /v1/sessions`
- 请求体：
  - `session_id?: string`（可选；不传时服务端生成）
- 响应体：
  - `session_id: string`
  - `created: bool`（当前实现固定返回 `true`）
- 响应示例：

```json
{
  "session_id": "s-web",
  "created": true
}
```

---

### 3.3 提交消息 / 恢复执行

- `POST /v1/messages`
- 请求体字段：
  - `session_id: string`（必填）
  - `client_id: string`（建议必填；用于关联 SSE 通道）
  - `request_type: "message" | "resume"`（默认 `message`）
  - `content?: string`（`message` 时必填且非空白）
  - `resume_value?: any`（`resume` 时使用）
  - `source?: string`（默认 `api`）
  - `priority?: "tool_result" | "human" | "resume" | "other"`
    - 未传时自动填充：
      - `message -> human`
      - `resume -> resume`
- 响应体字段：
  - `accepted: bool`
  - `session_id: string`
  - `priority: string`

#### message 示例

```json
{
  "session_id": "s-web",
  "client_id": "client-001",
  "request_type": "message",
  "content": "你好",
  "source": "frontend"
}
```

#### resume 示例（选择性审批）

```json
{
  "session_id": "s-web",
  "client_id": "client-001",
  "request_type": "resume",
  "resume_value": {
    "type": "selection",
    "approved": ["tool-call-id-1"],
    "rejected": ["tool-call-id-2"]
  },
  "source": "frontend"
}
```

#### `resume_value` 支持结构

- `{"type":"approve"}`
- `{"type":"reject"}`
- `{"type":"selection","approved":[...],"rejected":[...]}`

---

### 3.4 取消当前推理

- `POST /v1/sessions/{session_id}/cancel`
- 路径参数：
  - `session_id: string`
- 响应体：
  - `session_id: string`
  - `cancelled: bool`

---

### 3.5 全局 SSE（推荐前端单连接）

- `GET /v1/streams?client_id=<your_client_id>`
- 用途：单连接接收同一 `client_id` 下所有 session 的实时事件
- 行为：仅推送订阅建立后的实时事件，不回放历史

## 4. SSE 事件格式

SSE 帧格式：

```text
event: <event_type>
data: {"client_id":"...","session_id":"...","type":"...","seq":0,"ts":"...","data":{...}}

```

其中 `event` 与 envelope 中的 `type` 一致。

### 4.1 已实现映射的事件类型

这些事件由 `AgentService._map_event_envelope_to_stream` 明确映射：

- `assistant`
  - `data.content: string`
- `reasoning`
  - `data.content: string`
- `usage`
  - `data.prompt_tokens: number`
  - `data.completion_tokens: number`
  - `data.total_tokens: number | null`
- `tool_call`
  - `data.assistant_content: string`
  - `data.tool_calls: array`
- `tool_result`
  - `data.content: string`（工具执行结果正文；失败时为错误信息或拒绝/打断提示）
  - `data.tool_call_id: string | null`
  - `data.tool_name: string | null`
  - `data.display_type: "terminal" | "code" | "normal_text" | "image"`
  - `data.rejected: bool`
  - `data.interrupted_by_user_message: bool`
  - `data.partial: bool`
- `approval_required`
  - `data.approval_type: string`（当前为 `execute_tool`）
  - `data.content: string`
  - `data.approval_args: object`（含 `tool_calls`）
  - `data.description: string`
  - `data.approval_id: string | null`
- `error`
  - `data.message: string`
- `done`
  - `data: object`（通常为空对象）

### 4.2 `meta` 字段

以上事件 payload 会附带 `data.meta`，来自运行时基础元信息与事件元信息合并。

## 5. 错误语义

- 参数错误：
  - 常见为 `422`（如缺失 `content`）
- 业务失败：
  - 常见为 `400`，`detail` 带错误信息
- SSE 订阅错误：
  - SSE 缺失 `client_id` 时：`422`（参数校验失败）

## 6. 优先级说明

消息队列优先级（值越小越优先）：

- `tool_result = -1`
- `human = 0`
- `resume = 1`
- `other = 10`

含义：

- 工具结果回灌优先于普通消息
- 用户消息优先于 resume
- 同优先级下按入队顺序稳定处理

## 7. 联调建议

- 前端推荐：
  1. 启动后生成并持有 `client_id`，建立 `GET /v1/streams?client_id=...` 单 SSE 连接
  2. 发送 `POST /v1/messages` 时携带同一 `client_id`
  3. 按 SSE envelope 中 `session_id` 做前端分流展示
- 排障顺序：
  1. 先看 `GET /health`
  2. 再看 `GET /v1/streams?client_id=...` 是否可连
  3. 最后看 `POST /v1/messages` 与 SSE 事件链是否闭环到 `done`

## 8. 环境变量（API 相关）

- `API_HOST`：监听地址
- `API_PORT`：监听端口
- `API_CORS_ALLOW_ORIGINS`：CORS 允许来源（逗号分隔）

