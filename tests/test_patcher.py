import pathlib
import subprocess
import tempfile
import unittest

REPO=pathlib.Path(__file__).resolve().parents[1]

class PatchTest(unittest.TestCase):
    def test_mock_upstream_anchors_and_output_syntax(self):
        with tempfile.TemporaryDirectory() as tmp:
            r=pathlib.Path(tmp)
            (r/'proxy/tun').mkdir(parents=True)
            (r/'transport/internet').mkdir(parents=True)
            (r/'main/distro/all').mkdir(parents=True)
            (r/'main/distro/all/all.go').write_text('package all\nimport (\n\t_ \"github.com/xtls/xray-core/proxy/socks\"\n)\n')
            (r/'transport/internet/system_dialer.go').write_text('package internet\n')
            (r/'proxy/tun/stack.go').write_text('''package tun
 type opts struct{\n\tRemote bool\n}
type stack struct{\n\twindow int32\n}
func NewStack(options opts)*stack{\n s:= &stack{\n\t\twindow:    tcpWindow,\n}\n\tif options.Remote {\n\t\ts.window = tcpLargeWindow\n\t}\nreturn s\n}
''')
            (r/'proxy/tun/tcp_lightweight.go').write_text('''package tun
 type tcpConn struct {\n\twindow int32\n}
func newTCPConn(s *stack)*tcpConn{c:=&tcpConn{\n\t\twindow:          s.window,\n};return c}
func (c *tcpConn)synchronize(){\n\t\t\tc.rcvShift = tcpWindowShift\n}
func (s *stack)DialTCP(){\n\tc.sack, c.stamps, c.rcvShift, c.opened = true, s.stamps, tcpWindowShift, opened\n}
func (c *tcpConn)taken(size int32) {\n\tc.rcvLen -= size\n\t// the system may also have stopped at the window that it was told\n\tif c.err == nil && (c.rcvAcked != c.rcvNxt || c.rcvAdvertised < c.window/2) {\n}\n}
func (c *tcpConn)write(h []byte,payload []byte){\n\t_ = c.stack.tun.WritePacket(h, payload)\n}
func (c *tcpConn)held(){\nif len(payload) == 0 || len(c.rcvHeld) == tcpHeld {\n}\n}
''')
            cmd=['python3',str(REPO/'patch/apply.py'),str(r)]
            p=subprocess.run(cmd,capture_output=True,text=True)
            self.assertEqual(0,p.returncode,p.stderr)
            for source in (r/'proxy/tun').glob('*.go'):
                fmt=subprocess.run(['gofmt',str(source)],capture_output=True,text=True)
                self.assertEqual(0,fmt.returncode,(source.name,fmt.stderr))
            self.assertIn('adaptiveController', (r/'proxy/tun/tcp_lightweight.go').read_text())
            self.assertIn('proxy/tun', (r/'main/distro/all/all.go').read_text())
            self.assertIn('experimentalTCPDialer', (r/'proxy/tun/dialer_experimental_windows.go').read_text())
            self.assertNotEqual(0,subprocess.run(cmd,capture_output=True).returncode)

if __name__=='__main__':unittest.main()
