"""Manage-native workgroup tool schema is loaded from the runtime package."""

from __future__ import annotations

import json
import threading
import time
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from manage.storage.sqlite import SQLiteDatabase
from manage.workgroup.native_tools import load_assign_workgroup_task_tool
from manage.workgroup.native_tools import NativeToolDispatcher
from manage.workgroup.models import ActorRunCreateRequest, WorkGroupCreateRequest
from manage.workgroup.store import WorkGroupStore


class ManageNativeToolSchemaTests(unittest.TestCase):
    def test_assign_tool_schema_is_packaged_with_manage(self) -> None:
        schema_path = (
            Path(__file__).resolve().parents[1]
            / "manage"
            / "workgroup"
            / "schemas"
            / "assign_workgroup_task.openai.json"
        )
        self.assertTrue(schema_path.is_file())
        raw = json.loads(schema_path.read_text(encoding="utf-8"))
        tool = load_assign_workgroup_task_tool()
        self.assertEqual(tool["function"]["name"], "assign_workgroup_task")
        self.assertEqual(tool["function"], raw["function"])
        self.assertEqual(tool["function"]["parameters"]["required"][0], "call_purpose")
        self.assertNotIn("result_schema", tool)

    def test_ask_user_binds_waiter_and_returns_resolved_tool_result(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "manage.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="HITL", created_by_node_id="node-a")
            )
            store.publish_workgroup(group.workgroup_id)
            run = store.create_actor_run(
                group.workgroup_id,
                ActorRunCreateRequest(actor_id="leader"),
            )
            dispatcher = NativeToolDispatcher(store, leader_run_id=run.run_id)
            result: list[str] = []
            errors: list[Exception] = []

            def invoke() -> None:
                try:
                    result.append(
                        dispatcher.dispatch(
                            workgroup_id=group.workgroup_id,
                            tool_name="ask_workgroup_user",
                            tool_call_id="call_live_hitl",
                            arguments_json='{"prompt":"continue?"}',
                        )
                    )
                except Exception as exc:  # pragma: no cover - assertion below reports it
                    errors.append(exc)

            worker = threading.Thread(target=invoke)
            worker.start()
            for _ in range(100):
                pending = store.list_pending_hitls()
                if pending and store.has_hitl_waiter(pending[0].hitl_id):
                    break
                time.sleep(0.01)
            self.assertFalse(errors)
            self.assertEqual(len(pending), 1)
            hitl = pending[0]
            self.assertEqual(hitl.run_id, run.run_id)
            self.assertEqual(hitl.tool_call_id, "call_live_hitl")
            store.resolve_hitl_cas(
                group.workgroup_id,
                hitl.hitl_id,
                resolution={"answer": "yes"},
            )
            worker.join(timeout=2)
            self.assertFalse(worker.is_alive())
            self.assertFalse(errors)
            self.assertEqual(len(result), 1)
            payload = json.loads(result[0])
            self.assertEqual(payload["status"], "answered")
            self.assertEqual(payload["answer"], "yes")


if __name__ == "__main__":
    unittest.main()
