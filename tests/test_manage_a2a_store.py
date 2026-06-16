from __future__ import annotations

import sys
import threading
import time
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.a2a.models import TaskCreateRequest, TaskReplyRequest, TaskStoredRecord  # noqa: E402
from manage.a2a.store import A2ATaskStore, inbox_content_preview, stored_to_inbox_item  # noqa: E402
from manage.storage.sqlite import SQLiteDatabase  # noqa: E402


def _always_ok(_agent_id: str) -> tuple[bool, str | None]:
    return True, None


def _store(**kwargs: object) -> A2ATaskStore:
    if "expire_sweep_seconds" not in kwargs:
        kwargs["expire_sweep_seconds"] = 0
    return A2ATaskStore(**kwargs)


def _create(store: A2ATaskStore, **kwargs: object) -> str:
    payload = TaskCreateRequest(
        from_agent_id=str(kwargs.get("from_agent_id", "caller")),
        to_agent_id=str(kwargs.get("to_agent_id", "callee")),
        content=str(kwargs.get("content", "hello")),
        idempotency_key=str(kwargs.get("idempotency_key", "")),
        ttl_seconds=int(kwargs.get("ttl_seconds", 3600)),
    )
    task, _ = store.create(payload, validate_target=_always_ok)
    return task.task_id


