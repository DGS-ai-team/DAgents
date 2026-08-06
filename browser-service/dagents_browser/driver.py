from __future__ import annotations

import asyncio
import json
import re
import time
import uuid
from pathlib import Path
from typing import Any

from browser_use import BrowserProfile, BrowserSession
from browser_use.browser.events import ClickCoordinateEvent, ClickElementEvent, NavigateToUrlEvent, SendKeysEvent, TypeTextEvent

from dagents_browser.action_runner import ActionRunner
from dagents_browser.config import BrowserServiceSettings
from dagents_browser.llm import create_extraction_llm
from dagents_browser.ports import allocate_debug_port

DEFAULT_SNAPSHOT_MAX = 150
MAX_SNAPSHOT_MAX = 500
DEFAULT_LLM_REPRESENTATION_MAX = 40000
DEFAULT_TASK_MAX_STEPS = 50


def normalize_max_elements(n: int) -> int:
    if n <= 0:
        return DEFAULT_SNAPSHOT_MAX
    return min(n, MAX_SNAPSHOT_MAX)


def sanitize_segment(raw: str) -> str:
    raw = (raw or "").strip()
    if not raw:
        return "default"
    out = re.sub(r"[^a-zA-Z0-9_-]+", "-", raw).strip("-")
    return out or "default"


def element_from_node(index: int, node: Any, max_text: int = 120) -> dict[str, Any]:
    attrs = node.attributes or {}
    text = (node.get_all_children_text(max_depth=2) or "").strip()
    if len(text) > max_text:
        text = text[: max_text - 3] + "..."
    return {
        "index": index,
        "tag": node.tag_name,
        "role": attrs.get("role") or node.tag_name,
        "text": text,
    }


