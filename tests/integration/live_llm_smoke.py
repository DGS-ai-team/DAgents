"""对配置中的 LLM 端点做一次最小 chat 调用（联网）。

默认跳过：仅当同时设置 ``RUN_LIVE_LLM_TESTS=1`` 且 ``LLM_API_KEY`` 非空时运行。
本地或手动 workflow 执行前请通过环境变量注入密钥与可选的 ``LLM_API_BASE`` / ``LLM_MODEL``，
与 ``app.config.settings.Settings`` 的命名一致。

说明：
- 文件名不使用 ``test_*.py``，避免 ``unittest discover -s tests -p "test_*.py"`` 误收联网用例。
"""

from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path

_ROOT = Path(__file__).resolve().parent.parent.parent
sys.path.insert(0, str(_ROOT))


def _live_enabled() -> bool:
    return os.environ.get("RUN_LIVE_LLM_TESTS", "").strip() == "1" and bool(
        os.environ.get("LLM_API_KEY", "").strip()
    )


@unittest.skipUnless(_live_enabled(), "设置 RUN_LIVE_LLM_TESTS=1 且 LLM_API_KEY 后启用本测试")
class LiveLlmChatSmokeTestCase(unittest.IsolatedAsyncioTestCase):
    """最小非流式 completion，验证密钥、base_url 与模型名可用。"""

    async def test_minimal_chat_completion(self) -> None:
        # 延迟导入，避免在未启用 live 时拉取重型依赖副作用；skip 时本方法不会执行。
        from app.config.settings import get_settings
        from app.core.main_agent.model import get_model_config, get_openai_client

        # 确保读取的是当前进程环境中的 LLM_*（手动 workflow 已注入）。
        get_settings(reload=True)
        cfg = get_model_config()
        model = str(cfg.get("model") or "").strip()
        if not model:
            self.skipTest("LLM_MODEL 未设置，无法发起请求")

        client = get_openai_client()
        extra = cfg.get("extra_body")
        kwargs: dict = {
            "model": model,
            "messages": [{"role": "user", "content": 'Reply with exactly the two letters: OK'}],
            "max_tokens": 32,
            "temperature": 0,
            "stream": False,
        }
        if isinstance(extra, dict) and extra:
            kwargs["extra_body"] = extra

        resp = await client.chat.completions.create(**kwargs)
        choice0 = resp.choices[0]
        msg = choice0.message
        text = str(getattr(msg, "content", None) or "").strip()
        self.assertTrue(text, "期望模型返回非空文本")


if __name__ == "__main__":
    unittest.main()
