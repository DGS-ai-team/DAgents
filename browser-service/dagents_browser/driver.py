from __future__ import annotations

import asyncio
import re
import time
import uuid
from pathlib import Path
from typing import Any

from browser_use import BrowserProfile, BrowserSession

from dagents_browser.config import BrowserServiceSettings
from dagents_browser.agent_prompt import build_extend_system_message
from dagents_browser.llm import create_extraction_llm
from dagents_browser.ports import allocate_debug_port
from dagents_browser.task_archive import (
    archive_task,
    format_recent_tasks_for_prompt,
    load_recent_tasks,
)
from dagents_browser.task_result import summarize_agent_history, task_status_response

DEFAULT_TASK_MAX_STEPS = 50


def sanitize_segment(raw: str) -> str:
    raw = (raw or "").strip()
    if not raw:
        return "default"
    out = re.sub(r"[^a-zA-Z0-9_-]+", "-", raw).strip("-")
    return out or "default"


class BrowserUseDriver:
    """browser-use 驱动：session 生命周期 + 任务级 run_task（Agent 闭环）。"""

    def __init__(self, settings: BrowserServiceSettings) -> None:
        self.settings = settings
        self._sessions: dict[str, BrowserSession] = {}
        self._session_ports: dict[str, int] = {}
        self._session_locks: dict[str, asyncio.Lock] = {}
        self._lock = asyncio.Lock()
        self._tasks: dict[str, dict[str, Any]] = {}
        self._session_latest_task: dict[str, str] = {}

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
        return {"ok": False, "error": f"unknown op: {op}"}

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
                "error": (
                    "浏览器任务需要真实 LLM：当前 Node 配置为 mock 或未设置 llm.model。"
                    "请在设置中切换非 mock 模型档案后重试（browser-use.Agent 无法在 mock 下闭环）。"
                ),
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

                    task_fs = str(
                        Path(self.settings.fs_root) / "browser" / "agent_fs" / sanitize_segment(session_key)
                    )
                    Path(task_fs).mkdir(parents=True, exist_ok=True)
                    recent = load_recent_tasks(task_fs)
                    agent = Agent(
                        task=task_text,
                        llm=llm,
                        browser_session=session,
                        extend_system_message=build_extend_system_message(
                            fs_root=self.settings.fs_root,
                            allowed_url_schemes=self.settings.allowed_url_schemes,
                            recent_tasks_block=format_recent_tasks_for_prompt(recent),
                        ),
                        file_system_path=task_fs,
                    )
                    history = await agent.run(max_steps=max_steps)
                    summarized = summarize_agent_history(history)
                    entry["status"] = "completed"
                    try:
                        archived = archive_task(
                            agent_fs=task_fs,
                            task_id=task_id,
                            task=task_text,
                            status="completed",
                            result=summarized,
                            session_key=session_key,
                            max_steps=max_steps,
                        )
                        summarized["detail_md"] = archived.get("detail_md")
                        summarized["detail_json"] = archived.get("detail_json")
                        summarized["cite_label"] = (summarized.get("summary") or task_text or task_id)[:80]
                    except Exception:
                        pass
                    entry["result"] = summarized
            except asyncio.CancelledError:
                entry["status"] = "cancelled"
                entry["error"] = "cancelled"
                raise
            except Exception as exc:
                entry["status"] = "failed"
                entry["error"] = str(exc)
                try:
                    task_fs = str(
                        Path(self.settings.fs_root) / "browser" / "agent_fs" / sanitize_segment(session_key)
                    )
                    archived = archive_task(
                        agent_fs=task_fs,
                        task_id=task_id,
                        task=task_text,
                        status="failed",
                        result=entry.get("result") or {},
                        error=str(exc),
                        session_key=session_key,
                        max_steps=max_steps,
                    )
                    entry["result"] = {
                        **(entry.get("result") or {}),
                        "detail_md": archived.get("detail_md"),
                        "detail_json": archived.get("detail_json"),
                        "cite_label": (task_text or task_id)[:80],
                    }
                except Exception:
                    pass
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
        # 兼容取消等仍返回扁平 detail 的调用方
        return task_status_response(entry).get("detail") or {}

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
        return task_status_response(entry)

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
        return task_status_response(entry)

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

