from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]

class PublicNamingTests(unittest.TestCase):
    def test_default_workflow_and_executable_names(self):
        text = (ROOT/'.github/workflows/upstream-release.yml').read_text(encoding='utf-8')
        self.assertIn('name: Xray-WinOptimize (latest upstream)', text)
        self.assertIn('xray-winopt.exe', text)
        self.assertIn('name: xray-winopt-${{ steps.upstream.outputs.tag }}', text)
        self.assertNotIn('xray-winopt-portable', text)
    def test_readme_uses_stable_executable_name(self):
        for path in ['README.md', 'README.en.md']:
            text = (ROOT/path).read_text(encoding='utf-8')
            self.assertIn('xray-winopt.exe', text)
            self.assertNotIn('xray-winopt-portable', text)

if __name__=='__main__': unittest.main()
