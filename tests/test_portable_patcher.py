import importlib.util
from pathlib import Path
import tempfile
import unittest

repo=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('apply_portable', repo/'patch/apply_portable.py')
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

DIALER='''package internet
import "syscall"
func f() {
 if sockopt != nil || len(Controllers) > 0 { // UDP
 }
 if sockopt != nil || len(Controllers) > 0 {
    dialer.Control = func(network, address string, c syscall.RawConn) error {
        return nil
    }
 }
 return dialer.DialContext(ctx, dest.Network.SystemString(), dest.NetAddr())
}
'''

class PortablePatcherTests(unittest.TestCase):
    def test_replaces_only_tcp_anchor(self):
        changed=module.transform(DIALER)
        self.assertEqual(changed.count('|| winOptEnabled('),1)
        self.assertIn('winOptBeforeConnect(',changed)
        self.assertIn('winOptApplyTCPOpts(',changed)
        self.assertIn('winOptAfterConnect(',changed)
        self.assertIn('winOptDialFailed(',changed)
        self.assertIn('winOptConnectState',changed)
    def test_rejects_upstream_anchor_change(self):
        with self.assertRaises(RuntimeError):
            module.transform(DIALER.replace('dialer.Control = func','dialer.ControlContext = func'))
    def test_rejects_double_patching(self):
        with self.assertRaises(RuntimeError):
            module.transform(module.transform(DIALER))
    def test_apply_and_reject_duplicate(self):
        with tempfile.TemporaryDirectory() as d:
            root=Path(d)
            (root/'go.mod').write_text('module fake\n')
            file=root/'transport/internet/system_dialer.go'
            file.parent.mkdir(parents=True)
            file.write_text(DIALER)
            module.apply(root,repo)
            self.assertEqual((file.parent/'system_dialer.go.winopt.orig').read_text(),DIALER)
            self.assertTrue((file.parent/'winopt_socket_windows.go').exists())
            self.assertTrue((file.parent/'winopt_quota.go').exists())
            self.assertTrue((file.parent/'winopt_history.go').exists())
            self.assertIn('!winOptPreconnectCapacityEnabled(os.Getenv("XRAY_WINOPT_PRECONNECT_CACHE"))', (file.parent/'winopt_socket_windows.go').read_text())
            self.assertTrue((file.parent/'winopt_sockopts_other.go').exists())
            with self.assertRaises(RuntimeError):module.apply(root,repo)

if __name__=='__main__':unittest.main()
