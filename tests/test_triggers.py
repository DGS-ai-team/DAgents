"""触发器资源、存储和调度器单测。"""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from app.context.models import OpenAIConversationContext
from app.harness.tools.triggers import trigger_create, trigger_get, trigger_list
from app.harness.triggers.models import TriggerCreateIn, TriggerUpdateIn, ensure_schedule_condition
from app.harness.triggers.runtime import reset_trigger_runtime, set_trigger_runtime
from app.harness.triggers.scheduler import TriggerScheduler
from app.harness.triggers.store import JsonTriggerStore


class FakeTriggerAgentService:
    def __init__(self) -> None:
        self.created_sessions: list[str | None] = []
        self.submitted_messages: list[dict[str, object]] = []

    async def create_session(self, session_id: str | None = None) -> str:
        self.created_sessions.append(session_id)
        return session_id or "generated-trigger-session"

    async def submit_message(self, **kwargs: object) -> None:
        self.submitted_messages.append(dict(kwargs))


class TriggerConditionValidationTests(unittest.TestCase):
    def test_empty_condition_is_rejected(self) -> None:
        with self.assertRaises(ValueError):
            ensure_schedule_condition({})

    def test_create_in_rejects_empty_condition(self) -> None:
        with self.assertRaises(ValueError):
            TriggerCreateIn(name="x", task_template="y", condition={})


class TriggerStoreTests(unittest.TestCase):
    def test_create_update_due_and_persist_trigger(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "triggers.json"
            store = JsonTriggerStore(path)
            trigger = TriggerCreateIn(
                name="heartbeat",
                condition={"interval_seconds": 10},
                task_template="check {trigger_name}",
            ).to_definition(now=100.0)

            created = store.create_trigger(trigger)
            self.assertTrue(created.enabled)
            self.assertEqual(created.next_fire_at, 110.0)
            self.assertEqual(store.due_triggers(109.0), [])
            self.assertEqual([item.trigger_id for item in store.due_triggers(110.0)], [created.trigger_id])

            updated = store.update_trigger(created.trigger_id, TriggerUpdateIn(enabled=False))
            self.assertFalse(updated.enabled)
            self.assertIsNone(updated.next_fire_at)

            reloaded = JsonTriggerStore(path)
            self.assertEqual(reloaded.get_trigger(created.trigger_id).name, "heartbeat")  # type: ignore[union-attr]


class TriggerToolTests(unittest.TestCase):
    def tearDown(self) -> None:
        reset_trigger_runtime()

    def test_trigger_tools_use_shared_runtime_store(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            store = JsonTriggerStore(Path(tmp) / "triggers.json")
            set_trigger_runtime(store=store, scheduler=None)

            created = json.loads(
                trigger_create(
                    name="daily-check",
                    task_template="check service",
                    condition={"interval_seconds": 3600},
                )
            )
            trigger_id = created["trigger"]["trigger_id"]
            listed = json.loads(trigger_list())
            fetched = json.loads(trigger_get(trigger_id))

            self.assertTrue(created["ok"])
            self.assertTrue(created["trigger"]["enabled"])
            self.assertEqual(listed["triggers"][0]["trigger_id"], trigger_id)
            self.assertEqual(fetched["trigger"]["name"], "daily-check")

    def test_trigger_create_binds_context_session_and_client(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            store = JsonTriggerStore(Path(tmp) / "triggers.json")
            set_trigger_runtime(store=store, scheduler=None)
            ctx = OpenAIConversationContext(
                session_id="sess-bind",
                sse_client_id="cli-bind",
            )

            created = json.loads(
                trigger_create(
                    name="ctx-bound",
                    task_template="run in session",
                    condition={"interval_seconds": 60},
                    context=ctx,
                )
            )

            trigger = created["trigger"]
            self.assertEqual(trigger["target_session_id"], "sess-bind")
            self.assertEqual(trigger["client_id"], "cli-bind")
            self.assertTrue(trigger["enabled"])


class TriggerSchedulerTests(unittest.IsolatedAsyncioTestCase):
    async def asyncTearDown(self) -> None:
        reset_trigger_runtime()

    async def test_fire_trigger_queues_message_into_agent_service(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            store = JsonTriggerStore(Path(tmp) / "triggers.json")
            service = FakeTriggerAgentService()
            scheduler = TriggerScheduler(store=store, service=service, poll_seconds=1)  # type: ignore[arg-type]
            trigger = store.create_trigger(
                TriggerCreateIn(
                    name="alert-diagnosis",
                    condition={"interval_seconds": 60},
                    task_template="diagnose {trigger_name} payload={payload_json}",
                ).to_definition(now=100.0)
            )
            trigger = store.update_trigger(trigger.trigger_id, TriggerUpdateIn(enabled=False))

            record = await scheduler.fire_trigger(
                trigger.trigger_id,
                reason="manual",
                payload={"service": "payment"},
                force=True,
            )

            self.assertEqual(record.status, "queued")
            self.assertEqual(record.session_id, "generated-trigger-session")
            self.assertEqual(service.created_sessions, [None])
            self.assertEqual(len(service.submitted_messages), 1)
            message = service.submitted_messages[0]
            self.assertEqual(message["session_id"], "generated-trigger-session")
            self.assertEqual(message["source"], f"trigger:{trigger.trigger_id}")
            self.assertIn('"service": "payment"', str(message["content"]))
            self.assertEqual(store.get_trigger(trigger.trigger_id).fire_count, 1)  # type: ignore[union-attr]

    async def test_scheduled_interval_trigger_queues_without_extra_policy(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            store = JsonTriggerStore(Path(tmp) / "triggers.json")
            service = FakeTriggerAgentService()
            scheduler = TriggerScheduler(store=store, service=service, poll_seconds=1)  # type: ignore[arg-type]
            trigger = store.create_trigger(
                TriggerCreateIn(
                    name="prod-restart",
                    condition={"interval_seconds": 1},
                    task_template="restart prod",
                ).to_definition(now=100.0)
            )

            record = await scheduler.fire_trigger(trigger.trigger_id, reason="schedule")

            self.assertEqual(record.status, "queued")
            self.assertEqual(len(service.submitted_messages), 1)


if __name__ == "__main__":
    unittest.main()
