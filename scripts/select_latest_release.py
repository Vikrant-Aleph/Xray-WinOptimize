#!/usr/bin/env python3
"""Select newest published Xray GitHub Release from the raw JSON API response."""

import argparse
from datetime import datetime
import json
from pathlib import Path
import re
import sys

TAG_PATTERN = re.compile(r"^v[0-9]+\.[0-9]+\.[0-9]+$")


def select_latest_release(payload: object) -> str:
    if not isinstance(payload, list):
        raise ValueError("GitHub API returned non-array JSON")
    valid = []
    for item in payload:
        if not isinstance(item, dict) or item.get("draft") is not False:
            continue
        published_at, tag = item.get("published_at"), item.get("tag_name")
        if not isinstance(published_at, str) or not isinstance(tag, str):
            continue
        try:
            timestamp = datetime.fromisoformat(published_at.replace("Z", "+00:00"))
        except ValueError:
            continue
        if timestamp.tzinfo is not None:
            valid.append((timestamp, tag))
    if not valid:
        raise ValueError("No published upstream Xray releases found in API response")
    _timestamp, tag = max(valid, key=lambda value: value[0])
    if not TAG_PATTERN.fullmatch(tag):
        raise ValueError(f"Latest upstream tag has unexpected format: {tag!r}")
    return tag


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("releases_json", type=Path)
    args = parser.parse_args()
    try:
        payload = json.loads(args.releases_json.read_text(encoding="utf-8-sig"))
        tag = select_latest_release(payload)
    except (OSError, ValueError) as exc:
        print(f"Release selection failed: {exc}", file=sys.stderr)
        return 1
    print(tag)
    return 0


if __name__ == "__main__":
    sys.exit(main())
