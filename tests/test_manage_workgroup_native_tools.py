"""Manage-native workgroup tool schema is loaded from the runtime package."""

from __future__ import annotations

import json
import unittest
from pathlib import Path

from manage.workgroup.native_tools import load_assign_workgroup_task_tool


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


if __name__ == "__main__":
    unittest.main()
