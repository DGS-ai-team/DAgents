# `tests/integration/`

可选**联网**测试，不在默认 PR 流水线中强制要求密钥；与根目录 `tests/` 下的单测区分。

| 文件 | 说明 |
|------|------|
| **`live_llm_smoke.py`** | 当 `RUN_LIVE_LLM_TESTS=1` 且已设置 `LLM_API_KEY` 时，对当前 `LLM_API_BASE` / `LLM_MODEL` 发起一次最小 chat 请求（文件名故意不用 `test_*.py`，以免被默认 `discover` 收进去） |

运行示例（仓库根目录）：

```bash
export RUN_LIVE_LLM_TESTS=1
export LLM_API_KEY="..."
# 可选
export LLM_API_BASE="https://api.openai.com/v1"
export LLM_MODEL="gpt-4o-mini"

python -m unittest tests.integration.live_llm_smoke -v
```

CI 中请使用 `.github/workflows/manual-live-llm-tests.yml`（手动触发 + 仓库 Secret `LLM_API_KEY`，可选输入覆盖 base/model）。
