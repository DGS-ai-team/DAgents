import unittest
import asyncio

from dagents_browser.config import BrowserServiceSettings, parse_listen
from dagents_browser.driver import PlaywrightDriver, navigation_url_allowed


class DriverProtocolTests(unittest.IsolatedAsyncioTestCase):
    def settings(self):
        return BrowserServiceSettings(runtime_root=".runtime-test", headed=False)

    async def test_ping_and_validation_do_not_launch_browser(self):
        driver = PlaywrightDriver(self.settings())
        try:
            self.assertEqual(
                await driver.call({"op": "ping"}),
                {"ok": True, "detail": {"driver": "playwright-v2", "protocol_version": 2}},
            )
            result = await driver.call({"op": "call", "session_key": "s", "actions": []})
            self.assertFalse(result["ok"])
            self.assertEqual(result["error_code"], "invalid_action")
            result = await driver.call({"op": "evaluate", "session_key": "s", "script": ""})
            self.assertFalse(result["ok"])
            self.assertEqual(result["error_code"], "invalid_action")
        finally:
            await driver.close()

    async def test_view_is_read_only_and_does_not_rebuild_snapshot_refs(self):
        driver = PlaywrightDriver(self.settings())

        class FakePage:
            url = "https://example.test/"

            def __init__(self, title, image):
                self._title = title
                self.image = image

            def is_closed(self):
                return False

            async def title(self):
                return self._title

            async def screenshot(self, **kwargs):
                self.screenshot_kwargs = kwargs
                return self.image

        class FakePageState:
            def __init__(self, page, refs, snapshot_counter):
                self.page = page
                self.refs = refs
                self.snapshot_counter = snapshot_counter

        class FakeState:
            def __init__(self, pages):
                self.pages = pages
                self.lock = asyncio.Lock()

        first = FakePage("First", b"png-one")
        second = FakePage("Second", b"png-two")
        first_refs = {"s7:e1": {"role": "button", "name": "Continue"}}
        second_refs = {"s3:e2": {"css": "#save"}}
        state = FakeState({
            "page_1": FakePageState(first, first_refs, 7),
            "page_2": FakePageState(second, second_refs, 3),
        })
        driver._sessions["agent-1"] = state
        try:
            result = await driver.call({"op": "view", "session_key": "agent-1", "page_id": "page_1"})
            self.assertTrue(result["ok"])
            self.assertEqual(result["title"], "First")
            self.assertEqual(result["url"], "https://example.test/")
            self.assertEqual(result["detail"]["page_id"], "page_1")
            self.assertEqual(result["detail"]["pages"], [
                {"page_id": "page_1", "url": "https://example.test/", "title": "First"},
                {"page_id": "page_2", "url": "https://example.test/", "title": "Second"},
            ])
            self.assertEqual(result["detail"]["mime"], "image/png")
            self.assertEqual(first_refs, {"s7:e1": {"role": "button", "name": "Continue"}})
            self.assertEqual(state.pages["page_1"].snapshot_counter, 7)
            self.assertEqual(state.pages["page_2"].snapshot_counter, 3)
        finally:
            driver._sessions.clear()

    async def test_view_rejects_unstarted_sessions_and_cross_session_page_ids(self):
        driver = PlaywrightDriver(self.settings())
        missing = await driver.call({"op": "view", "session_key": "missing"})
        self.assertEqual(missing["error_code"], "browser_not_started")

        class FakePage:
            url = "about:blank"
            def is_closed(self): return False
            async def title(self): return "Open page"
            async def screenshot(self, **_kwargs): return b"png"

        class FakePageState:
            def __init__(self, page): self.page = page

        class FakeState:
            def __init__(self, pages): self.pages, self.lock = pages, asyncio.Lock()

        driver._sessions["agent-a"] = FakeState({"page_a": FakePageState(FakePage())})
        driver._sessions["agent-b"] = FakeState({"page_b": FakePageState(FakePage())})
        try:
            result = await driver.call({"op": "view", "session_key": "agent-a", "page_id": "page_b"})
            self.assertEqual(result["error_code"], "page_not_found")
        finally:
            driver._sessions.clear()

    async def test_view_enforces_four_megabyte_image_limit(self):
        driver = PlaywrightDriver(self.settings())

        class FakePage:
            url = "https://example.test/"
            def is_closed(self): return False
            async def title(self): return "Large page"
            async def screenshot(self, **_kwargs): return b"x" * (4 * 1024 * 1024 + 1)

        class FakePageState:
            def __init__(self, page): self.page = page

        class FakeState:
            def __init__(self): self.pages, self.lock = {"page_1": FakePageState(FakePage())}, asyncio.Lock()

        driver._sessions["agent-1"] = FakeState()
        try:
            result = await driver.call({"op": "view", "session_key": "agent-1"})
            self.assertFalse(result["ok"])
            self.assertEqual(result["error_code"], "image_too_large")
        finally:
            driver._sessions.clear()


