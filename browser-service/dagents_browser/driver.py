from __future__ import annotations

import asyncio
import hashlib
import json
import re
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

from playwright.async_api import Browser, BrowserContext, Locator, Page, Playwright, async_playwright

from dagents_browser.config import BrowserServiceSettings

MAX_ACTIONS = 12
MAX_SCRIPT = 16_000
MAX_ARG_BYTES = 32 * 1024
MAX_RESULT_BYTES = 64 * 1024
DEFAULT_EVALUATE_TIMEOUT_MS = 5_000
MAX_EVALUATE_TIMEOUT_MS = 10_000
RECEIPT_TTL_SECONDS = 600


def sanitize_segment(raw: str) -> str:
    out = re.sub(r"[^a-zA-Z0-9_-]+", "-", (raw or "").strip()).strip("-")
    return out or "default"


def navigation_url_allowed(url: str, allowed_schemes: list[str] | None) -> bool:
    value = str(url or "").strip()
    if not value:
        return False
    parsed = urlsplit(value)
    if not parsed.scheme:
        return not value.startswith("//")
    allowed = {str(item or "").strip().lower().rstrip(":") for item in (allowed_schemes or ["https", "http"])}
    return parsed.scheme.lower() in allowed


@dataclass
class PageState:
    page: Page
    page_id: str
    refs: dict[str, dict[str, Any]] = field(default_factory=dict)
    snapshot_counter: int = 0


@dataclass
class SessionState:
    session_key: str
    context: BrowserContext
    browser: Browser | None
    owns_context: bool = True
    pages: dict[str, PageState] = field(default_factory=dict)
    lock: asyncio.Lock = field(default_factory=asyncio.Lock)
    counter: int = 0


