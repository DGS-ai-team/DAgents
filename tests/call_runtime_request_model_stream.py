from __future__ import annotations

import asyncio
import json
import sys
from pathlib import Path
from typing import Any

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.config.env import load_env
from app.core.main_agent.runtime_openai import OpenAIImplicitReActRuntime


def _chunk_to_json(chunk: Any) -> str:
    """把 OpenAI SDK chunk 转成可打印 JSON 字符串。

    逻辑：
    1. 优先使用 SDK 对象的 `model_dump`；
    2. 若对象不支持，降级为 `str(chunk)`；
    3. 统一返回单行 JSON 文本，便于 grep/重定向分析。
    """
    model_dump = getattr(chunk, "model_dump", None)
    if callable(model_dump):
        try:
            return json.dumps(model_dump(), ensure_ascii=False)
        except Exception:
            return str(chunk)
    return str(chunk)


async def main() -> None:
    """直接调用 `_request_model_stream` 并仅打印原始 chunk。

    逻辑：
    1. 初始化 runtime，并包装 `chat.completions.create`；
    2. 在包装层透传每个原始 chunk，同时打印 `[raw_chunk] ...`；
    3. 调用 `_request_model_stream` 仅用于驱动流消费，不做任何额外输出。

    关键边界：
    - 这是手动调试脚本，不进入 service/api/cli；
    - 需要本地 `.env` 中有可用 OpenAI 配置。
    """
    load_env(_ROOT)
    runtime = OpenAIImplicitReActRuntime()

    original_create = runtime._client.chat.completions.create

    async def _create_with_raw_log(*args: Any, **kwargs: Any):
        stream = await original_create(*args, **kwargs)

        class _LoggedStream:
            def __aiter__(self):
                return self

            async def __anext__(self):
                chunk = await stream.__anext__()
                print(f"[raw_chunk] {_chunk_to_json(chunk)}", flush=True)
                return chunk

        return _LoggedStream()

    runtime._client.chat.completions.create = _create_with_raw_log  # type: ignore[assignment]

    user_prompt = "请先调用 host_platform 工具获取当前宿主机信息，再用一句话总结。"
    if len(sys.argv) > 1:
        user_prompt = " ".join(sys.argv[1:]).strip() or user_prompt

    messages = [{"role": "user", "content": user_prompt}]

    async for event in runtime._request_model_stream(messages):
        del event


if __name__ == "__main__":
    asyncio.run(main())

