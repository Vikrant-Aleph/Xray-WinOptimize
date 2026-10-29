#!/usr/bin/env python3
"""Fail a PR/push when implementation changes are missing TECHNICAL.md updates.

This is a *file-level* gate, not a review of documentation correctness.
Use --base <commit> [--head <commit>] for GitHub Actions. For local checks
against uncommitted work, use --staged or --working-tree.
"""

import argparse
import subprocess
import sys
from pathlib import Path

TECHNICAL = 'docs/TECHNICAL.md'
CODE_PREFIXES = (
    'adapter/',
    'portable/',
    'recvwindow/',
    'patch/',
    'scripts/',
    'tests/',
    '.github/workflows/',
)
CODE_FILES = {'go.mod', 'go.sum', 'docs/INSTALL-WINDOWS.md'}


def needs_technical_update(path: str) -> bool:
    return path in CODE_FILES or any(path.startswith(p) for p in CODE_PREFIXES)


def evaluate(changed: list[str]) -> tuple[bool, list[str]]:
    changed = sorted({p.replace('\\', '/') for p in changed if p})
    affected = [path for path in changed if needs_technical_update(path)]
    return not affected or TECHNICAL in changed, affected


def git_output(*args: str) -> list[str]:
    cp = subprocess.run(['git', *args], check=True, stdout=subprocess.PIPE,
                        stderr=subprocess.PIPE, text=True)
    return [s for s in cp.stdout.splitlines() if s]


def changed_files(args: argparse.Namespace) -> list[str]:
    if args.staged:
        return git_output('diff', '--cached', '--name-only', '--diff-filter=ACMR')
    if args.working_tree:
        return git_output('diff', '--name-only', '--diff-filter=ACMR')
    if not args.base:
        raise ValueError('specify --base, --staged or --working-tree')
    if len(args.base) == 40 and set(args.base) == {'0'}:
        # First push to a new repository. Every file in the initial commit
        # is considered added, so TECHNICAL.md must already be present.
        return git_output('ls-tree', '-r', '--name-only', args.head)
    return git_output('diff', '--name-only', '--diff-filter=ACMR', args.base, args.head)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument('--base', help='base SHA for a push/PR comparison')
    source.add_argument('--staged', action='store_true', help='check staged diff')
    source.add_argument('--working-tree', action='store_true', help='check unstaged diff')
    parser.add_argument('--head', default='HEAD', help='end ref (default HEAD)')
    args = parser.parse_args()
    try:
        changed = changed_files(args)
    except (ValueError, subprocess.CalledProcessError) as exc:
        print(f'Cannot verify doc synchronization: {exc}', file=sys.stderr)
        return 2
    ok, affected = evaluate(changed)
    if not ok:
        print('ERROR: Implementation/build/test/install changes require an update '
              'to docs/TECHNICAL.md in the SAME change set.', file=sys.stderr)
        print('Changed files requiring documentation:', file=sys.stderr)
        for name in affected:
            print(f'  - {name}', file=sys.stderr)
        print('Update the affected section AND §13 change log; then commit both.',
              file=sys.stderr)
        return 1
    if affected:
        print(f'Documentation gate passed: {len(affected)} technical file(s) and '
              f'{TECHNICAL} changed.')
    else:
        print('Documentation gate passed: no implementation/build/install changes.')
    return 0


if __name__ == '__main__':
    sys.exit(main())
