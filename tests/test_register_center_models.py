from __future__ import annotations

import sys
import unittest
from pathlib import Path

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT / "register_center"))

from rc_models import AgentUpsertRequest  # noqa: E402


class RegisterCenterModelsTestCase(unittest.TestCase):
    def test_discovery_group_accepts_string_or_list(self) -> None:
        req_single = AgentUpsertRequest(
            agent_id="a1",
            base_url="http://localhost:8000/",
            discovery_group="team-a",
        )
        self.assertEqual(req_single.discovery_group, ["team-a"])

        req_list = AgentUpsertRequest(
            agent_id="a2",
            base_url="http://localhost:8001/",
            discovery_group=["team-a", " team-b ", "team-a"],
        )
        self.assertEqual(req_list.discovery_group, ["team-a", "team-b"])

    def test_capabilities_hint_normalize(self) -> None:
        req = AgentUpsertRequest(
            agent_id="a3",
            base_url="http://localhost:8002/",
            discovery_group=["team-a"],
            capabilities_hint=["code", " code ", "review", ""],
        )
        self.assertEqual(req.capabilities_hint, ["code", "review"])


if __name__ == "__main__":
    unittest.main()
