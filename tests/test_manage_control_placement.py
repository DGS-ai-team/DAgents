from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.config import ManageSettings  # noqa: E402
from manage.manage_app import create_app  # noqa: E402


class ManageControlPlacementRetiredTests(unittest.TestCase):
    """远程 Placement control API 已拆除（原 410 tombstone 包已删除）。"""

    def test_control_routes_gone(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                peers = client.get("/v1/control/peers")
                create = client.post(
                    "/v1/control/nodes/home-01/agents",
                    json={"owner_node_id": "owner-01", "display_name": "x", "defaults": {}},
                )
                deleted = client.delete("/v1/control/nodes/home-01/agents/agt-1")
            self.assertEqual(peers.status_code, 404, peers.text)
            self.assertEqual(create.status_code, 404, create.text)
            self.assertEqual(deleted.status_code, 404, deleted.text)


if __name__ == "__main__":
    unittest.main()
