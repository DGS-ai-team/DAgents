# `tests/`

单元测试目录（逐步补齐）。

| 文件 | 说明 |
|------|------|
| **`test_agent_service.py`** | 验证 `AgentService` 启动、session 消费、`human` 入队不自动 cancel、`cancel_current_turn` 触发 flush、`run_turn` 替身 |
| **`test_message_queue.py`** | 验证优先级（`human` > `resume` > `other`）、`receive` 出队、自定义 envelope |
| **`test_schema_approval.py`** | 验证 **`app.schemas.approval`** 中 resume 解析与审批载荷 |
| **`test_metrics_tokens.py`** | 验证 **`parse_usage_tokens`**、**`record_llm_token_usage`**（Prometheus token Gauge） |
| **`test_api_sse.py`** | 验证 FastAPI 提交消息与 SSE 流（`GET /v1/streams?client_id=...`） |
| **`test_api_cancel_turn.py`** | 验证 **`POST /v1/sessions/{session_id}/cancel`**（无在途 turn 时 **`cancelled=false`**） |
| **`test_bash_su_guard.py`** | 验证 **`bash_run`** 特权拦截（mock **`get_host_snapshot`**） |
| **`test_host_snapshot.py`** | 验证 **`capture_host_snapshot_at_startup`** / **`get_host_snapshot`** 缓存 |
| **`test_prompt_runtime_env.py`** | 验证 **`get_system_prompt`** 运行环境段基于 **`get_host_snapshot`**（OS + 用户） |
| **`test_session_context_metrics.py`** | 验证 **`dagents_session_context_messages_count`**（**`refresh_session_context_metrics`**） |
| **`test_raw_message_journal.py`** | 验证 **`append_openai_message_with_journal`** / **`record_raw_openai_message_append`**（JSONL 路径、开关、`session_id` 为空跳过） |
| **`call_agent_api.py`** | 手动联调脚本：调用 `/v1/messages` 并订阅 SSE 流 |
| **`call_runtime_request_model_stream.py`** | 手动调试脚本：直接调用 `_request_model_stream`，同时打印 OpenAI 原始 chunk 与 runtime 事件 |
| **`integration/`** | 可选联网集成测试（默认跳过）；说明见子目录 `README.md` |

CI：向默认分支提交的 **Pull Request** 会由 `.github/workflows/pr-tests.yml` 在 **Python 3.13** 下执行：

`python -m unittest discover -s tests -p "test_*.py" -v`

（`tests/integration/` 内测试默认跳过，不消耗密钥。）

运行方式（仓库根目录）：

```bash
python -m unittest discover -s tests -p "test_*.py" -v

python -m unittest tests.test_agent_service
python -m unittest tests.test_message_queue
python -m unittest tests.test_api_sse

# 可选：真实 LLM 联网冒烟（需密钥，见 tests/integration/README.md）
# export RUN_LIVE_LLM_TESTS=1 LLM_API_KEY=...
# python -m unittest tests.integration.test_llm_live -v

# 手动联调（需先启动 run_agent_api.py）
python tests/call_agent_api.py

# 直接调试 runtime 流式（不经过 service/api）
python tests/call_runtime_request_model_stream.py
python tests/call_runtime_request_model_stream.py "解释一下 tool call 分片返回"
```

