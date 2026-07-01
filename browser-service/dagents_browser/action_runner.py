from __future__ import annotations

from typing import Any

from browser_use.agent.views import ActionResult
from browser_use.filesystem.file_system import FileSystem
from browser_use.llm.base import BaseChatModel
from browser_use.tools.service import Tools


class ActionRunner:
    """通过 browser-use Tools registry 执行内置 action。"""

    def __init__(self, fs_root: str, extraction_llm: BaseChatModel | None = None) -> None:
        self._tools = Tools()
        self._fs_root = fs_root
        self._extraction_llm = extraction_llm
        self._file_system = FileSystem(fs_root, create_default_files=False)

    @property
    def extraction_llm(self) -> BaseChatModel | None:
        return self._extraction_llm

    async def run(
        self,
        action: str,
        params: dict[str, Any],
        *,
        session: Any,
        available_file_paths: list[str] | None = None,
    ) -> dict[str, Any]:
        try:
            result = await self._tools.registry.execute_action(
                action,
                params,
                browser_session=session,
                page_extraction_llm=self._extraction_llm,
                file_system=self._file_system,
                available_file_paths=available_file_paths or [],
            )
        except Exception as exc:
            return {"ok": False, "error": str(exc)}
        return action_result_to_response(result)


def action_result_to_response(result: ActionResult | Any) -> dict[str, Any]:
    if result is None:
        return {"ok": False, "error": "action returned no result"}
    error = getattr(result, "error", None)
    if error:
        return {"ok": False, "error": str(error)}
    detail: dict[str, Any] = {}
    extracted = getattr(result, "extracted_content", None)
    if extracted:
        detail["extracted_content"] = extracted
    memory = getattr(result, "long_term_memory", None)
    if memory:
        detail["long_term_memory"] = memory
    metadata = getattr(result, "metadata", None)
    if metadata:
        detail["metadata"] = metadata
    return {"ok": True, "detail": detail}
