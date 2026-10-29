"""Regression coverage for newest published GitHub Release selection."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

MODULE_PATH = Path(__file__).resolve().parents[1] / "scripts" / "select_latest_release.py"
spec = importlib.util.spec_from_file_location("select_latest_release", MODULE_PATH)
selector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(selector)


def release(tag, when, draft=False, prerelease=False):
    return {"tag_name": tag, "published_at": when, "draft": draft, "prerelease": prerelease}


class ReleaseSelectorTests(unittest.TestCase):
    def test_newest_published_ignores_draft(self):
        payload = [release("v26.9.30", "2026-09-30T12:00:00Z", prerelease=True),
                   release("v26.10.21", "2026-10-21T12:07:00Z"),
                   release("v26.11.1", "2026-11-01T11:00:00Z", draft=True)]
        self.assertEqual(selector.select_latest_release(payload), "v26.10.21")

    def test_newest_prerelease_wins(self):
        payload = [release("v26.9.30", "2026-09-30T00:00:00Z"),
                   release("v26.10.1", "2026-10-01T00:00:00Z", prerelease=True)]
        self.assertEqual(selector.select_latest_release(payload), "v26.10.1")

    def test_empty_and_api_error_object_fail(self):
        for payload in ([], {"message": "rate limit exceeded"}):
            with self.subTest(payload=payload), self.assertRaises(ValueError):
                selector.select_latest_release(payload)

    def test_newest_tag_unsupported_fails_closed(self):
        payload = [release("v26.10.21", "2026-10-21T00:00:00Z"),
                   release("v26.10.21-hotfix", "2026-10-22T00:00:00Z")]
        with self.assertRaisesRegex(ValueError, "unexpected format"):
            selector.select_latest_release(payload)

    def test_cli_selection_from_realistic_json_array(self):
        payload = [release("v26.9.30", "2026-09-30T12:00:00Z", prerelease=True),
                   release("v26.10.21", "2026-10-21T12:07:00Z")]
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "releases.json"
            path.write_text(json.dumps(payload), encoding="utf-8")
            result = subprocess.run([sys.executable, str(MODULE_PATH), str(path)],
                                    capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "v26.10.21")

    def test_corrupt_json_fails_loudly(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "releases.json"
            path.write_text("not json", encoding="utf-8")
            result = subprocess.run([sys.executable, str(MODULE_PATH), str(path)],
                                    capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Release selection failed", result.stderr)


if __name__ == "__main__":
    unittest.main()
