"""Checks that a damaged or partial Release triggers a rebuild."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
from check_release_assets import missing_assets
from release_targets import TARGETS, zip_name


class ReleaseMetadataTests(unittest.TestCase):
    def test_target_zip_names(self):
        self.assertEqual(len(TARGETS), 5)
        self.assertEqual(len({zip_name(t) for t in TARGETS}), 5)
        with self.assertRaises(ValueError):
            zip_name('unknown')

    def test_empty_and_missing_assets_are_incomplete(self):
        payload = {'assets': [{'name': zip_name(t), 'size': 512} for t in TARGETS]}
        self.assertEqual(missing_assets(payload), [])
        payload['assets'][2]['size'] = 0
        self.assertEqual(missing_assets(payload), [zip_name('win-32')])
        payload['assets'].pop()
        self.assertEqual(set(missing_assets(payload)), {zip_name('win-32'), zip_name('win-arm-64')})

    def test_bad_api_payload_fails_closed(self):
        for payload in (None, [], {}, {'assets': None}, {'assets': [None]},
                        {'assets': [{'name': zip_name('win-32'), 'size': '42'}]}):
            with self.subTest(payload=payload), self.assertRaises(ValueError):
                missing_assets(payload)

    def test_cli_returns_success_only_for_complete_release(self):
        script = ROOT / 'scripts/check_release_assets.py'
        with tempfile.TemporaryDirectory() as td:
            path = Path(td) / 'release.json'
            full = {'assets': [{'name': zip_name(t), 'size': 500} for t in TARGETS]}
            path.write_text(json.dumps(full), encoding='utf8')
            self.assertEqual(subprocess.run([sys.executable, script, path], capture_output=True).returncode, 0)
            full['assets'].pop()
            path.write_text(json.dumps(full), encoding='utf8')
            self.assertNotEqual(subprocess.run([sys.executable, script, path], capture_output=True).returncode, 0)


if __name__ == '__main__':
    unittest.main()
