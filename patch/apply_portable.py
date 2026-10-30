#!/usr/bin/env python3
"""Apply the no-driver Windows TCP autotuning patch to clean Xray.

Strict anchors: fail if an upstream release changes the dialer structure.
Does not apply the independent WinDivert experimental TCP-stack patch.
"""
from pathlib import Path
import argparse
import shutil


def replace_one(text: str, needle: str, replacement: str) -> str:
    found = text.count(needle)
    if found != 1:
        raise RuntimeError(f"upstream dialer anchor changed (expected 1, found {found}): {needle[:100]!r}")
    return text.replace(needle, replacement, 1)


def transform(source: str) -> str:
    if 'winOptAfterConnect(' in source:
        raise RuntimeError('Portable patch already present; use a fresh upstream checkout')
    flag = 'if sockopt != nil || len(Controllers) > 0 {'
    if source.count(flag) != 2:
        raise RuntimeError(f'expected exactly two UDP/TCP condition anchors, found {source.count(flag)}')
    at = source.rfind(flag)
    source = source[:at] + 'var winOptAttempt winOptConnectState\n    ' + flag.replace(' {', ' || winOptEnabled(dest.Network.SystemString()) {') + source[at+len(flag):]
    source = replace_one(source,
        'dialer.Control = func(network, address string, c syscall.RawConn) error {',
        '''dialer.Control = func(network, address string, c syscall.RawConn) error {
            winOptApplyTCPOpts(c, network, address)
            if !winOptExplicitRcvbuf(sockopt) { winOptBeforeConnect(c, network, address, &winOptAttempt) }''')
    source = replace_one(source,
        'dialer.Control = func(network, address string, c syscall.RawConn) error {',
        'dialer.Control = func(network, address string, c syscall.RawConn) error {') if False else source
    source = replace_one(source,
        'return dialer.DialContext(ctx, dest.Network.SystemString(), dest.NetAddr())',
        '''conn, dialErr := dialer.DialContext(ctx, dest.Network.SystemString(), dest.NetAddr())
    if dialErr == nil {
        winOptAfterConnect(conn, dest.Network.SystemString(), dest.NetAddr(), winOptExplicitRcvbuf(sockopt), &winOptAttempt)
    } else {
        winOptDialFailed(&winOptAttempt)
    }
    return conn, dialErr''')
    return source


def apply(root: Path, repo: Path):
    dialer = root/'transport/internet/system_dialer.go'
    if not dialer.is_file() or not (root/'go.mod').is_file():
        raise RuntimeError('not Xray source: transport/internet/system_dialer.go or go.mod missing')
    destdir = dialer.parent
    files = ['controller.go', 'quota.go', 'history.go', 'socket_windows.go', 'socket_other.go']
    for filename in files:
        if (destdir/('winopt_'+filename)).exists():
            raise RuntimeError(f'patch previously applied, or conflicting file: {filename}')
    source = dialer.read_text(encoding='utf-8')
    patched = transform(source)
    # Validate all inputs before any source edits.
    pieces = {}
    for filename in files:
        f = repo/'portable'/filename
        txt = f.read_text(encoding='utf-8')
        if txt.count('package portable') != 1:
            raise RuntimeError(f'unexpected Go package declaration: {filename}')
        pieces[filename] = txt.replace('package portable','package internet',1)
    helpers = '''//go:build windows

package internet

import (
    "strconv"
    "strings"
)

// An existing user-defined SO_RCVBUF always wins over portable policy.
func winOptExplicitRcvbuf(sockopt *SocketConfig) bool {
    if sockopt == nil {return false}
    for _, opt := range sockopt.CustomSockopt {
        if opt == nil {continue}
        if opt.System != "" && !strings.EqualFold(opt.System,"windows") {continue}
        level,e1 := strconv.Atoi(opt.Level)
        name,e2 := strconv.Atoi(opt.Opt)
        if e1 == nil && e2 == nil && level==65535 && name==4098 {return true}
    }
    return false
}
'''
    helper_other = '''//go:build !windows

package internet

func winOptExplicitRcvbuf(sockopt *SocketConfig) bool {return false}
'''
    backup = dialer.with_suffix('.go.winopt.orig')
    if backup.exists():
        raise RuntimeError(f'backup already exists, refusing to overwrite: {backup}')
    shutil.copy2(dialer, backup)
    dialer.write_text(patched,encoding='utf-8')
    for filename,code in pieces.items():
        (destdir/('winopt_'+filename)).write_text(code,encoding='utf-8')
    (destdir/'winopt_sockopts_windows.go').write_text(helpers,encoding='utf-8')
    (destdir/'winopt_sockopts_other.go').write_text(helper_other,encoding='utf-8')
    print('Applied Windows TCP receive controller; default path-metrics cache, no preconnect SO_RCVBUF, no driver.')

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('xray_source',type=Path)
    opts=parser.parse_args()
    apply(opts.xray_source.resolve(),Path(__file__).resolve().parents[1])
