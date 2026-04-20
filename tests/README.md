# `tests/`

单元测试目录（逐步补齐）。

| 文件 | 说明 |
|------|------|
| **`test_agent_service.py`** | 验证 `AgentService` 启动、session 消费、`human` 入队不自动 cancel、`cancel_current_turn` 触发 flush、`run_turn` 替身 |
| **`test_message_queue.py`** | 验证优先级（`human` > `resume` > `other`）、`receive` 出队、自定义 envelope |
| **`test_schema_approval.py`** | 验证 **`app.schemas.approval`** 中 resume 解析与审批载荷 |
| **`test_metrics_tokens.py`** | 验证 **`parse_usage_tokens`**、**`record_llm_token_usage`**（Prometheus token Counter） |
| **`test_api_sse.py`** | 验证 FastAPI 提交消息与 SSE 流（`/v1/streams/{request_id}`） |
| **`test_api_cancel_turn.py`** | 验证 **`POST /v1/sessions/{session_id}/cancel`**（无在途 turn 时 **`cancelled=false`**） |
| **`call_agent_api.py`** | 手动联调脚本：调用 `/v1/messages` 并订阅 SSE 流 |
| **`call_runtime_request_model_stream.py`** | 手动调试脚本：直接调用 `_request_model_stream`，同时打印 OpenAI 原始 chunk 与 runtime 事件 |

运行方式（仓库根目录）：

```bash
python -m unittest tests.test_agent_service
python -m unittest tests.test_message_queue
python -m unittest tests.test_api_sse

# 手动联调（需先启动 run_agent_api.py）
python tests/call_agent_api.py

# 直接调试 runtime 流式（不经过 service/api）
python tests/call_runtime_request_model_stream.py
python tests/call_runtime_request_model_stream.py "解释一下 tool call 分片返回"
```