class BrowserUseDriver:
    """browser-use 驱动：细粒度 op + 任务级 run_task（Agent 闭环）。"""

    def __init__(self, settings: BrowserServiceSettings) -> None:
        self.settings = settings
        self._sessions: dict[str, BrowserSession] = {}
        self._session_ports: dict[str, int] = {}
        self._session_locks: dict[str, asyncio.Lock] = {}
        self._lock = asyncio.Lock()
        self._tasks: dict[str, dict[str, Any]] = {}
        self._session_latest_task: dict[str, str] = {}
        extraction_llm = None
        if settings.llm is not None:
            try:
                extraction_llm = create_extraction_llm(settings.llm)
            except Exception:
                extraction_llm = None
        self._actions = ActionRunner(settings.fs_root, extraction_llm)

    def _session_lock(self, session_key: str) -> asyncio.Lock:
        lock = self._session_locks.get(session_key)
        if lock is None:
            lock = asyncio.Lock()
            self._session_locks[session_key] = lock
        return lock

    def _allocate_debug_port(self, session_key: str) -> int:
        """每 session 独立 remote-debugging-port；attach(cdp_url) 模式沿用配置端口。"""
        return allocate_debug_port(
            session_key,
            base_port=int(self.settings.debug_port or 9222),
            used_ports=set(self._session_ports.values()),
            cdp_url=self.settings.cdp_url or "",
        )

    async def close(self) -> None:
        for task in list(self._tasks.values()):
            at = task.get("asyncio_task")
            if at is not None and not at.done():
                at.cancel()
        keys = list(self._sessions.keys())
        for key in keys:
            await self._stop_session(key)

    async def call(self, req: dict[str, Any]) -> dict[str, Any]:
        op = str(req.get("op") or "").strip()
        if op == "ping":
            return {"ok": True, "detail": {"driver": "browser-use-v2"}}
        if op == "start":
            return await self._start(req)
        if op == "stop":
            return await self._stop(req)
        if op == "run_task":
            return await self._run_task(req)
        if op == "task_status":
            return await self._task_status(req)
        if op == "task_cancel":
            return await self._task_cancel(req)
        session_key = str(req.get("session_key") or "").strip()
        if not session_key:
            return {"ok": False, "error": "session_key is required"}
        session = self._sessions.get(session_key)
        if session is None:
            return {"ok": False, "error": "browser session not started"}
        lock = self._session_lock(session_key)
        async with lock:
            if op == "navigate":
                return await self._navigate(session, req)
            if op == "click":
                return await self._click(session, req)
            if op == "click_coordinate":
                return await self._click_coordinate(session, req)
            if op == "fill":
                return await self._fill(session, req)
            if op == "press":
                return await self._press(session, req)
            if op == "screenshot":
                return await self._screenshot(session, req)
            if op == "wait":
                return await self._wait(session, req)
            if op == "snapshot":
                return await self._snapshot(session, req)
            if op == "search":
                return await self._search(session, req)
            if op == "go_back":
                return await self._go_back(session)
            if op == "scroll":
                return await self._scroll(session, req)
            if op == "find_text":
                return await self._find_text(session, req)
            if op == "switch_tab":
                return await self._switch_tab(session, req)
            if op == "close_tab":
                return await self._close_tab(session, req)
            if op == "extract":
                return await self._extract(session, req)
            if op == "evaluate":
                return await self._evaluate(session, req)
            if op == "find_elements":
                return await self._find_elements(session, req)
            if op == "search_page":
                return await self._search_page(session, req)
            if op == "upload_file":
                return await self._upload_file(session, req)
            if op == "dropdown_options":
                return await self._dropdown_options(session, req)
            if op == "select_dropdown":
                return await self._select_dropdown(session, req)
        return {"ok": False, "error": f"unknown op: {op}"}

    async def _start(self, req: dict[str, Any]) -> dict[str, Any]:
        session_key = str(req.get("session_key") or "").strip()
        if not session_key:
            return {"ok": False, "error": "session_key is required"}
        async with self._lock:
            if session_key in self._sessions:
                return await self._page_info(self._sessions[session_key], {"already_started": True})
            if len(self._sessions) >= self.settings.max_sessions:
                return {
                    "ok": False,
                    "error": f"browser session limit reached (max {self.settings.max_sessions})",
                }
            try:
                debug_port = self._allocate_debug_port(session_key)
            except RuntimeError as exc:
                return {"ok": False, "error": str(exc)}
        headed = self.settings.headed
        if req.get("headed") is not None:
            headed = bool(req["headed"])
        profile_dir = (
            Path(self.settings.fs_root) / "browser" / "profiles" / sanitize_segment(session_key)
        )
        profile_dir.mkdir(parents=True, exist_ok=True)
        args = [
            "--remote-allow-origins=*",
            "--remote-debugging-address=127.0.0.1",
            f"--remote-debugging-port={debug_port}",
        ]
        if self.settings.ignore_https_errors:
            args.append("--ignore-certificate-errors")
        profile = BrowserProfile(
            headless=not headed,
            user_data_dir=str(profile_dir),
            executable_path=self.settings.chrome_path or None,
            cdp_url=self.settings.cdp_url or None,
            args=args,
            keep_alive=True,
        )
        session = BrowserSession(browser_profile=profile, id=session_key)
        await session.start()
        async with self._lock:
            self._sessions[session_key] = session
            self._session_ports[session_key] = debug_port
        detail = {
            "mode": {
                "attach": bool(self.settings.cdp_url),
                "headed": headed,
                "engine": "browser-use",
                "interaction": "index",
                "debug_port": debug_port,
            }
        }
        return await self._page_info(session, detail)

    async def _stop(self, req: dict[str, Any]) -> dict[str, Any]:
        session_key = str(req.get("session_key") or "").strip()
        await self._cancel_session_tasks(session_key)
        await self._stop_session(session_key)
        return {"ok": True}

    async def _stop_session(self, session_key: str) -> None:
        async with self._lock:
            session = self._sessions.pop(session_key, None)
            self._session_ports.pop(session_key, None)
        if session is None:
            return
        if self.settings.cdp_url:
            await session.stop()
        else:
            await session.kill()

    async def _run_task(self, req: dict[str, Any]) -> dict[str, Any]:
        session_key = str(req.get("session_key") or "").strip()
        task_text = str(req.get("task") or "").strip()
        if not session_key:
            return {"ok": False, "error": "session_key is required"}
        if not task_text:
            return {"ok": False, "error": "task is required"}
        if session_key not in self._sessions:
            started = await self._start({"session_key": session_key, "headed": req.get("headed")})
            if not started.get("ok"):
                return started
        if self.settings.llm is None:
            return {
                "ok": False,
                "error": "browser run_task requires a non-mock llm in node config (browser-use Agent)",
            }
        try:
            llm = create_extraction_llm(self.settings.llm)
        except Exception as exc:
            return {"ok": False, "error": f"browser llm init failed: {exc}"}

        max_steps = int(req.get("max_steps") or 0)
        if max_steps <= 0:
            max_steps = DEFAULT_TASK_MAX_STEPS
        max_steps = min(max_steps, 200)

        task_id = f"btask-{uuid.uuid4().hex[:12]}"
        entry: dict[str, Any] = {
            "task_id": task_id,
            "session_key": session_key,
            "task": task_text,
            "status": "queued",
            "max_steps": max_steps,
            "created_at": time.time(),
            "updated_at": time.time(),
            "result": None,
            "error": None,
            "asyncio_task": None,
        }
        self._tasks[task_id] = entry
        self._session_latest_task[session_key] = task_id

        async def _worker() -> None:
            lock = self._session_lock(session_key)
            entry["status"] = "running"
            entry["updated_at"] = time.time()
            try:
                async with lock:
                    session = self._sessions.get(session_key)
                    if session is None:
                        raise RuntimeError("browser session not started")
                    from browser_use import Agent

                    agent = Agent(
                        task=task_text,
                        llm=llm,
                        browser_session=session,
                    )
                    history = await agent.run(max_steps=max_steps)
                    final = None
                    success = None
                    steps = None
                    try:
                        final = history.final_result() if history is not None else None
                    except Exception:
                        final = str(history) if history is not None else None
                    try:
                        success = history.is_successful() if history is not None else None
                    except Exception:
                        success = None
                    try:
                        steps = history.number_of_steps() if history is not None else None
                    except Exception:
                        steps = None
                    entry["status"] = "completed"
                    entry["result"] = {
                        "final_result": final,
                        "success": success,
                        "steps": steps,
                    }
            except asyncio.CancelledError:
                entry["status"] = "cancelled"
                entry["error"] = "cancelled"
                raise
            except Exception as exc:
                entry["status"] = "failed"
                entry["error"] = str(exc)
            finally:
                entry["updated_at"] = time.time()
                entry["asyncio_task"] = None

        at = asyncio.create_task(_worker())
        entry["asyncio_task"] = at
        return {
            "ok": True,
            "detail": {
                "task_id": task_id,
                "status": "queued",
                "session_key": session_key,
                "max_steps": max_steps,
            },
        }

    def _task_public(self, entry: dict[str, Any]) -> dict[str, Any]:
        return {
            "task_id": entry.get("task_id"),
            "session_key": entry.get("session_key"),
            "task": entry.get("task"),
            "status": entry.get("status"),
            "max_steps": entry.get("max_steps"),
            "created_at": entry.get("created_at"),
            "updated_at": entry.get("updated_at"),
            "result": entry.get("result"),
            "error": entry.get("error"),
        }

    async def _task_status(self, req: dict[str, Any]) -> dict[str, Any]:
        session_key = str(req.get("session_key") or "").strip()
        task_id = str(req.get("task_id") or "").strip()
        if not session_key:
            return {"ok": False, "error": "session_key is required"}
        if not task_id:
            task_id = self._session_latest_task.get(session_key, "")
        if not task_id:
            return {"ok": False, "error": "no task for session"}
        entry = self._tasks.get(task_id)
        if entry is None or entry.get("session_key") != session_key:
            return {"ok": False, "error": f"task not found: {task_id}"}
        return {"ok": True, "detail": self._task_public(entry)}

    async def _task_cancel(self, req: dict[str, Any]) -> dict[str, Any]:
        session_key = str(req.get("session_key") or "").strip()
        task_id = str(req.get("task_id") or "").strip()
        if not session_key:
            return {"ok": False, "error": "session_key is required"}
        if not task_id:
            task_id = self._session_latest_task.get(session_key, "")
        if not task_id:
            return {"ok": False, "error": "no task for session"}
        entry = self._tasks.get(task_id)
        if entry is None or entry.get("session_key") != session_key:
            return {"ok": False, "error": f"task not found: {task_id}"}
        at = entry.get("asyncio_task")
        if at is not None and not at.done():
            at.cancel()
            try:
                await at
            except asyncio.CancelledError:
                pass
            except Exception:
                pass
        elif entry.get("status") in ("queued", "running"):
            entry["status"] = "cancelled"
            entry["error"] = "cancelled"
            entry["updated_at"] = time.time()
        return {"ok": True, "detail": self._task_public(entry)}

    async def _cancel_session_tasks(self, session_key: str) -> None:
        for entry in list(self._tasks.values()):
            if entry.get("session_key") != session_key:
                continue
            at = entry.get("asyncio_task")
            if at is not None and not at.done():
                at.cancel()
                try:
                    await at
                except Exception:
                    pass

    async def _navigate(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        url = str(req.get("url") or "").strip()
        if not url:
            return {"ok": False, "error": "url is required"}
        event = session.event_bus.dispatch(NavigateToUrlEvent(url=url))
        await event
        wait_until = str(req.get("wait_until") or "load").strip()
        await self._wait_load(session, wait_until)
        return await self._page_info(session)

    async def _wait_load(self, session: BrowserSession, wait_until: str) -> None:
        if wait_until == "networkidle":
            await asyncio.sleep(2.0)
            return
        await asyncio.sleep(0.5)

    async def _click(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        index = int(req.get("index") or 0)
        if index > 0:
            node = await session.get_dom_element_by_index(index)
            if node is None:
                return {"ok": False, "error": f"element index {index} not found in selector_map"}
            event = session.event_bus.dispatch(ClickElementEvent(node=node))
            await event
            return await self._page_info(session, {"clicked_index": index})
        return await self._act_by_selector(session, req, for_fill=False)

    async def _click_coordinate(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        x = int(req.get("coordinate_x") or -1)
        y = int(req.get("coordinate_y") or -1)
        if x < 0 or y < 0:
            return {"ok": False, "error": "coordinate_x and coordinate_y are required"}
        button = str(req.get("button") or "left").strip() or "left"
        event = session.event_bus.dispatch(
            ClickCoordinateEvent(coordinate_x=x, coordinate_y=y, button=button)
        )
        await event
        return await self._page_info(
            session, {"clicked_coordinate": {"x": x, "y": y, "button": button}, "interaction": "visual"}
        )

    async def _fill(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        index = int(req.get("index") or 0)
        text = str(req.get("text") or "")
        if index > 0:
            node = await session.get_dom_element_by_index(index)
            if node is None:
                return {"ok": False, "error": f"element index {index} not found in selector_map"}
            event = session.event_bus.dispatch(TypeTextEvent(node=node, text=text))
            await event
            return await self._page_info(session, {"filled_index": index})
        return await self._act_by_selector(session, req, for_fill=True)

    async def _press(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        key = str(req.get("key") or "").strip()
        if not key:
            return {"ok": False, "error": "key is required"}
        event = session.event_bus.dispatch(SendKeysEvent(keys=key))
        await event
        return await self._page_info(session)

    async def _screenshot(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        path = str(req.get("path") or "").strip()
        if not path:
            return {"ok": False, "error": "path is required"}
        data = await session.take_screenshot(full_page=False)
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        Path(path).write_bytes(data)
        out = await self._page_info(session)
        out["screenshot_path"] = path
        return out

    async def _wait(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        index = int(req.get("index") or 0)
        selector = str(req.get("selector") or "").strip()
        timeout_ms = int(req.get("timeout_ms") or self.settings.default_timeout_ms)
        if timeout_ms <= 0:
            timeout_ms = self.settings.default_timeout_ms
        deadline = time.monotonic() + timeout_ms / 1000.0
        if index > 0:
            while time.monotonic() < deadline:
                node = await session.get_dom_element_by_index(index)
                if node is not None:
                    return await self._page_info(session, {"waited_index": index})
                await asyncio.sleep(0.2)
            return {"ok": False, "error": f"timeout waiting for index {index}"}
        if selector:
            while time.monotonic() < deadline:
                if await self._eval_selector_exists(session, selector):
                    return await self._page_info(session, {"waited_selector": selector})
                await asyncio.sleep(0.2)
            return {"ok": False, "error": f"timeout waiting for selector {selector!r}"}
        load_state = str(req.get("load_state") or "load").strip()
        await self._wait_load(session, load_state)
        return await self._page_info(session)

    async def _snapshot(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        max_elements = normalize_max_elements(int(req.get("max_elements") or 0))
        include_screenshot = bool(req.get("include_screenshot"))
        screenshot_path = str(req.get("path") or "").strip()
        state = await session.get_browser_state_summary(include_screenshot=False)
        dom_state = state.dom_state
        llm_text = ""
        interactive_count = 0
        elements: list[dict[str, Any]] = []
        if dom_state is not None:
            interactive_count = len(dom_state.selector_map or {})
            try:
                llm_text = dom_state.llm_representation()
            except Exception:
                llm_text = ""
            if len(llm_text) > DEFAULT_LLM_REPRESENTATION_MAX:
                llm_text = llm_text[: DEFAULT_LLM_REPRESENTATION_MAX] + "…"
            if dom_state.selector_map:
                for index, node in sorted(dom_state.selector_map.items()):
                    elements.append(element_from_node(index, node))
                    if len(elements) >= max_elements:
                        break
        interaction = "index"
        if include_screenshot:
            interaction = "visual"
        detail: dict[str, Any] = {
            "llm_representation": llm_text,
            "interactive_count": interactive_count,
            "elements": elements,
            "returned": len(elements),
            "truncated": interactive_count > len(elements),
            "max_elements": max_elements,
            "engine": "browser-use",
            "interaction": interaction,
        }
        out = await self._page_info(session)
        if include_screenshot:
            if not screenshot_path:
                return {"ok": False, "error": "path is required when include_screenshot is true"}
            data = await session.take_screenshot(full_page=False)
            Path(screenshot_path).parent.mkdir(parents=True, exist_ok=True)
            Path(screenshot_path).write_bytes(data)
            out["screenshot_path"] = screenshot_path
            detail["screenshot"] = True
        out["detail"] = detail
        return out

    async def _page_info(
        self, session: BrowserSession, extra_detail: dict[str, Any] | None = None
    ) -> dict[str, Any]:
        state = await session.get_browser_state_summary(include_screenshot=False)
        detail = dict(extra_detail or {})
        try:
            tabs = await session.get_tabs()
            detail["tabs"] = [
                {
                    "tab_id": str(getattr(t, "tab_id", "") or "")[-4:],
                    "url": getattr(t, "url", "") or "",
                    "title": getattr(t, "title", "") or "",
                }
                for t in (tabs or [])
            ]
        except Exception:
            pass
        out: dict[str, Any] = {
            "ok": True,
            "url": state.url or "",
            "title": state.title or "",
        }
        if detail:
            out["detail"] = detail
        return out

    async def _run_action(
        self, session: BrowserSession, action: str, params: dict[str, Any], **kwargs: Any
    ) -> dict[str, Any]:
        result = await self._actions.run(action, params, session=session, **kwargs)
        if not result.get("ok"):
            return result
        page = await self._page_info(session, result.get("detail"))
        return page

    async def _search(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        query = str(req.get("query") or "").strip()
        if not query:
            return {"ok": False, "error": "query is required"}
        engine = str(req.get("engine") or "duckduckgo").strip() or "duckduckgo"
        return await self._run_action(session, "search", {"query": query, "engine": engine})

    async def _go_back(self, session: BrowserSession) -> dict[str, Any]:
        return await self._run_action(session, "go_back", {})

    async def _scroll(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        down = req.get("down")
        if down is None:
            down = True
        pages = float(req.get("pages") or 1.0)
        params: dict[str, Any] = {"down": bool(down), "pages": pages}
        index = req.get("index")
        if index is not None:
            params["index"] = int(index)
        return await self._run_action(session, "scroll", params)

    async def _find_text(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        text = str(req.get("text") or "").strip()
        if not text:
            return {"ok": False, "error": "text is required"}
        return await self._run_action(session, "find_text", {"text": text})

    async def _switch_tab(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        tab_id = str(req.get("tab_id") or "").strip()
        if len(tab_id) != 4:
            return {"ok": False, "error": "tab_id must be 4 characters (see browser_snapshot detail.tabs)"}
        return await self._run_action(session, "switch", {"tab_id": tab_id})

    async def _close_tab(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        tab_id = str(req.get("tab_id") or "").strip()
        if len(tab_id) != 4:
            return {"ok": False, "error": "tab_id must be 4 characters"}
        return await self._run_action(session, "close", {"tab_id": tab_id})

    async def _extract(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        if self._actions.extraction_llm is None:
            return {
                "ok": False,
                "error": "browser_extract requires llm.model in config (non-mock)",
            }
        query = str(req.get("query") or "").strip()
        if not query:
            return {"ok": False, "error": "query is required"}
        params: dict[str, Any] = {
            "query": query,
            "extract_links": bool(req.get("extract_links")),
            "extract_images": bool(req.get("extract_images")),
            "start_from_char": int(req.get("start_from_char") or 0),
        }
        already = req.get("already_collected") or []
        if isinstance(already, list):
            params["already_collected"] = [str(x) for x in already]
        return await self._run_action(session, "extract", params)

    async def _evaluate(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        code = str(req.get("code") or "").strip()
        if not code:
            return {"ok": False, "error": "code is required"}
        return await self._run_action(session, "evaluate", {"code": code})

    async def _find_elements(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        selector = str(req.get("selector") or "").strip()
        if not selector:
            return {"ok": False, "error": "selector is required"}
        params: dict[str, Any] = {
            "selector": selector,
            "max_results": int(req.get("max_results") or 50),
            "include_text": bool(req.get("include_text", True)),
        }
        attrs = req.get("attributes")
        if isinstance(attrs, list) and attrs:
            params["attributes"] = [str(x) for x in attrs]
        return await self._run_action(session, "find_elements", params)

    async def _search_page(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        pattern = str(req.get("pattern") or "").strip()
        if not pattern:
            return {"ok": False, "error": "pattern is required"}
        params: dict[str, Any] = {
            "pattern": pattern,
            "regex": bool(req.get("regex")),
            "case_sensitive": bool(req.get("case_sensitive")),
            "context_chars": int(req.get("context_chars") or 150),
            "max_results": int(req.get("max_results") or 25),
        }
        css_scope = str(req.get("css_scope") or "").strip()
        if css_scope:
            params["css_scope"] = css_scope
        return await self._run_action(session, "search_page", params)

    async def _upload_file(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        index = int(req.get("index") or 0)
        path = str(req.get("path") or "").strip()
        if index <= 0:
            return {"ok": False, "error": "index is required"}
        if not path:
            return {"ok": False, "error": "path is required"}
        if not Path(path).is_file():
            return {"ok": False, "error": f"file not found: {path}"}
        return await self._run_action(
            session,
            "upload_file",
            {"index": index, "path": path},
            available_file_paths=[path],
        )

    async def _dropdown_options(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        index = int(req.get("index") or 0)
        if index <= 0:
            return {"ok": False, "error": "index is required"}
        return await self._run_action(session, "dropdown_options", {"index": index})

    async def _select_dropdown(self, session: BrowserSession, req: dict[str, Any]) -> dict[str, Any]:
        index = int(req.get("index") or 0)
        text = str(req.get("text") or "").strip()
        if index <= 0:
            return {"ok": False, "error": "index is required"}
        if not text:
            return {"ok": False, "error": "text is required"}
        return await self._run_action(session, "select_dropdown", {"index": index, "text": text})

    async def _act_by_selector(
        self, session: BrowserSession, req: dict[str, Any], *, for_fill: bool
    ) -> dict[str, Any]:
        selector = str(req.get("selector") or "").strip()
        fallbacks = [str(x).strip() for x in (req.get("selector_fallbacks") or []) if str(x).strip()]
        candidates = [c for c in [selector, *fallbacks] if c]
        if not candidates:
            return {
                "ok": False,
                "error": "index or selector is required (prefer index from browser_snapshot)",
            }
        text = str(req.get("text") or "")
        for cand in candidates:
            if not await self._eval_selector_exists(session, cand):
                continue
            ok = await self._eval_click_or_fill(session, cand, text, for_fill)
            if ok:
                key = "filled_selector" if for_fill else "clicked_selector"
                return await self._page_info(session, {key: cand, "fallback": True})
        return {"ok": False, "error": f"selector not found: {candidates[0]}"}

    async def _eval_selector_exists(self, session: BrowserSession, selector: str) -> bool:
        js = f"document.querySelector({json.dumps(selector)}) !== null"
        return await self._eval_bool(session, js)

    async def _eval_click_or_fill(
        self, session: BrowserSession, selector: str, text: str, is_fill: bool
    ) -> bool:
        if is_fill:
            js = f"""(function() {{
              const el = document.querySelector({json.dumps(selector)});
              if (!el) return false;
              el.focus();
              el.value = '';
              el.value = {json.dumps(text)};
              el.dispatchEvent(new Event('input', {{ bubbles: true }}));
              el.dispatchEvent(new Event('change', {{ bubbles: true }}));
              return true;
            }})()"""
        else:
            js = f"""(function() {{
              const el = document.querySelector({json.dumps(selector)});
              if (!el) return false;
              el.click();
              return true;
            }})()"""
        return await self._eval_bool(session, js)

    async def _eval_bool(self, session: BrowserSession, expression: str) -> bool:
        cdp = await session.get_or_create_cdp_session(target_id=None, focus=True)
        result = await cdp.cdp_client.send.Runtime.evaluate(
            params={"expression": expression, "returnByValue": True},
            session_id=cdp.session_id,
        )
        value = (result.get("result") or {}).get("value")
        return bool(value)
