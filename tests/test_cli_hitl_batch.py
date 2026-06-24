import unittest

from app.cli.hitl_batch import expand_hitl_required


class HitlBatchTests(unittest.TestCase):
    def test_expand_mixed_batch(self) -> None:
        data = {
            "hitl_id": "hitl-1",
            "message": "mixed",
            "items": [
                {
                    "hitl_type": "user_information",
                    "content": "Pick one?",
                    "user_information_args": {
                        "tool_call_id": "call-ask-1",
                        "question": "Pick one?",
                    },
                },
                {
                    "hitl_type": "execute_tool",
                    "id": "call-bash-1",
                    "name": "bash_run",
                    "arguments": {"command": "echo ok"},
                },
            ],
        }
        user_infos, approval = expand_hitl_required(data)
        self.assertEqual(len(user_infos), 1)
        self.assertIsNotNone(approval)
        assert approval is not None
        self.assertEqual(approval.get("approval_id"), "hitl-1")
        calls = approval.get("approval_args", {}).get("tool_calls", [])
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0].get("id"), "call-bash-1")

    def test_expand_preserves_child_session_id(self) -> None:
        data = {
            "hitl_id": "hitl-child-1",
            "child_session_id": "child-abc",
            "hitl_scope": "temporary_agent",
            "child_purpose": "research",
            "items": [
                {
                    "hitl_type": "execute_tool",
                    "id": "call-bash-1",
                    "name": "bash_run",
                    "arguments": {"command": "echo ok"},
                },
            ],
        }
        _user_infos, approval = expand_hitl_required(data)
        self.assertIsNotNone(approval)
        assert approval is not None
        self.assertEqual(approval.get("child_session_id"), "child-abc")
        self.assertEqual(approval.get("hitl_scope"), "temporary_agent")


if __name__ == "__main__":
    unittest.main()
