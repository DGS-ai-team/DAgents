"""触发器资源、存储和调度器单测。"""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from app.harness.triggers.models import TriggerCreateIn, TriggerUpdateIn
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


class TriggerStoreTests(unittest.TestCase):
    def test_create_update_due_and_persist_trigger(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "triggers.json"
            store = JsonTriggerStore(path)
            trigger = TriggerCreateIn(
                name="heartbeat",
                source_type="interval",
                condition={"interval_seconds": 10},
                task_template="check {trigger_name}",
                enabled=True,
            ).to_definition(now=100.0)

            created = store.create_trigger(trigger)
            self.assertEqual(created.next_fire_at, 110.0)
            self.assertEqual(store.due_triggers(109.0), [])
            self.assertEqual([item.trigger_id for item in store.due_triggers(110.0)], [created.trigger_id])

            updated = store.update_trigger(created.trigger_id, TriggerUpdateIn(enabled=False))
            self.assertFalse(updated.enabled)
            self.assertIsNone(updated.next_fire_at)

            reloaded = JsonTriggerStore(path)
            self.assertEqual(reloaded.get_trigger(created.trigger_id).name, "heartbeat")  # type: ignore[union-attr]


class TriggerSchedulerTests(unittest.IsolatedAsyncioTestCase):
    async def test_fire_trigger_queues_message_into_agent_service(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            store = JsonTriggerStore(Path(tmp) / "triggers.json")
            service = FakeTriggerAgentService()
            scheduler = TriggerScheduler(store=store, service=service, poll_seconds=1)  # type: ignore[arg-type]
            trigger = store.create_trigger(
                TriggerCreateIn(
                    name="alert-diagnosis",
                    source_type="manual",
                    task_template="diagnose {trigger_name} payload={payload_json}",
                    enabled=False,
                ).to_definition(now=100.0)
            )

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

    async def test_high_risk_scheduled_trigger_is_skipped_without_auto_fire_policy(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            store = JsonTriggerStore(Path(tmp) / "triggers.json")
            service = FakeTriggerAgentService()
            scheduler = TriggerScheduler(store=store, service=service, poll_seconds=1)  # type: ignore[arg-type]
            trigger = store.create_trigger(
                TriggerCreateIn(
                    name="prod-restart",
                    source_type="interval",
                    condition={"interval_seconds": 1},
                    task_template="restart prod",
                    risk_level="high",
                    enabled=True,
                ).to_definition(now=100.0)
            )

            record = await scheduler.fire_trigger(trigger.trigger_id, reason="schedule")

            self.assertEqual(record.status, "skipped")
            self.assertEqual(service.submitted_messages, [])
            self.assertIn("auto_fire_allowed", record.message)


if __name__ == "__main__":
    unittest.main()
