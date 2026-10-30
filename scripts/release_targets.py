#!/usr/bin/env python3
"""Single source of truth for the five Windows release targets and PE machines."""

# IMAGE_FILE_MACHINE_I386, AMD64, ARM64 from the Windows PE specification.
# Windows 7 targets use a patched Go toolchain but retain the same PE machine
# types as their modern-Windows counterparts.
TARGETS = {
    "win7-32": 0x014C,
    "win7-64": 0x8664,
    "win-32": 0x014C,
    "win-64": 0x8664,
    "win-arm-64": 0xAA64,
}


def zip_name(target: str) -> str:
    """Return the public ZIP filename for a known build target."""
    if target not in TARGETS:
        raise ValueError(f"Unknown Windows release target: {target}")
    return f"Xray-winopt-{target}.zip"
