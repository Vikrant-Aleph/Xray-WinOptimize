#!/usr/bin/env python3
"""Apply experimental Windows TCP stack integration to a fresh Xray source tree.

Strict anchors intentionally break on incompatible upstream layout. No fuzz.
"""
import argparse
from pathlib import Path
import shutil


def replace_one(text, old, new, path):
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"{path}: anchor mismatch: expected 1 occurrence, got {count}: {old[:100]!r}")
    return text.replace(old, new, 1)


def apply(root, repository):
    core = root / 'proxy/tun'
    stack = core / 'stack.go'
    tcp = core / 'tcp_lightweight.go'
    distro = root / 'main/distro/all/all.go'
    for f in (stack, tcp, distro, root/'transport/internet/system_dialer.go'):
        if not f.is_file(): raise RuntimeError(f"not Xray-core source: missing {f}")
    if (core/'winpacket_adapter_windows.go').exists():
        raise RuntimeError('already patched; start from fresh Xray source')
    stxt = stack.read_text(encoding='utf-8')
    stxt = replace_one(stxt,'\tRemote bool\n}', '\tRemote bool\n\t// AdaptiveReceive enables Linux-inspired receive-window growth for active dials.\n\tAdaptiveReceive bool\n}', stack)
    stxt = replace_one(stxt,'\twindow int32\n','\twindow int32\n\tadaptive bool\n',stack)
    stxt = replace_one(stxt,'\t\twindow:    tcpWindow,','\t\twindow:    tcpWindow,\n\t\tadaptive:  options.AdaptiveReceive,',stack)
    stxt = replace_one(stxt,'\tif options.Remote {\n\t\ts.window = tcpLargeWindow\n\t}', '\tif options.Remote {\n\t\ts.window = tcpLargeWindow\n\t}\n\tif options.AdaptiveReceive { s.window = 256 << 10 }',stack)

    ttxt=tcp.read_text(encoding='utf-8')
    ttxt=replace_one(ttxt,'\twindow int32\n','\twindow int32\n\tadaptive bool\n\tadaptiveController rxWindowController\n\tadaptiveDelivered uint64\n',tcp)
    ttxt=replace_one(ttxt,'\t\twindow:          s.window,','\t\twindow:          s.window,\n\t\tadaptive:        s.adaptive,\n\t\tadaptiveController: newRXWindowController(),',tcp)
    # Initial window scale is selected BEFORE the initial SYN; 8 supports 8MiB.
    ttxt=replace_one(ttxt,'\tc.sack, c.stamps, c.rcvShift, c.opened = true, s.stamps, tcpWindowShift, opened', '\tc.sack, c.stamps, c.rcvShift, c.opened = true, s.stamps, tcpWindowShift, opened\n\tif c.adaptive { c.rcvShift = 8 }',tcp)
    ttxt=replace_one(ttxt,'\t\t\tc.rcvShift = tcpWindowShift', '\t\t\tc.rcvShift = tcpWindowShift\n\t\t\tif c.adaptive { c.rcvShift = 8 }',tcp)
    ttxt=replace_one(ttxt,'\tc.rcvLen -= size\n\t// the system may also have stopped at the window that it was told\n\tif c.err == nil && (c.rcvAcked != c.rcvNxt || c.rcvAdvertised < c.window/2) {', '\tc.rcvLen -= size\n\tgrew := false\n\tif c.adaptive { grew = c.adjustAdaptiveReceive(size) }\n\t// the system may also have stopped at the window that it was told\n\tif c.err == nil && (grew || c.rcvAcked != c.rcvNxt || c.rcvAdvertised < c.window/2) {',tcp)
    # Fail a user-mode TCP connection immediately if WinDivert cannot reserve/capture its flow.
    ttxt=replace_one(ttxt,'\t_ = c.stack.tun.WritePacket(h, payload)', '\tif err := c.stack.tun.WritePacket(h, payload); err != nil && c.adaptive { c.abort(err) }',tcp)
    ttxt=replace_one(ttxt,'if len(payload) == 0 || len(c.rcvHeld) == tcpHeld {','if len(payload) == 0 || (len(c.rcvHeld) >= tcpHeld && !c.adaptive) || len(c.rcvHeld) >= 4096 {',tcp)
    dtxt = distro.read_text(encoding='utf-8')
    if '_ "github.com/xtls/xray-core/proxy/tun"' not in dtxt:
        dtxt = replace_one(dtxt, '_ "github.com/xtls/xray-core/proxy/socks"', '_ "github.com/xtls/xray-core/proxy/socks"\n\t_ "github.com/xtls/xray-core/proxy/tun"', distro)
    for f in [stack,tcp,distro]:
        backup = f.with_suffix(f.suffix+'.tcpbackend.orig')
        shutil.copy2(f,backup)
    stack.write_text(stxt,encoding='utf-8')
    tcp.write_text(ttxt,encoding='utf-8')
    distro.write_text(dtxt,encoding='utf-8')
    # Copy the WinDivert packet layer as a separate component within the tun package.
    adapter=(repository/'adapter/packet_windows.go').read_text(encoding='utf-8')
    adapter=adapter.replace('package adapter\n','package tun\n',1)
    adapter=adapter.replace('func New() *PacketAdapter','func NewWinDivertPacketAdapter() *PacketAdapter',1)
    (core/'winpacket_adapter_windows.go').write_text(adapter,encoding='utf-8')
    shutil.copy2(repository/'patch/dialer_windows.go',core/'dialer_experimental_windows.go')
    # Generate an embedded copy of the tested pure-Go receive-window controller.
    controller=(repository/'recvwindow/window.go').read_text(encoding='utf-8')
    for a,b in [('package recvwindow','package tun'),('Initial','rxInitial'),('Maximum','rxMaximum'),('Controller','rxWindowController'),('func New()','func newRXWindowController()')]:
        controller=controller.replace(a,b)
    # Constructor line after Controller replacement is 'func newRXWindowController() rxWindowController'
    controller+='''
// adjustAdaptiveReceive is called under c.mu AFTER application data is consumed.
// Growing window without shrinking prevents invalidating earlier advertised ranges.
func (c *tcpConn) adjustAdaptiveReceive(size int32) bool {
    if size <= 0 || c.err != nil { return false }
    c.adaptiveDelivered += uint64(size)
    rtt := c.srtt
    if rtt <= 0 { rtt = 150 * time.Millisecond }
    var ooo int32
    if len(c.rcvHeld)>0 {
        last:=c.rcvHeld[len(c.rcvHeld)-1]
        ooo = int32(last.seq + uint32(last.data.Len()) - c.rcvNxt)
        if ooo<0 {ooo=0}
    }
    next, grew := c.adaptiveController.Observe(time.Now(),rtt,c.adaptiveDelivered,ooo)
    if grew && next>c.window {c.window=next;return true}
    return false
}
'''
    (core/'recvwindow_adaptive.go').write_text(controller,encoding='utf-8')
    print('Patch applied (EXPERIMENTAL). Run gofmt and go build on Windows. Backups have .tcpbackend.orig suffix.')

if __name__=='__main__':
    cli=argparse.ArgumentParser()
    cli.add_argument('xray_source',type=Path)
    args=cli.parse_args()
    apply(args.xray_source.resolve(),Path(__file__).resolve().parents[1])
