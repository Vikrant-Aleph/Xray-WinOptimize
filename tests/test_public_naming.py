"""Check that public workflow and README names match the five-target release layout."""
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]


class PublicNamingTests(unittest.TestCase):
    def test_default_workflow_and_executable_names(self):
        text = (ROOT / '.github/workflows/upstream-release.yml').read_text(encoding='utf-8')
        self.assertIn('name: Xray-WinOptimize (latest upstream)', text)
        self.assertIn('-o ../build/xray.exe', text)
        self.assertIn('--exe build/xray.exe', text)
        self.assertIn('dist/Xray-winopt-${TARGET}.zip', text)
        self.assertIn('scripts/verify_release.py', text)
        self.assertNotIn('xray-winopt.exe', text)
        self.assertNotIn('xray-winopt-portable', text)

    def test_readme_uses_stable_executable_name(self):
        for path in ['README.md', 'README.en.md']:
            with self.subTest(path=path):
                text = (ROOT / path).read_text(encoding='utf-8')
                self.assertIn('xray.exe', text)
                self.assertNotIn('xray-winopt.exe', text)
                self.assertNotIn('xray-winopt-portable', text)


if __name__ == '__main__':
    unittest.main()
