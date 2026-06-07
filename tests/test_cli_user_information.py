"""CLI 用户询问事件解析单测。"""

from __future__ import annotations

import unittest

from app.cli.user_information import (
    build_answer_from_options,
    build_answer_from_text,
    extract_user_information_request,
)


class CliUserInformationTests(unittest.TestCase):
    def test_extract_user_information_request_from_sse_payload(self) -> None:
        request = extract_user_information_request(
            {
                "content": "请选择环境",
                "user_information_args": {
                    "tool_call_id": "call-1",
                    "question": "请选择环境",
                    "options": [{"id": "dev", "label": "开发", "value": "dev"}],
                    "allow_multiple": False,
                    "required": True,
                },
            }
        )
        assert request is not None
        self.assertEqual(request.tool_call_id, "call-1")
        self.assertEqual(len(request.options), 1)

    def test_build_answer_from_text(self) -> None:
        from app.cli.user_information import UserInformationRequest

        request = UserInformationRequest(
            tool_call_id="call-2",
            question="补充说明",
            options=[],
            allow_multiple=False,
            placeholder="",
            required=True,
        )
        answer = build_answer_from_text(request, "  hello  ")
        self.assertEqual(answer.answer, "hello")
        self.assertEqual(answer.to_resume_value()["type"], "user_information")

    def test_build_answer_from_options(self) -> None:
        from app.cli.user_information import UserInformationOption, UserInformationRequest

        request = UserInformationRequest(
            tool_call_id="call-3",
            question="选择",
            options=[
                UserInformationOption(id="a", label="选项A", value="a"),
                UserInformationOption(id="b", label="选项B", value="b"),
            ],
            allow_multiple=True,
            placeholder="",
            required=True,
        )
        answer = build_answer_from_options(request, ["a", "b"])
        self.assertEqual(answer.selected_options, ["a", "b"])
        self.assertIn("选项A", answer.answer)
