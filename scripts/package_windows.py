#!/usr/bin/env python3
"""Package one cross-compiled Windows Xray binary in a verified portable ZIP."""
import argparse
from pathlib import Path
import struct
import sys
import zipfile

from release_targets import TARGETS


# Keep release binaries minimal: project documentation stays in the repository.
PACKAGE_CONTENTS = frozenset({"xray.exe", "LICENSE", "BUILD-INFO.txt"})


def pe_machine(binary: bytes) -> int:
    if len(binary) < 0x40 or binary[:2] != b'MZ':
        raise ValueError('Executable lacks an MZ header')
    pe_pos = struct.unpack_from('<I', binary, 0x3C)[0]
    if pe_pos + 6 > len(binary) or binary[pe_pos:pe_pos+4] != b'PE\0\0':
        raise ValueError('Executable lacks a valid PE header')
    return struct.unpack_from('<H', binary, pe_pos+4)[0]


def package(target: str, binary_path: Path, output: Path, upstream: str, commit: str, root: Path):
    if target not in TARGETS:
        raise ValueError(f'Unrecognized target: {target}')
    binary = binary_path.read_bytes()
    got = pe_machine(binary)
    expected = TARGETS[target]
    if got != expected:
        raise ValueError(f'{target}: PE machine 0x{got:04x}, expected 0x{expected:04x}')
    output.parent.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(output, 'w', zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        archive.writestr('xray.exe', binary)
        archive.write(root/'LICENSE', 'LICENSE')
        archive.writestr('BUILD-INFO.txt', f'Upstream: {upstream}\nCommit: {commit}\nTarget: {target}\n'
                         'Xray-WinOptimize non-official experimental build.\n'
                         'Windows 7: adaptive TCP buffer behavior is not verified.\n')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--target', required=True, choices=TARGETS)
    parser.add_argument('--exe', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--upstream', required=True)
    parser.add_argument('--commit', required=True)
    args = parser.parse_args()
    package(args.target, args.exe, args.output, args.upstream, args.commit,
            Path(__file__).resolve().parents[1])
    print(f'Packaged {args.output}')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError) as exc:
        print(f'Packaging error: {exc}', file=sys.stderr)
        sys.exit(1)