class DriverHelperTests(unittest.TestCase):
    def test_url_scheme_policy(self):
        self.assertTrue(navigation_url_allowed("https://example.com", ["https", "http"]))
        self.assertTrue(navigation_url_allowed("http://127.0.0.1:8080", ["https", "http"]))
        self.assertFalse(navigation_url_allowed("file:///tmp/x", ["https", "http"]))
        self.assertFalse(navigation_url_allowed("javascript:alert(1)", ["https", "http"]))

    def test_parse_listen(self):
        self.assertEqual(parse_listen("127.0.0.1:19000"), ("127.0.0.1", 19000))
        self.assertEqual(parse_listen("19001"), ("127.0.0.1", 19001))


if __name__ == "__main__":
    unittest.main()

class DesktopControlTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self):
        from dagents_browser.driver import PageState, SessionState
        self.driver = PlaywrightDriver(BrowserServiceSettings(runtime_root='.runtime-test', headed=True))
        self.pages = []
        class Page:
            url = 'about:blank'
            closed = False
            def __init__(self): self.events = {}; self.operations = []
            def is_closed(self): return self.closed
            def on(self, event, handler): self.events[event] = handler
            async def title(self): return 'Test page'
            async def screenshot(self, **kwargs): return b'png'
            async def goto(self, url, **kwargs):
                self.url = url; self.operations.append('navigate')
                handler = self.events.get('framenavigated')
                if handler: handler(type('Frame', (), {'parent_frame': None})())
            async def go_back(self, **kwargs): self.operations.append('back')
            async def go_forward(self, **kwargs): self.operations.append('forward')
            async def reload(self, **kwargs): self.operations.append('reload')
            async def close(self): self.closed = True
        pages = self.pages
        class Context:
            async def new_page(self):
                page = Page(); pages.append(page); return page
        self.context = Context()
        self.state = SessionState('agent-1', self.context, None)
        self.page = await self.driver._register_page(self.state, await self.context.new_page())
        self.page.refs['old'] = {'role': 'button'}
        self.driver._sessions['agent-1'] = self.state

    async def asyncTearDown(self):
        self.driver._sessions.clear()

    async def test_navigation_history_reload_and_cross_session_isolation(self):
        for command in ('navigate', 'back', 'forward', 'reload'):
            result = await self.driver.call({'op': 'control', 'session_key': 'agent-1', 'page_id': self.page.page_id, 'command': command, 'url': 'https://example.test'})
            self.assertTrue(result['ok'], result)
            self.assertEqual(result['detail']['page_id'], self.page.page_id)
        self.assertEqual(self.page.page.operations, ['navigate', 'back', 'forward', 'reload'])
        self.assertEqual(self.page.refs, {})
        # Explicit navigation invalidates refs; read-only view never rebuilds them.
        self.assertEqual(self.page.snapshot_counter, 0)
        result = await self.driver.call({'op': 'control', 'session_key': 'agent-1', 'page_id': 'foreign-page', 'command': 'navigate', 'url': 'https://example.test'})
        self.assertEqual(result['error_code'], 'page_not_found')
        self.assertEqual(len(self.page.page.operations), 4)

    async def test_new_close_all_and_reopen(self):
        result = await self.driver.call({'op': 'control', 'session_key': 'agent-1', 'command': 'new'})
        self.assertTrue(result['ok'])
        second = result['detail']['page_id']
        self.assertNotEqual(second, self.page.page_id)
        for page_id in (second, self.page.page_id):
            result = await self.driver.call({'op': 'control', 'session_key': 'agent-1', 'command': 'close', 'page_id': page_id})
            self.assertTrue(result['ok'])
        view = await self.driver.call({'op': 'view', 'session_key': 'agent-1'})
        self.assertTrue(view['ok'])
        self.assertEqual(view['detail']['pages'], [])
        result = await self.driver.call({'op': 'control', 'session_key': 'agent-1', 'command': 'navigate', 'url': 'https://example.test'})
        self.assertTrue(result['ok'])
        self.assertEqual(len(self.state.pages), 1)

    async def test_start_is_headless_and_invalid_commands_never_start(self):
        from unittest.mock import AsyncMock
        self.driver._start = AsyncMock(return_value=(self.state, None))
        for command, url in [('evaluate', ''), ('navigate', 'file:///private'), ('navigate', 'javascript:alert(1)'), ('navigate', 'http://[')]:
            result = await self.driver.call({'op': 'control', 'session_key': 'fresh-agent', 'command': command, 'url': url})
            self.assertFalse(result['ok'])
        self.driver._start.assert_not_awaited()
        result = await self.driver.call({'op': 'control', 'session_key': 'fresh-agent', 'command': 'navigate', 'url': 'https://example.test'})
        self.assertTrue(result['ok'])
        self.assertIs(self.driver._start.call_args.args[0]['headed'], False)

    async def test_navigation_error_keeps_started_session_and_does_not_retry(self):
        from unittest.mock import AsyncMock
        self.page.page.goto = AsyncMock(side_effect=TimeoutError('timeout'))
        result = await self.driver.call({'op': 'control', 'session_key': 'agent-1', 'command': 'navigate', 'url': 'https://example.test'})
        self.assertFalse(result['ok'])
        self.assertEqual(result['error_code'], 'action_timeout')
        self.assertTrue(result['detail']['started'])
        self.page.page.goto.assert_awaited_once()
