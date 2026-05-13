# `tests/`

单元测试目录（**已清空历史用例**，按 **`UNIT_TEST_CHECKLIST.md`** 分阶段补回）。

| 文件 / 目录 | 说明 |
|-------------|------|
| **`UNIT_TEST_CHECKLIST.md`** | **单元测试清单**：按模块的优先级、建议文件名与断言要点 |
| **`test_smoke.py`** | 最小冒烟：保证 `unittest discover` 至少发现 1 条用例（CI 非空） |
| **`test_config_settings.py`** | `app.config`：`get_settings` / `load_env` / `resolve_runtime_root` |
| **`test_message_queue.py`** | `MessageQueue` 优先级、FIFO、`stop`/`receive`、观测堆 |
| **`test_agent_service.py`** | `AgentService` 生命周期与 `_map_event_envelope_to_stream`（**无 `requirements` 时整文件 skip**） |
| **`test_context_models.py`** | `OpenAIConversationContext` / `ConversationContext` 往返与规范化 |
| **`test_raw_message_journal.py`** | 原始消息 JSONL：`record_*` / `append_*` / `insert_*` |
| **`test_schema_approval.py`** | 工具审批 resume 解析 |
| **`test_streaming_events.py`** | `InMemoryEventBus` |
| **`test_support/`** | 单测替身（如 Settings `SimpleNamespace`） |
| **`integration/`** | 可选联网集成测试（默认 skip）；见子目录 **`README.md`** |

> `test_agent_service.py` 在导入 `AgentService` 失败（例如未安装 `openai`）时，相关用例以 **skip** 跳过；CI 安装 **`requirements.txt`** 后应全部执行。

## 运行（仓库根目录）

```bash
pip install -r requirements.txt
python -m unittest discover -s tests -p "test_*.py" -v
```

单文件示例：

```bash
python -m unittest tests.test_smoke -v
# 可选联网 LLM 冒烟（需环境变量，见 integration/README.md；模块名非 test_*，故不在 discover 中）
python -m unittest tests.integration.live_llm_smoke -v
```

## CI

`.github/workflows/pr-tests.yml` 与上式一致；**不**自动执行 `tests/integration/` 内用例。

## 规划入口

补写用例前请先阅读 **`UNIT_TEST_CHECKLIST.md`**，按 **P0 → P1 → P2** 迭代，避免一次性铺开难以 review。
