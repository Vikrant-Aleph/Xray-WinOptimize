#!/usr/bin/env python3
"""Before publishing, verify all ZIP assets contain target-matching PE binaries."""
import argparse
import hashlib
from pathlib import Path
import sys
import zipfile

from package_windows import PACKAGE_CONTENTS, pe_machine
from release_targets import TARGETS, zip_name


def verify(directory: Path, checksums: Path | None = None):
    names = {zip_name(t) for t in TARGETS}
    existing = {p.name for p in directory.glob('*.zip')}
    if existing != names:
        raise ValueError(f'Expected exactly 5 ZIPs; missing={sorted(names-existing)}; unexpected={sorted(existing-names)}')
    results = []
    for target, expected in TARGETS.items():
        path = directory/zip_name(target)
        with zipfile.ZipFile(path) as archive:
            n = archive.namelist()
            if len(n) != len(set(n)) or set(n) != PACKAGE_CONTENTS:
                raise ValueError(f'{path.name}: ZIP contents must be exactly {sorted(PACKAGE_CONTENTS)}; got {sorted(n)}')
            machine = pe_machine(archive.read('xray.exe'))
            if machine != expected:
                raise ValueError(f'{path.name}: wrong PE machine 0x{machine:x}')
            if f'Target: {target}' not in archive.read('BUILD-INFO.txt').decode('utf8'):
                raise ValueError(f'{path.name}: BUILD-INFO target mismatch')
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        results.append(f'{digest}  {path.name}')
    if checksums:
        checksums.write_text('\n'.join(sorted(results))+'\n', encoding='utf8')
    print('Verified five Windows ZIPs, PE architectures, and checksums.')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('directory', type=Path)
    parser.add_argument('--checksums', type=Path)
    args = parser.parse_args()
    try:
        verify(args.directory, args.checksums)
    except (ValueError, OSError, zipfile.BadZipFile) as err:
        print(f'Release validation failed: {err}', file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()
