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
