#!/usr/bin/env python3
"""Skip rebuilds only when all five release ZIP assets have nonzero sizes."""

import argparse
import json
from pathlib import Path
import sys

from release_targets import TARGETS, zip_name


def missing_assets(release: dict) -> list[str]:
    """Return required ZIP names that are absent, empty, or malformed.

    Unexpected other release assets (such as SHA256SUMS.txt) are allowed.
    Malformed GitHub API payloads fail closed instead of skipping a build.
    """
    if not isinstance(release, dict):
        raise ValueError("Expected GitHub Release JSON object")
    assets = release.get("assets")
    if not isinstance(assets, list):
        raise ValueError("Expected an assets array in GitHub Release JSON")

    available = set()
    for asset in assets:
        if not isinstance(asset, dict):
            raise ValueError("Invalid GitHub Release asset object")
        name = asset.get("name")
        size = asset.get("size")
        if not isinstance(name, str) or not isinstance(size, int) or isinstance(size, bool) or size < 0:
            raise ValueError("Invalid GitHub Release asset name or size")
        if size > 0:
            available.add(name)

    return [zip_name(target) for target in TARGETS if zip_name(target) not in available]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("release_json", type=Path)
    args = parser.parse_args()
    try:
        release = json.loads(args.release_json.read_text(encoding="utf-8"))
        missing = missing_assets(release)
    except (OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"Cannot validate release assets: {exc}", file=sys.stderr)
        return 1

    if missing:
        print("Missing or empty release assets: " + ", ".join(missing))
        return 1
    print("All five Windows release ZIPs exist and are non-empty.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
