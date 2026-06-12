from __future__ import annotations

import json
import os
import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.config import ManageSettings  # noqa: E402
from manage.manage_app import create_app  # noqa: E402


def _register_agent(client: TestClient, agent_id: str, *, expose: bool = True) -> None:
    client.post(
        "/v1/registry/agents",
        json={"agent_id": agent_id, "base_url": f"http://{agent_id}.local", "expose_to_peers": expose},
        headers={"x-dagents-agent-id": agent_id},
    )
    client.post(
        f"/v1/registry/agents/{agent_id}/heartbeat",
        json={"ttl_seconds": 120},
        headers={"x-dagents-agent-id": agent_id},
    )


def _assign_groups(client: TestClient, agent_id: str, groups: list[str] | None = None) -> None:
    payload = {"discovery_group": groups or ["default-lab"]}
    resp = client.patch(
        f"/v1/registry/agents/{agent_id}/groups",
        json=payload,
    )
    assert resp.status_code == 200, resp.text


class ManageA2ATests(unittest.TestCase):
    def test_task_create_inbox_reply_flow(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings(
                host="127.0.0.1",
                port=8020,
                db_path=Path(tmp) / "manage.db",
                blob_dir=None,
                blob_max_bytes=None,
                offline_grace_seconds=86400,
                audit_max_entries=100,
                legacy_direct_relay=False,
                a2a_inbox_content_max_chars=4096,
                a2a_expire_sweep_seconds=0,
            )
            app = create_app(settings)
            with TestClient(app) as client:
                _register_agent(client, "caller-01")
                _register_agent(client, "callee-01")
                _assign_groups(client, "caller-01")
                _assign_groups(client, "callee-01")

                created = client.post(
                    "/v1/a2a/tasks",
                    json={
                        "from_agent_id": "caller-01",
                        "to_agent_id": "callee-01",
                        "kind": "invoke",
                        "content": "ping",
                        "idempotency_key": "k1",
                    },
                    headers={"x-dagents-agent-id": "caller-01"},
                )
                self.assertEqual(created.status_code, 200)
                task_id = created.json()["task_id"]
                self.assertEqual(created.json()["status"], "queued")

                dup = client.post(
                    "/v1/a2a/tasks",
                    json={
                        "from_agent_id": "caller-01",
                        "to_agent_id": "callee-01",
                        "content": "ignored",
                        "idempotency_key": "k1",
                    },
                    headers={"x-dagents-agent-id": "caller-01"},
                )
                self.assertEqual(dup.status_code, 200)
                self.assertEqual(dup.json()["task_id"], task_id)

                inbox = client.get(
                    "/v1/a2a/inbox",
                    params={"agent_id": "callee-01", "limit": 5},
                    headers={"x-dagents-agent-id": "callee-01"},
                )
                self.assertEqual(inbox.status_code, 200)
                body = inbox.json()
                self.assertEqual(len(body["tasks"]), 1)
                self.assertEqual(body["tasks"][0]["task_id"], task_id)
                self.assertEqual(body["tasks"][0]["content"], "ping")

                ack = client.post(
                    f"/v1/a2a/tasks/{task_id}/ack",
                    json={"agent_id": "callee-01"},
                    headers={"x-dagents-agent-id": "callee-01"},
                )
                self.assertEqual(ack.status_code, 200)
                self.assertEqual(ack.json()["task"]["status"], "processing")

                reply = client.post(
                    f"/v1/a2a/tasks/{task_id}/reply",
                    json={
                        "agent_id": "callee-01",
                        "status": "completed",
                        "result_text": "pong",
                        "callee_session_id": "sess-b",
                    },
                    headers={"x-dagents-agent-id": "callee-01"},
                )
                self.assertEqual(reply.status_code, 200)
                self.assertEqual(reply.json()["status"], "completed")

                got = client.get(
                    f"/v1/a2a/tasks/{task_id}",
                    params={"caller_agent_id": "caller-01"},
                    headers={"x-dagents-agent-id": "caller-01"},
                )
                self.assertEqual(got.status_code, 200)
                self.assertEqual(got.json()["task"]["status"], "completed")
                self.assertEqual(got.json()["task"]["result_text"], "pong")

            app2 = create_app(settings)
            with TestClient(app2) as client:
                reloaded = client.get(
                    f"/v1/a2a/tasks/{task_id}",
                    params={"caller_agent_id": "caller-01"},
                    headers={"x-dagents-agent-id": "caller-01"},
                )
            self.assertEqual(reloaded.status_code, 200)
            self.assertEqual(reloaded.json()["task"]["result_text"], "pong")

    def test_task_awaiting_caller_resume_flow(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings(
                host="127.0.0.1",
                port=8020,
                db_path=Path(tmp) / "manage.db",
                blob_dir=None,
                blob_max_bytes=None,
                offline_grace_seconds=86400,
                audit_max_entries=100,
                legacy_direct_relay=False,
                a2a_inbox_content_max_chars=4096,
                a2a_expire_sweep_seconds=0,
            )
            app = create_app(settings)
            with TestClient(app) as client:
                _register_agent(client, "caller-01")
                _register_agent(client, "callee-01")
                _assign_groups(client, "caller-01")
                _assign_groups(client, "callee-01")
                created = client.post(
                    "/v1/a2a/tasks",
                    json={
                        "from_agent_id": "caller-01",
                        "to_agent_id": "callee-01",
                        "kind": "invoke",
                        "content": "need approval",
                        "caller_session_id": "sess-caller",
                    },
                    headers={"x-dagents-agent-id": "caller-01"},
                )
                task_id = created.json()["task_id"]
                client.post(
                    f"/v1/a2a/tasks/{task_id}/ack",
                    json={"agent_id": "callee-01"},
                    headers={"x-dagents-agent-id": "callee-01"},
                )
                hitl_payload = json.dumps(
                    {
                        "hitl_kind": "tool_approval",
                        "task_id": task_id,
                        "callee_session_id": "a2a-task",
                        "caller_session_id": "sess-caller",
                        "event_type": "approval_required",
                        "event_data": {"approval_id": "appr-1"},
                    }
                )
                waiting = client.post(
                    f"/v1/a2a/tasks/{task_id}/reply",
                    json={
                        "agent_id": "callee-01",
                        "status": "requires_input",
                        "result_text": hitl_payload,
                        "callee_session_id": "a2a-task",
                    },
                    headers={"x-dagents-agent-id": "callee-01"},
                )
                self.assertEqual(waiting.status_code, 200)
                self.assertEqual(waiting.json()["status"], "awaiting_caller")

                got = client.get(
                    f"/v1/a2a/tasks/{task_id}",
                    params={"caller_agent_id": "caller-01"},
                    headers={"x-dagents-agent-id": "caller-01"},
                )
                self.assertEqual(got.json()["task"]["status"], "awaiting_caller")

                resume = client.post(
                    f"/v1/a2a/tasks/{task_id}/caller_resume",
                    json={
                        "caller_agent_id": "caller-01",
                        "resume_value": {"type": "approval", "tool_call_id": "call-1", "decisions": []},
                    },
                    headers={"x-dagents-agent-id": "caller-01"},
                )
                self.assertEqual(resume.status_code, 200)

                polled = client.get(
                    f"/v1/a2a/tasks/{task_id}/caller_input",
                    params={"agent_id": "callee-01"},
                    headers={"x-dagents-agent-id": "callee-01"},
                )
                self.assertTrue(polled.json()["ready"])
                self.assertEqual(polled.json()["resume_value"]["type"], "approval")

                done = client.post(
                    f"/v1/a2a/tasks/{task_id}/reply",
                    json={
                        "agent_id": "callee-01",
                        "status": "completed",
                        "result_text": "APPROVED after hitl",
                        "callee_session_id": "a2a-task",
                    },
                    headers={"x-dagents-agent-id": "callee-01"},
                )
                self.assertEqual(done.json()["status"], "completed")

    def test_create_rejects_discovery_group_mismatch(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            _register_agent(client, "caller-01")
            _register_agent(client, "callee-01")
            _assign_groups(client, "caller-01", ["ops"])
            _assign_groups(client, "callee-01", ["staging"])
            resp = client.post(
                "/v1/a2a/tasks",
                json={"from_agent_id": "caller-01", "to_agent_id": "callee-01", "content": "x"},
                headers={"x-dagents-agent-id": "caller-01"},
            )
        self.assertEqual(resp.status_code, 403)
        self.assertEqual(resp.json()["detail"], "discovery_group_mismatch")

    def test_create_rejects_empty_discovery_group(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            _register_agent(client, "caller-01")
            _register_agent(client, "callee-01")
            resp = client.post(
                "/v1/a2a/tasks",
                json={"from_agent_id": "caller-01", "to_agent_id": "callee-01", "content": "x"},
                headers={"x-dagents-agent-id": "caller-01"},
            )
        self.assertEqual(resp.status_code, 403)
        self.assertEqual(resp.json()["detail"], "caller_discovery_group_empty")

    def test_create_rejects_hidden_target(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            _register_agent(client, "caller-01")
            _register_agent(client, "hidden-01", expose=False)
            resp = client.post(
                "/v1/a2a/tasks",
                json={"from_agent_id": "caller-01", "to_agent_id": "hidden-01", "content": "x"},
                headers={"x-dagents-agent-id": "caller-01"},
            )
        self.assertEqual(resp.status_code, 403)
        self.assertEqual(resp.json()["detail"], "target_not_exposed")

    def test_inbox_long_poll_returns_empty(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            _register_agent(client, "solo-01")
            resp = client.get(
                "/v1/a2a/inbox",
                params={"agent_id": "solo-01", "wait": 0.1},
                headers={"x-dagents-agent-id": "solo-01"},
            )
        self.assertEqual(resp.status_code, 200)
        self.assertEqual(resp.json()["tasks"], [])

    def test_metrics_include_a2a_counter(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            metrics = client.get("/metrics")
        self.assertEqual(metrics.status_code, 200)
        self.assertIn("dagents_manage_a2a_operations_total", metrics.text)

    def test_create_rejects_unknown_target(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            _register_agent(client, "caller-01")
            resp = client.post(
                "/v1/a2a/tasks",
                json={"from_agent_id": "caller-01", "to_agent_id": "missing-01", "content": "x"},
                headers={"x-dagents-agent-id": "caller-01"},
            )
        self.assertEqual(resp.status_code, 404)
        self.assertEqual(resp.json()["detail"], "target_not_found")

    def test_create_rejects_agent_id_header_mismatch(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            _register_agent(client, "caller-01")
            _register_agent(client, "callee-01")
            resp = client.post(
                "/v1/a2a/tasks",
                json={"from_agent_id": "caller-01", "to_agent_id": "callee-01", "content": "x"},
                headers={"x-dagents-agent-id": "other-01"},
            )
        self.assertEqual(resp.status_code, 403)

    def test_get_task_forbidden_for_unrelated_agent(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            _register_agent(client, "caller-01")
            _register_agent(client, "callee-01")
            _register_agent(client, "stranger-01")
            _assign_groups(client, "caller-01")
            _assign_groups(client, "callee-01")
            created = client.post(
                "/v1/a2a/tasks",
                json={"from_agent_id": "caller-01", "to_agent_id": "callee-01", "content": "x"},
                headers={"x-dagents-agent-id": "caller-01"},
            )
            task_id = created.json()["task_id"]
            resp = client.get(
                f"/v1/a2a/tasks/{task_id}",
                params={"caller_agent_id": "stranger-01"},
                headers={"x-dagents-agent-id": "stranger-01"},
            )
        self.assertEqual(resp.status_code, 403)

    def test_callee_can_read_task(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            _register_agent(client, "caller-01")
            _register_agent(client, "callee-01")
            _assign_groups(client, "caller-01")
            _assign_groups(client, "callee-01")
            created = client.post(
                "/v1/a2a/tasks",
                json={"from_agent_id": "caller-01", "to_agent_id": "callee-01", "content": "x"},
                headers={"x-dagents-agent-id": "caller-01"},
            )
            task_id = created.json()["task_id"]
            resp = client.get(
                f"/v1/a2a/tasks/{task_id}",
                params={"caller_agent_id": "callee-01"},
                headers={"x-dagents-agent-id": "callee-01"},
            )
        self.assertEqual(resp.status_code, 200)
        self.assertEqual(resp.json()["task"]["to_agent_id"], "callee-01")

    def test_reply_failed_via_http(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            _register_agent(client, "caller-01")
            _register_agent(client, "callee-01")
            _assign_groups(client, "caller-01")
            _assign_groups(client, "callee-01")
            created = client.post(
                "/v1/a2a/tasks",
                json={"from_agent_id": "caller-01", "to_agent_id": "callee-01", "content": "x"},
                headers={"x-dagents-agent-id": "caller-01"},
            )
            task_id = created.json()["task_id"]
            client.get(
                "/v1/a2a/inbox",
                params={"agent_id": "callee-01"},
                headers={"x-dagents-agent-id": "callee-01"},
            )
            reply = client.post(
                f"/v1/a2a/tasks/{task_id}/reply",
                json={"agent_id": "callee-01", "status": "failed", "error_detail": "nope"},
                headers={"x-dagents-agent-id": "callee-01"},
            )
            got = client.get(
                f"/v1/a2a/tasks/{task_id}",
                params={"caller_agent_id": "caller-01"},
                headers={"x-dagents-agent-id": "caller-01"},
            )
        self.assertEqual(reply.status_code, 200)
        self.assertEqual(reply.json()["status"], "failed")
        self.assertEqual(got.json()["task"]["status"], "failed")
        self.assertEqual(got.json()["task"]["error_detail"], "nope")

    def test_ack_not_found_for_wrong_agent(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            _register_agent(client, "caller-01")
            _register_agent(client, "callee-01")
            _assign_groups(client, "caller-01")
            _assign_groups(client, "callee-01")
            created = client.post(
                "/v1/a2a/tasks",
                json={"from_agent_id": "caller-01", "to_agent_id": "callee-01", "content": "x"},
                headers={"x-dagents-agent-id": "caller-01"},
            )
            task_id = created.json()["task_id"]
            resp = client.post(
                f"/v1/a2a/tasks/{task_id}/ack",
                json={"agent_id": "caller-01"},
                headers={"x-dagents-agent-id": "caller-01"},
            )
        self.assertEqual(resp.status_code, 404)

    def test_register_persists_agent_card(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            client.post(
                "/v1/registry/agents",
                json={
                    "agent_id": "card-agent",
                    "base_url": "http://card.local",
                    "expose_to_peers": True,
                    "card": {"name": "合规助手", "metadata": {"role": "compliance"}},
                },
                headers={"x-dagents-agent-id": "card-agent"},
            )
            client.patch(
                "/v1/registry/agents/card-agent/groups",
                json={"discovery_group": ["lab"]},
            )
            discover = client.get("/v1/registry/agents/discover", params={"discovery_group": "lab"})
        match = next(a for a in discover.json()["agents"] if a["agent_id"] == "card-agent")
        self.assertEqual(match["card"]["name"], "合规助手")
        self.assertEqual(match["card"]["metadata"]["role"], "compliance")


class ManageA2AInboxEfficiencyTests(unittest.TestCase):
    def test_inbox_truncates_large_content(self) -> None:
        settings = ManageSettings(
            host="127.0.0.1",
            port=8020,
            db_path=None,
            blob_dir=None,
            blob_max_bytes=None,
            offline_grace_seconds=86400,
            audit_max_entries=100,
            legacy_direct_relay=False,
            a2a_inbox_content_max_chars=16,
            a2a_expire_sweep_seconds=0,
        )
        app = create_app(settings)
        with TestClient(app) as client:
            _register_agent(client, "caller-01")
            _register_agent(client, "callee-01")
            _assign_groups(client, "caller-01")
            _assign_groups(client, "callee-01")
            client.post(
                "/v1/a2a/tasks",
                json={
                    "from_agent_id": "caller-01",
                    "to_agent_id": "callee-01",
                    "content": "x" * 40,
                },
                headers={"x-dagents-agent-id": "caller-01"},
            )
            inbox = client.get(
                "/v1/a2a/inbox",
                params={"agent_id": "callee-01"},
                headers={"x-dagents-agent-id": "callee-01"},
            )
        self.assertEqual(inbox.status_code, 200)
        task = inbox.json()["tasks"][0]
        self.assertTrue(task["content_truncated"])
        self.assertLessEqual(len(task["content"]), 17)


if __name__ == "__main__":
    unittest.main()
