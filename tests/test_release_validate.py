import unittest

from scripts.release.validate_release import has_current_version_marker
from scripts.release.prepare_release import (
    ROADMAP_RELEASE,
    update_readme_current_version_marker,
)


class CurrentVersionMarkerTests(unittest.TestCase):
    def test_accepts_current_version_wording(self) -> None:
        self.assertTrue(has_current_version_marker("当前版本为 **v0.11.0**。", "0.11.0"))

    def test_accepts_current_release_version_wording(self) -> None:
        self.assertTrue(has_current_version_marker("当前发布版本为 **v0.11.0**。", "0.11.0"))

    def test_rejects_a_different_version(self) -> None:
        self.assertFalse(has_current_version_marker("当前发布版本为 **v0.11.0**。", "0.11.1"))

    def test_release_preparation_rewrites_both_marker_phrasings(self) -> None:
        for wording in ("当前版本为", "当前发布版本为"):
            with self.subTest(wording=wording):
                readme = f"{wording} **v0.11.0**。"
                updated = update_readme_current_version_marker(readme, "0.11.1")
                self.assertEqual(updated, "当前版本为 **v0.11.1**。")
                self.assertNotIn("v0.11.0", updated)


class RoadmapReleaseMarkerTests(unittest.TestCase):
    def test_matches_previous_next_version_wording(self) -> None:
        roadmap = "下一版建议为 v0.12.0，进入候选验收。"
        updated, count = ROADMAP_RELEASE.subn("准备发布 v1.0.0", roadmap, count=1)
        self.assertEqual(count, 1)
        self.assertEqual(updated, "准备发布 v1.0.0，进入候选验收。")

    def test_matches_explicit_release_wording(self) -> None:
        roadmap = "准备发布 v0.12.0。"
        updated, count = ROADMAP_RELEASE.subn("准备发布 v1.0.0", roadmap, count=1)
        self.assertEqual(count, 1)
        self.assertEqual(updated, "准备发布 v1.0.0。")

    def test_updates_current_release_baseline(self) -> None:
        roadmap = "## 当前发布基线（v1.0.0）"
        updated, count = ROADMAP_RELEASE.subn(
            lambda match: f"{match.group('prefix')}v1.1.0", roadmap, count=1
        )
        self.assertEqual(count, 1)
        self.assertEqual(updated, "## 当前发布基线（v1.1.0）")


if __name__ == "__main__":
    unittest.main()
