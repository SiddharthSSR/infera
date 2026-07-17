#!/usr/bin/env python3
"""Create a new regular evidence file exclusively with mode 0600."""

from __future__ import annotations

import os
import stat
import sys


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: create-private-evidence.py <path>", file=sys.stderr)
        return 2
    if not hasattr(os, "O_NOFOLLOW"):
        print("ERROR: O_NOFOLLOW is required for private evidence creation", file=sys.stderr)
        return 1

    path = sys.argv[1]
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW
    try:
        descriptor = os.open(path, flags, 0o600)
    except OSError as exc:
        print(f"ERROR: unable to create private evidence: {exc}", file=sys.stderr)
        return 1

    try:
        if not stat.S_ISREG(os.fstat(descriptor).st_mode):
            print("ERROR: evidence target is not a regular file", file=sys.stderr)
            return 1
        os.fchmod(descriptor, 0o600)
    finally:
        os.close(descriptor)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
