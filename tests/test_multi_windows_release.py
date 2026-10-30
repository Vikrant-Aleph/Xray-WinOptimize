"""All-five-platform publishing gates; prevent wrong-architecture or partial uploads."""
import importlib.util
import json
from pathlib import Path
import struct
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def script(name):
    import sys
    sys.path.insert(0,str(ROOT/'scripts'))
    spec = importlib.util.spec_from_file_location(name, ROOT/'scripts'/f'{name}.py')
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


release_targets = script('release_targets')
package = script('package_windows')
checker = script('check_release_assets')
verifier = script('verify_release')


def fake_pe(machine):
    blob = bytearray(128)
    blob[:2] = b'MZ'
    struct.pack_into('<I',blob,0x3C,64)
    blob[64:68]=b'PE\0\0'
    struct.pack_into('<H',blob,68,machine)
    return bytes(blob)


class MultiWindowsReleaseTests(unittest.TestCase):
    def test_required_names_and_arches(self):
        self.assertEqual(release_targets.TARGETS, {
            'win7-32':0x014c,'win7-64':0x8664,'win-32':0x014c,
            'win-64':0x8664,'win-arm-64':0xaa64,
        })

    def test_skip_only_if_all_five_complete(self):
        all_assets={'assets':[{'name':release_targets.zip_name(x),'size':500} for x in release_targets.TARGETS]}
        self.assertEqual(checker.missing_assets(all_assets), [])
        all_assets['assets'][0]['size']=0
        self.assertEqual(len(checker.missing_assets(all_assets)),1)
        with self.assertRaises(ValueError):
            checker.missing_assets({'assets':None})

    def test_all_archives_validate_and_wrong_binary_rejected(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d)
            exe=root/'xray.exe'
            out=root/'dist'
            for target, machine in release_targets.TARGETS.items():
                exe.write_bytes(fake_pe(machine))
                package.package(target,exe,out/release_targets.zip_name(target),'v26.10.21','deadbeef',ROOT)
            verifier.verify(out,root/'SHA256SUMS.txt')
            for target in release_targets.TARGETS:
                with zipfile.ZipFile(out/release_targets.zip_name(target)) as archive:
                    self.assertEqual(set(archive.namelist()), {'xray.exe', 'LICENSE', 'BUILD-INFO.txt'})
                    self.assertNotIn('README-project.zh-CN.md', archive.namelist())
                    self.assertNotIn('README-project.en.md', archive.namelist())
                    self.assertNotIn('TECHNICAL.md', archive.namelist())
            self.assertEqual(len((root/'SHA256SUMS.txt').read_text().splitlines()),5)
            # Fail closed if old build scripts accidentally include documentation.
            archive_path = out/release_targets.zip_name('win-64')
            with zipfile.ZipFile(archive_path, 'a') as archive:
                archive.writestr('TECHNICAL.md', 'must remain in the GitHub repository')
            with self.assertRaisesRegex(ValueError, 'ZIP contents must be exactly'):
                verifier.verify(out)
            exe.write_bytes(fake_pe(release_targets.TARGETS['win-64']))
            package.package('win-64', exe, archive_path, 'v26.10.21', 'deadbeef', ROOT)
            # Rebuilt archive is minimal again.
            verifier.verify(out)
            exe.write_bytes(fake_pe(0x014c))
            with self.assertRaisesRegex(ValueError, 'PE machine'):
                package.package('win-arm-64',exe,out/'bad.zip','v26.10.21','deadbeef',ROOT)
            (out/release_targets.zip_name('win-32')).unlink()
            with self.assertRaisesRegex(ValueError,'missing'):
                verifier.verify(out)

    def test_workflow_contains_release_matrix_and_fail_closed_gate(self):
        workflow=(ROOT/'.github/workflows/upstream-release.yml').read_text(encoding='utf8')
        for target in release_targets.TARGETS:
            self.assertIn(f'target: {target}',workflow)
        self.assertIn('cron:',workflow)
        self.assertIn('*/2 * *',workflow)
        self.assertIn('gh release create',workflow)
        self.assertIn('--draft',workflow)
        self.assertIn('scripts/verify_release.py',workflow)
        self.assertIn('go-for-win7-linux-amd64.zip',workflow)
        self.assertIn('name: Xray-WinOptimize (latest upstream)',workflow)
        self.assertIn('force_rebuild:',workflow)
        self.assertIn('FORCE_REBUILD:',workflow)


if __name__=='__main__':
    unittest.main()
