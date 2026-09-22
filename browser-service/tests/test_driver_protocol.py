import unittest

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
