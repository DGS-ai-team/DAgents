# `tests/integration/` REFERENCE

## `live_llm_smoke.py`

- **`_live_enabled`**：判断是否同时满足 `RUN_LIVE_LLM_TESTS=1` 与 `LLM_API_KEY` 非空
- **`LiveLlmChatSmokeTestCase`**：`unittest.IsolatedAsyncioTestCase`，`test_minimal_chat_completion` 调用 `get_openai_client().chat.completions.create` 做最小非流式请求
