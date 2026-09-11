"""Focused regression tests for the npm license metadata policy."""

import json
import tempfile
import unittest
from pathlib import Path

from scripts.ci.check_npm_license_policy import ALLOWED_LICENSES, license_value


class LicenseMetadataTests(unittest.TestCase):
    _missing = object()

    def read_license(self, value=_missing):
        with tempfile.TemporaryDirectory() as directory:
            package = Path(directory) / "package.json"
            metadata = {} if value is self._missing else {"license": value}
            package.write_text(json.dumps(metadata), encoding="utf-8")
            return license_value(package)

    def test_string_mit_is_allowed(self):
        value = self.read_license("MIT")
        self.assertEqual(value, "MIT")
        self.assertIn(value, ALLOWED_LICENSES)

    def test_object_mit_is_allowed(self):
        value = self.read_license({"type": "MIT", "url": "https://example.test/license"})
        self.assertEqual(value, "MIT")
        self.assertIn(value, ALLOWED_LICENSES)

    def test_mit_zero_is_allowed(self):
        value = self.read_license("MIT-0")
        self.assertEqual(value, "MIT-0")
        self.assertIn(value, ALLOWED_LICENSES)

    def test_unknown_or_missing_license_is_rejected(self):
        for metadata in ("Custom-License", None, self._missing):
            value = self.read_license(metadata)
            self.assertNotIn(value, ALLOWED_LICENSES)


if __name__ == "__main__":
    unittest.main()
