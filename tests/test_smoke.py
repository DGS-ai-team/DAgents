"""占位冒烟：保证 `unittest discover` 在清空旧用例后仍能发现至少一条用例。

逻辑：
1. 只做轻量 import，验证仓库在 `PYTHONPATH=仓库根` 下可解析核心包。

关键边界：
- 不访问网络、不读 `.env`、不依赖 OpenAI SDK 是否已安装（仅 import 本仓库模块）。
"""

from __future__ import annotations

import unittest


class WorkspaceImportSmokeTest(unittest.TestCase):
    def test_import_harness_queue(self) -> None:
        from app.harness.queue.message_queue import MessageEnvelope, MessageQueue

        # Pydantic v2 下 model_config 常为 dict，用 model_fields 做稳定冒烟断言。
        self.assertIn("session_id", MessageEnvelope.model_fields)
        self.assertTrue(callable(MessageQueue))


if __name__ == "__main__":
    unittest.main()
