"""Focused regression tests for the npm license metadata policy."""

import json
import tempfile
import unittest
from pathlib import Path

from scripts.ci.check_npm_license_policy import (
    ALLOWED_LICENSES,
    license_expression_allowed,
    license_value,
)


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

    def test_spdx_or_accepts_an_allowed_option(self):
        self.assertTrue(license_expression_allowed("(MIT OR GPL-3.0-or-later)"))

    def test_spdx_and_requires_every_license_to_be_allowed(self):
        self.assertTrue(license_expression_allowed("(MIT AND Zlib)"))
        self.assertFalse(license_expression_allowed("(MIT AND GPL-3.0-or-later)"))

    def test_unknown_and_malformed_expressions_are_rejected(self):
        for value in (
            "Custom-License",
            "MIT OR",
            "(MIT",
            "GPL-3.0 OR AGPL-3.0",
        ):
            self.assertFalse(license_expression_allowed(value))

    def test_legacy_metadata_aliases_are_normalized(self):
        self.assertTrue(license_expression_allowed("MIT/X11"))
        self.assertTrue(license_expression_allowed("BSD", "duck", "0.1.12"))
        self.assertFalse(license_expression_allowed("BSD", "other-package", "1.0.0"))

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
            self.assertFalse(license_expression_allowed(value))


if __name__ == "__main__":
    unittest.main()
