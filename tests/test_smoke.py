"""Workspace import smoke — Manage / config packages still resolve."""

import unittest


class WorkspaceImportSmokeTest(unittest.TestCase):
    def test_app_config_import(self):
        from app.config import env  # noqa: F401

    def test_manage_package_import(self):
        import manage  # noqa: F401


if __name__ == "__main__":
    unittest.main()
