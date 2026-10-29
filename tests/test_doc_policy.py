"""Tests for the technical document update gate."""
import importlib.util
from pathlib import Path
import unittest

MOD = Path(__file__).with_name('check_technical_doc.py')
spec = importlib.util.spec_from_file_location('check_technical_doc', MOD)
doc_gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(doc_gate)


class DocGateTests(unittest.TestCase):
    def test_source_requires_doc(self):
        ok, affected = doc_gate.evaluate(['recvwindow/window.go'])
        self.assertFalse(ok)
        self.assertEqual(['recvwindow/window.go'], affected)

    def test_source_and_document_pass(self):
        ok, _ = doc_gate.evaluate(['patch/apply.py', 'docs/TECHNICAL.md'])
        self.assertTrue(ok)

    def test_workflow_change_requires_doc(self):
        self.assertFalse(doc_gate.evaluate(['.github/workflows/upstream-release.yml'])[0])

    def test_installation_change_requires_doc(self):
        self.assertFalse(doc_gate.evaluate(['docs/INSTALL-WINDOWS.md'])[0])

    def test_readme_only_is_allowed(self):
        self.assertTrue(doc_gate.evaluate(['README.md', 'README.en.md'])[0])

    def test_doc_only_allowed(self):
        self.assertTrue(doc_gate.evaluate(['docs/TECHNICAL.md'])[0])

    def test_windows_separators_normalized(self):
        self.assertTrue(doc_gate.evaluate(['adapter\\packet_windows.go', 'docs\\TECHNICAL.md'])[0])

    def test_tests_and_module_need_doc(self):
        self.assertFalse(doc_gate.evaluate(['tests/test_patcher.py', 'go.mod'])[0])


if __name__ == '__main__':
    unittest.main()