class PlaywrightDriver:
    """Deterministic Playwright driver for the two main-agent browser tools."""

    def __init__(self, settings: BrowserServiceSettings) -> None:
        self.settings = settings
        self._playwright: Playwright | None = None
        self._sessions: dict[str, SessionState] = {}
        self._lock = asyncio.Lock()
        self._receipts: dict[tuple[str, str], tuple[float, dict[str, Any]]] = {}

    async def close(self) -> None:
        async with self._lock:
            keys = list(self._sessions)
        for key in keys:
            await self._stop_session(key)
        if self._playwright is not None:
            await self._playwright.stop()
            self._playwright = None

    async def call(self, req: dict[str, Any]) -> dict[str, Any]:
        op = str(req.get("op") or "").strip()
        if op == "ping":
            return {"ok": True, "detail": {"driver": "playwright-v2", "protocol_version": 2}}
        if op in {"call", "start", "stop"}:
            if op != "call":
                req = {**req, "op": "call", "actions": [{"op": op}]}
            return await self._call(req)
        if op == "evaluate":
            return await self._evaluate(req)
        return {"ok": False, "error_code": "invalid_action", "error": f"unknown op: {op}"}

    async def _ensure_playwright(self) -> Playwright:
        if self._playwright is None:
            self._playwright = await async_playwright().start()
        return self._playwright

    async def _start(self, req: dict[str, Any]) -> tuple[SessionState | None, dict[str, Any] | None]:
        session_key = str(req.get("session_key") or "").strip()
        if not session_key:
            return None, self._error("invalid_action", "session_key is required")
        async with self._lock:
            existing = self._sessions.get(session_key)
            if existing is not None:
                return existing, None
            if len(self._sessions) >= max(1, int(self.settings.max_sessions or 8)):
                return None, self._error("session_limit_reached", "browser session limit reached")
        pw = await self._ensure_playwright()
        headed = bool(self.settings.headed if req.get("headed") is None else req.get("headed"))
        width = int(req.get("viewport_width") or 1280)
        height = int(req.get("viewport_height") or 720)
        profile = Path(self.settings.runtime_root) / "browser" / "profiles" / sanitize_segment(session_key)
        profile.mkdir(parents=True, exist_ok=True)
        browser: Browser | None = None
        owns_context = True
        try:
            if self.settings.cdp_url:
                browser = await pw.chromium.connect_over_cdp(self.settings.cdp_url)
                contexts = browser.contexts
                if not contexts:
                    context = await browser.new_context(viewport={"width": width, "height": height}, service_workers="block")
                else:
                    context = contexts[0]
                    owns_context = False
            else:
                context = await pw.chromium.launch_persistent_context(
                    str(profile),
                    headless=not headed,
                    executable_path=self.settings.chrome_path or None,
                    viewport={"width": width, "height": height},
                    ignore_https_errors=bool(self.settings.ignore_https_errors),
                    service_workers="block",
                )
            state = SessionState(session_key=session_key, context=context, browser=browser, owns_context=owns_context)
            context.on("page", lambda page: self._register_page_nowait(state, page))
            await self._install_network_policy(context)
            for page in context.pages:
                await self._register_page(state, page)
            if not state.pages:
                await self._register_page(state, await context.new_page())
            async with self._lock:
                self._sessions[session_key] = state
            return state, None
        except Exception as exc:
            if browser is not None:
                await browser.close()
            return None, self._error("playwright_unavailable", str(exc))

    def _register_page_nowait(self, state: SessionState, page: Page) -> None:
        asyncio.create_task(self._register_page(state, page))

    async def _register_page(self, state: SessionState, page: Page) -> PageState:
        for existing in state.pages.values():
            if existing.page is page:
                return existing
        state.counter += 1
        page_state = PageState(page=page, page_id=f"page_{state.counter}")
        state.pages[page_state.page_id] = page_state
        page.on("framenavigated", lambda frame: self._navigation_event_nowait(page_state, frame))
        page.on("close", lambda *_: state.pages.pop(page_state.page_id, None))
        return page_state

    def _navigation_event_nowait(self, page_state: PageState, frame: Any) -> None:
        if getattr(frame, "parent_frame", None) is None:
            page_state.refs.clear()

    async def _install_network_policy(self, context: BrowserContext) -> None:
        allowed = {s.lower().rstrip(":") for s in (self.settings.allowed_url_schemes or ["https", "http"])}

        async def route_handler(route: Any) -> None:
            scheme = urlsplit(route.request.url).scheme.lower()
            if scheme not in allowed:
                await route.abort("blockedbyclient")
                return
            await route.continue_()

        await context.route("**/*", route_handler)
        if hasattr(context, "route_web_socket"):
            async def websocket_handler(ws: Any) -> None:
                scheme = urlsplit(ws.url).scheme.lower()
                websocket_allowed = scheme in allowed or (scheme == "ws" and "http" in allowed) or (scheme == "wss" and "https" in allowed)
                if not websocket_allowed:
                    await ws.close()
                    return
                await ws.connect()
            await context.route_web_socket("**/*", websocket_handler)

    async def _stop_session(self, session_key: str) -> None:
        async with self._lock:
            state = self._sessions.pop(session_key, None)
        if state is None:
            return
        try:
            if state.owns_context:
                await state.context.close()
        finally:
            if state.browser is not None:
                await state.browser.close()

    async def _call(self, req: dict[str, Any]) -> dict[str, Any]:
        session_key = str(req.get("session_key") or "").strip()
        actions = req.get("actions")
        if not isinstance(actions, list) or not actions or len(actions) > MAX_ACTIONS:
            return self._error("invalid_action", "actions must contain 1..12 items")
        call_id = str(req.get("call_id") or "").strip()
        digest = self._digest({"actions": actions, "session_key": session_key})
        self._prune_receipts()
        receipt = self._receipts.get((call_id, digest)) if call_id else None
        if receipt is not None:
            return receipt[1]
        if call_id and any(key[0] == call_id and key[1] != digest for key in self._receipts):
            return self._error("call_conflict", "call_id was already used with another payload")
        state, err = await self._start(req) if str(actions[0].get("op") or "") == "start" else (self._sessions.get(session_key), None)
        if err is not None:
            return err
        if state is None:
            return self._error("browser_not_started", "browser session is not started")
        async with state.lock:
            result = await self._run_actions(state, actions, req)
        if call_id:
            self._receipts[(call_id, digest)] = (time.monotonic(), result)
        return result

    async def _run_actions(self, state: SessionState, actions: list[dict[str, Any]], req: dict[str, Any]) -> dict[str, Any]:
        results: list[dict[str, Any]] = []
        status = "succeeded"
        failed_index: int | None = None
        final_observation: dict[str, Any] | None = None
        for index, raw in enumerate(actions):
            op = str(raw.get("op") or "").strip()
            started = time.monotonic()
            try:
                data = await self._action(state, raw, req)
                results.append({"index": index, "op": op, "status": "succeeded", "duration_ms": int((time.monotonic() - started) * 1000), **({"data": data} if data is not None else {})})
                final_observation = data if op == "observe" and isinstance(data, dict) else None
            except Exception as exc:
                status = "partial_failure" if index > 0 else "failed"
                failed_index = index
                final_observation = None
                results.append({"index": index, "op": op, "status": "failed", "duration_ms": int((time.monotonic() - started) * 1000), "error": {"code": self._error_code(exc), "message": str(exc), "retryable": False}})
                for skipped in range(index + 1, len(actions)):
                    results.append({"index": skipped, "op": str(actions[skipped].get("op") or ""), "status": "skipped"})
                break
        page = await self._active_page(state)
        observation = final_observation if final_observation is not None else (await self._observe(state, page) if page is not None and not page.is_closed() else None)
        detail = {"status": status, "action_results": results, "observation": observation}
        if failed_index is not None:
            detail["failed_action_index"] = failed_index
        return {"ok": status == "succeeded", "url": page.url if page is not None and not page.is_closed() else "", "title": await page.title() if page is not None and not page.is_closed() else "", "detail": detail}

    async def _action(self, state: SessionState, raw: dict[str, Any], req: dict[str, Any]) -> Any:
        op = str(raw.get("op") or "").strip()
        if op == "start":
            return {"started": True}
        if op == "stop":
            await self._stop_session(str(req.get("session_key") or ""))
            return {"stopped": True}
        page = await self._page_for(state, raw.get("page_id"))
        if page is None:
            raise RuntimeError("page_closed")
        if op == "tabs":
            return [{"page_id": p.page_id, "url": p.page.url, "title": await p.page.title()} for p in state.pages.values() if not p.page.is_closed()]
        if op == "observe":
            return await self._observe(state, page)
        if op == "screenshot":
            output_dir = sanitize_segment(self.settings.output_dir or "browser")
            path = Path(self.settings.runtime_root) / output_dir / "screenshots" / f"{sanitize_segment(str(req.get('session_key') or 'session'))}-{int(time.time() * 1000)}.png"
            path.parent.mkdir(parents=True, exist_ok=True)
            await page.screenshot(path=str(path), full_page=False)
            return {"path": str(path), "mime": "image/png"}
        if op == "navigate":
            url = str((raw.get("params") or {}).get("url") or "").strip()
            if not navigation_url_allowed(url, self.settings.allowed_url_schemes):
                raise RuntimeError("navigation_blocked")
            await page.goto(url, wait_until="domcontentloaded", timeout=int(raw.get("timeout_ms") or self.settings.default_timeout_ms))
            return {"url": page.url}
        if op == "back":
            await page.go_back(timeout=int(raw.get("timeout_ms") or self.settings.default_timeout_ms))
            return {"url": page.url}
        if op == "reload":
            await page.reload(wait_until="domcontentloaded", timeout=int(raw.get("timeout_ms") or self.settings.default_timeout_ms))
            return {"url": page.url}
        if op in {"click", "fill", "type", "select_option", "check", "press", "hover"}:
            locator = await self._locator(state, page, raw.get("target"))
            params = raw.get("params") or {}
            if op == "click": await locator.click(timeout=int(raw.get("timeout_ms") or self.settings.default_timeout_ms))
            elif op == "fill": await locator.fill(str(params.get("value", "") if params.get("value") is not None else ""), timeout=int(raw.get("timeout_ms") or self.settings.default_timeout_ms))
            elif op == "type": await locator.press_sequentially(str(params.get("value", "") if params.get("value") is not None else ""), timeout=int(raw.get("timeout_ms") or self.settings.default_timeout_ms))
            elif op == "select_option":
                option: Any
                if "value" in params:
                    option = {"value": params["value"]}
                elif "label" in params:
                    option = {"label": params["label"]}
                elif "index" in params:
                    option = {"index": int(params["index"])}
                else:
                    raise RuntimeError("select_option requires value, label, or index")
                await locator.select_option(option, timeout=int(raw.get("timeout_ms") or self.settings.default_timeout_ms))
            elif op == "check": await locator.check(timeout=int(raw.get("timeout_ms") or self.settings.default_timeout_ms))
            elif op == "press": await locator.press(str(params.get("key") or "Enter"), timeout=int(raw.get("timeout_ms") or self.settings.default_timeout_ms))
            else: await locator.hover(timeout=int(raw.get("timeout_ms") or self.settings.default_timeout_ms))
            return None
        if op == "scroll":
            await page.mouse.wheel(float((raw.get("params") or {}).get("x", 0)), float((raw.get("params") or {}).get("y", 600)))
            return None
        if op == "wait_for":
            timeout = int(raw.get("timeout_ms") or self.settings.default_timeout_ms)
            target = raw.get("target")
            if target:
                await (await self._locator(state, page, target)).wait_for(timeout=timeout)
            else:
                await page.wait_for_timeout(min(timeout, 10_000))
            return None
        raise RuntimeError("unsupported_action")

    async def _evaluate(self, req: dict[str, Any]) -> dict[str, Any]:
        session_key = str(req.get("session_key") or "").strip()
        script = str(req.get("script") or "")
        if not session_key or not script or len(script) > MAX_SCRIPT:
            return self._error("invalid_action", "session_key and script are required")
        arg = req.get("arg")
        if len(json.dumps(arg, ensure_ascii=False, separators=(",", ":")).encode("utf-8")) > MAX_ARG_BYTES:
            return self._error("script_result_too_large", "evaluate arg is too large")
        state = self._sessions.get(session_key)
        if state is None:
            return self._error("browser_not_started", "browser session is not started")
        digest = self._digest({"session_key": session_key, "script": script, "arg": arg, "page_id": req.get("page_id"), "target": req.get("target")})
        call_id = str(req.get("call_id") or "").strip()
        self._prune_receipts()
        receipt = self._receipts.get((call_id, digest)) if call_id else None
        if receipt is not None: return receipt[1]
        if call_id and any(key[0] == call_id and key[1] != digest for key in self._receipts): return self._error("call_conflict", "call_id was already used with another payload")
        async with state.lock:
            page = await self._page_for(state, req.get("page_id"))
            if page is None: return self._error("page_closed", "page is closed")
            page_state = next((item for item in state.pages.values() if item.page is page), None)
            try:
                if req.get("target"):
                    locator = await self._locator(state, page, req.get("target"))
                    result = await asyncio.wait_for(locator.evaluate(script, arg), timeout=self._evaluate_timeout(req))
                else:
                    result = await asyncio.wait_for(page.evaluate(script, arg), timeout=self._evaluate_timeout(req))
                encoded = json.dumps(result, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode("utf-8")
                if len(encoded) > MAX_RESULT_BYTES: raise ValueError("script_result_too_large")
                out = {"ok": True, "url": page.url, "title": await page.title(), "detail": {"status": "succeeded", "value": json.loads(encoded), "observation": await self._observe(state, page)}}
            except asyncio.TimeoutError:
                await self._terminate_page(state, page)
                out = self._error("script_timeout", "evaluate timed out")
            except Exception as exc:
                code = "script_result_not_serializable" if "not serializable" in str(exc) or "not JSON" in str(exc) else "script_runtime_error"
                if "script_parse" in str(exc) or "SyntaxError" in str(exc): code = "script_parse_error"
                out = self._error(code, str(exc))
            finally:
                if page_state is not None: page_state.refs.clear()
        if call_id: self._receipts[(call_id, digest)] = (time.monotonic(), out)
        return out

    async def _terminate_page(self, state: SessionState, page: Page) -> None:
        try: await page.close()
        except Exception: pass
        # A timed-out script can leave the page/context in an unknown state.
        # Tear down the whole session so the next explicit `start` can recover
        # instead of reusing a closed BrowserContext.
        await self._stop_session(state.session_key)

    def _evaluate_timeout(self, req: dict[str, Any]) -> float:
        value = int(req.get("timeout_ms") or DEFAULT_EVALUATE_TIMEOUT_MS)
        return max(1, min(value, MAX_EVALUATE_TIMEOUT_MS)) / 1000

    async def _page_for(self, state: SessionState, page_id: Any) -> Page | None:
        if page_id:
            item = state.pages.get(str(page_id))
            return item.page if item and not item.page.is_closed() else None
        return await self._active_page(state)

    async def _active_page(self, state: SessionState) -> Page | None:
        for item in reversed(list(state.pages.values())):
            if not item.page.is_closed(): return item.page
        return None

    async def _locator(self, state: SessionState, page: Page, target: Any) -> Locator:
        if not isinstance(target, dict): raise RuntimeError("target_not_found")
        item: dict[str, Any] = target
        if target.get("ref"):
            page_state = next((p for p in state.pages.values() if p.page is page), None)
            if page_state is None or str(target["ref"]) not in page_state.refs: raise RuntimeError("stale_ref")
            item = page_state.refs[str(target["ref"])]
        if item.get("test_id"): locator = page.get_by_test_id(str(item["test_id"]))
        elif item.get("role"):
            locator = page.get_by_role(str(item["role"]), name=item.get("name") or item.get("accessible_name"), exact=bool(item.get("exact", True)))
        elif item.get("label"): locator = page.get_by_label(str(item["label"]), exact=bool(item.get("exact", True)))
        elif item.get("placeholder"): locator = page.get_by_placeholder(str(item["placeholder"]), exact=bool(item.get("exact", True)))
        elif item.get("text"): locator = page.get_by_text(str(item["text"]), exact=bool(item.get("exact", True)))
        elif item.get("css"): locator = page.locator(str(item["css"]))
        else: raise RuntimeError("target_not_found")
        count = await locator.count()
        if count == 0: raise RuntimeError("target_not_found")
        if count > 1: raise RuntimeError("ambiguous_target")
        return locator

    async def _observe(self, state: SessionState, page: Page) -> dict[str, Any]:
        page_state = next((p for p in state.pages.values() if p.page is page), None)
        if page_state is None: return {}
        page_state.snapshot_counter += 1
        snapshot_id = f"s{page_state.snapshot_counter}"
        try:
            elements = await page.evaluate("""() => Array.from(document.querySelectorAll('a,button,input,textarea,select,[role]')).slice(0, 100).map((e, i) => ({tag:e.tagName.toLowerCase(), role:e.getAttribute('role') || (e.tagName==='BUTTON'?'button':e.tagName==='A'?'link':e.tagName==='SELECT'?'combobox':(e.tagName==='INPUT'||e.tagName==='TEXTAREA'?'textbox':null)), name:e.getAttribute('aria-label') || e.getAttribute('name') || e.getAttribute('placeholder') || (e.labels && e.labels[0] && (e.labels[0].innerText || '').trim()) || (e.innerText || '').trim().slice(0,80), text:(e.innerText || e.value || '').trim().slice(0,80), type:e.getAttribute('type') || ''}))""")
        except Exception:
            elements = []
        page_state.refs.clear()
        lines: list[str] = []
        for index, element in enumerate(elements or [], 1):
            ref = f"{snapshot_id}:e{index}"
            recipe = {"role": element.get("role"), "name": element.get("name"), "text": element.get("text"), "exact": False}
            recipe = {k: v for k, v in recipe.items() if v}
            page_state.refs[ref] = recipe
            label = element.get("name") or element.get("text") or element.get("tag")
            lines.append(f'- {element.get("role") or element.get("tag")} "{label}" [ref={ref}]')
        body = ""
        if not page.is_closed():
            try:
                body = await page.locator("body").inner_text(timeout=2_000)
            except Exception:
                body = ""
        content = "\n".join(lines)
        if body and not content: content = body[:30_000]
        return {"snapshot_id": snapshot_id, "page_id": page_state.page_id, "truncated": len(content) > 30_000, "content": content[:30_000]}

    @staticmethod
    def _digest(value: Any) -> str:
        return hashlib.sha256(json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode("utf-8")).hexdigest()

    def _prune_receipts(self) -> None:
        cutoff = time.monotonic() - RECEIPT_TTL_SECONDS
        for key, (created_at, _result) in list(self._receipts.items()):
            if created_at < cutoff:
                self._receipts.pop(key, None)

    @staticmethod
    def _error(code: str, message: str) -> dict[str, Any]:
        return {"ok": False, "error_code": code, "error": message}

    @staticmethod
    def _error_code(exc: Exception) -> str:
        message = str(exc)
        for code in ("stale_ref", "target_not_found", "ambiguous_target", "navigation_blocked", "unsupported_action", "page_closed"):
            if code in message: return code
        return "action_timeout" if "Timeout" in type(exc).__name__ or "timeout" in message.lower() else "internal_error"