class A2ATaskStoreTests(unittest.TestCase):
    def test_create_idempotency_returns_same_task(self) -> None:
        store = _store()
        first, created1 = store.create(
            TaskCreateRequest(
                from_agent_id="a",
                to_agent_id="b",
                content="one",
                idempotency_key="dup",
            ),
            validate_target=_always_ok,
        )
        second, created2 = store.create(
            TaskCreateRequest(
                from_agent_id="a",
                to_agent_id="b",
                content="two",
                idempotency_key="dup",
            ),
            validate_target=_always_ok,
        )
        self.assertTrue(created1)
        self.assertFalse(created2)
        self.assertEqual(first.task_id, second.task_id)
        self.assertEqual(second.content, "one")

    def test_create_rejects_invalid_target(self) -> None:
        store = _store()

        def reject(_agent_id: str) -> tuple[bool, str | None]:
            return False, "target_offline"

        with self.assertRaises(ValueError) as ctx:
            store.create(
                TaskCreateRequest(from_agent_id="a", to_agent_id="b", content="x"),
                validate_target=reject,
            )
        self.assertEqual(str(ctx.exception), "target_offline")

    def test_poll_inbox_marks_delivered_and_deduplicates(self) -> None:
        store = _store()
        task_id = _create(store)
        items, pending = store.poll_inbox("callee", limit=10, wait_seconds=0)
        self.assertEqual(len(items), 1)
        self.assertEqual(items[0].task_id, task_id)
        self.assertEqual(pending, 0)

        again, pending2 = store.poll_inbox("callee", wait_seconds=0)
        self.assertEqual(again, [])
        self.assertEqual(pending2, 0)

        record = store.get(task_id)
        assert record is not None
        self.assertEqual(record.status, "delivered")

    def test_poll_inbox_wakes_on_new_task(self) -> None:
        store = _store()
        result: list = []

        def waiter() -> None:
            items, _ = store.poll_inbox("callee", wait_seconds=2.0)
            result.append(items)

        thread = threading.Thread(target=waiter, daemon=True)
        thread.start()
        time.sleep(0.05)
        _create(store, content="late")
        thread.join(timeout=3.0)
        self.assertFalse(thread.is_alive())
        self.assertEqual(len(result), 1)
        self.assertEqual(len(result[0]), 1)
        self.assertEqual(result[0][0].content, "late")

    def test_ack_and_reply_transitions(self) -> None:
        store = _store()
        task_id = _create(store)
        store.poll_inbox("callee", wait_seconds=0)

        acked = store.ack(task_id, "callee")
        assert acked is not None
        self.assertEqual(acked.status, "processing")

        replied = store.reply(
            task_id,
            "callee",
            TaskReplyRequest(agent_id="callee", status="completed", result_text="ok"),
        )
        assert replied is not None
        self.assertEqual(replied.status, "completed")
        self.assertEqual(replied.result_text, "ok")

    def test_reply_failed_status(self) -> None:
        store = _store()
        task_id = _create(store)
        store.poll_inbox("callee", wait_seconds=0)
        replied = store.reply(
            task_id,
            "callee",
            TaskReplyRequest(
                agent_id="callee",
                status="failed",
                error_detail="boom",
            ),
        )
        assert replied is not None
        self.assertEqual(replied.status, "failed")
        self.assertEqual(replied.error_detail, "boom")

    def test_ack_wrong_agent_returns_none(self) -> None:
        store = _store()
        task_id = _create(store)
        self.assertIsNone(store.ack(task_id, "other"))

    def test_expire_stale_tasks(self) -> None:
        store = _store()
        fixed = 1_000_000
        with patch("manage.a2a.store.time.time", return_value=fixed):
            task_id = _create(store, ttl_seconds=10)
        with patch("manage.a2a.store.time.time", return_value=fixed + 11):
            expired = store.sweep_expired()
            record = store.get(task_id)
        self.assertEqual(expired, 1)
        assert record is not None
        self.assertEqual(record.status, "expired")

    def test_count_pending_for(self) -> None:
        store = _store()
        self.assertEqual(store.count_pending_for("callee"), 0)
        _create(store)
        _create(store, content="two")
        self.assertEqual(store.count_pending_for("callee"), 2)
        store.poll_inbox("callee", wait_seconds=0)
        self.assertEqual(store.count_pending_for("callee"), 0)

    def test_sqlite_persist_and_reload(self) -> None:
        with TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "manage.db"
            db = SQLiteDatabase(db_path)
            store = _store(db=db)
            task_id = _create(store, content="persist")
            store.poll_inbox("callee", wait_seconds=0)
            store.reply(
                task_id,
                "callee",
                TaskReplyRequest(agent_id="callee", status="completed", result_text="done"),
            )

            reloaded = _store(db=db)
            record = reloaded.get(task_id)
            assert record is not None
            self.assertEqual(record.status, "completed")
            self.assertEqual(record.result_text, "done")

    def test_inbox_content_truncated_in_poll(self) -> None:
        store = _store(inbox_content_max_chars=8)
        long_text = "0123456789abcdef"
        _create(store, content=long_text)
        items, _ = store.poll_inbox("callee", wait_seconds=0)
        self.assertEqual(len(items), 1)
        self.assertTrue(items[0].content_truncated)
        self.assertEqual(items[0].content, "01234567…")
        full = store.get(items[0].task_id)
        assert full is not None
        self.assertEqual(full.content, long_text)

    def test_pending_index_isolated_by_agent(self) -> None:
        store = _store()
        _create(store, to_agent_id="callee-a", content="a")
        _create(store, to_agent_id="callee-b", content="b")
        _create(store, to_agent_id="callee-b", content="b2")
        self.assertEqual(store.count_pending_for("callee-a"), 1)
        self.assertEqual(store.count_pending_for("callee-b"), 2)
        items, pending = store.poll_inbox("callee-a", wait_seconds=0)
        self.assertEqual(len(items), 1)
        self.assertEqual(items[0].content, "a")
        self.assertEqual(pending, 0)
        self.assertEqual(store.count_pending_for("callee-b"), 2)

    def test_inbox_content_preview_helper(self) -> None:
        preview, truncated = inbox_content_preview("hello world", max_chars=5)
        self.assertTrue(truncated)
        self.assertEqual(preview, "hello…")
        record = TaskStoredRecord(
            task_id="t1",
            from_agent_id="a",
            to_agent_id="b",
            kind="invoke",
            content="x" * 20,
            created_at_unix=1,
            updated_at_unix=1,
            expires_at_unix=99,
        )
        item = stored_to_inbox_item(record, max_content_chars=10)
        self.assertTrue(item.content_truncated)
        self.assertEqual(len(item.content), 11)

    def test_submit_caller_resume_and_poll_input(self) -> None:
        store = _store()
        task_id = _create(store)
        store.ack(task_id, "callee")
        store.reply(
            task_id,
            "callee",
            TaskReplyRequest(
                agent_id="callee",
                status="requires_input",
                result_text='{"hitl_kind":"tool_approval"}',
            ),
        )
        store.submit_caller_notify(task_id, "caller")
        task = store.submit_caller_resume(
            task_id,
            "caller",
            {"type": "selection", "approved": ["call-1"], "rejected": []},
        )
        self.assertIsNotNone(task)
        assert task is not None
        self.assertEqual(task.status, "caller_responded")
        resume, polled = store.poll_caller_input(task_id, "callee", wait_seconds=0)
        self.assertIsNotNone(resume)
        assert resume is not None
        self.assertEqual(resume["type"], "selection")
        assert polled is not None
        self.assertEqual(polled.status, "processing")

    def test_submit_caller_resume_rejects_invalid_state(self) -> None:
        store = _store()
        task_id = _create(store)
        self.assertIsNone(store.submit_caller_resume(task_id, "caller", {"type": "approve"}))


if __name__ == "__main__":
    unittest.main()
