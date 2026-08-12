"""工作组 RunHistory / LLM 解析可观测 API。"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.workgroup.llm_chat import ChatResult, MockLLMClient, describe_llm_resolution  # noqa: E402
from manage.workgroup.models import WorkGroupCreateRequest  # noqa: E402
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.turn_kernel import TurnKernel  # noqa: E402
from fastapi.testclient import TestClient  # noqa: E402
from manage.config import ManageSettings  # noqa: E402
from manage.manage_app import create_app  # noqa: E402


class DescribeLlmResolutionTests(unittest.TestCase):
    def test_forced_and_missing(self) -> None:
        self.assertEqual(describe_llm_resolution(None, profile_id="x", mock=True)["mode"], "mock")
        self.assertEqual(describe_llm_resolution(None, profile_id="x")["reason"], "no_llm_store")


class RunHistoryApiTests(unittest.TestCase):
    def test_list_runs_and_history_after_chat(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            store: WorkGroupStore = app.state.workgroup_store
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="obs",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            wid = group.workgroup_id
            store.publish_workgroup(wid)
            # 用注入 kernel 路径：直接跑 TurnKernel 写 history，再经 HTTP 读
            kernel = TurnKernel(
                store,
                chat_client=MockLLMClient([ChatResult(content="回声你好", finish_reason="stop")]),
                mock_llm=True,
            )
            out = kernel.handle_human_message(
                wid, text="你好", from_node_id="node-a", disable_tools=True
            )
            self.assertEqual(out["loop"]["status"], "succeeded")
            runs = store.list_actor_runs(wid, limit=5)
            self.assertTrue(runs)
            run_id = runs[0].run_id
            hist = store.get_run_history(run_id)
            self.assertIsNotNone(hist)
            self.assertTrue(any(m.role == "assistant" for m in hist.messages))

            with TestClient(app) as client:
                # admin login not required if auth optional in test? use node header
                listed = client.get(
                    f"/v1/workgroups/{wid}/runs",
                    headers={"x-dagents-agent-id": "node-a"},
                )
                self.assertEqual(listed.status_code, 200, listed.text)
                body = listed.json()
                self.assertTrue(body.get("runs"))
                self.assertEqual(body.get("llm", {}).get("mode"), "mock")

                detail = client.get(
                    f"/v1/workgroups/{wid}/runs/{run_id}/history",
                    headers={"x-dagents-agent-id": "node-a"},
                )
                self.assertEqual(detail.status_code, 200, detail.text)
                hist_body = detail.json()
                self.assertEqual(hist_body["run"]["run_id"], run_id)
                roles = [m["role"] for m in hist_body["history"]["messages"]]
                self.assertIn("assistant", roles)


if __name__ == "__main__":
    unittest.main()
