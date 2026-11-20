#!/usr/bin/env python3
"""Validate Raft history without treating a zero-filled append as a bad member."""

import hashlib
import os
import re
import subprocess
import sys
import tempfile


CHUNK_SIZE = 64 * 1024
HEADER = re.compile(rb"OVSDB CLUSTER ([1-9][0-9]*) ([0-9a-f]{40})\n")


def zero_tail_offset(source):
    """Accept only complete checksummed records followed exclusively by zeros."""
    offset = 0
    while True:
        header = source.readline(128)
        if not header:
            return None
        if header.startswith(b"\0"):
            if not offset or any(header):
                return None
            while chunk := source.read(CHUNK_SIZE):
                if any(chunk):
                    return None
            return offset
        match = HEADER.fullmatch(header)
        if not match:
            return None
        remaining = int(match[1])
        digest = hashlib.sha1(usedforsecurity=False)
        while remaining:
            chunk = source.read(min(remaining, CHUNK_SIZE))
            if not chunk:
                return None
            digest.update(chunk)
            remaining -= len(chunk)
        if digest.hexdigest().encode() != match[2]:
            return None
        offset = source.tell()


def file_identity(stat):
    return stat.st_dev, stat.st_ino, stat.st_size, stat.st_mtime_ns, stat.st_ctime_ns


def validate_zero_tail(path):
    with open(path, "rb") as source:
        identity = file_identity(os.fstat(source.fileno()))
        offset = zero_tail_offset(source)
        if offset is None:
            return False
        # Never trim or replace the member. The server already tolerates this
        # physical tail; use a private copy to check its full Raft semantics.
        with tempfile.NamedTemporaryFile(prefix="ovn-db-prefix-", suffix=".db") as prefix:
            source.seek(0)
            remaining = offset
            while remaining:
                chunk = source.read(min(remaining, CHUNK_SIZE))
                if not chunk:
                    return False
                prefix.write(chunk)
                remaining -= len(chunk)
            prefix.flush()
            result = subprocess.run(["ovsdb-tool", "check-cluster", prefix.name],
                                    capture_output=True, text=True, check=False)
            if result.returncode:
                sys.stderr.write(result.stdout + result.stderr)
                return False
            # Compaction, replacement or append during validation must not
            # turn a stale prefix into a successful check of the current file.
            if (file_identity(os.fstat(source.fileno())) != identity
                    or file_identity(os.stat(path)) != identity):
                return False
        print(f"{path}: validated Raft prefix through byte {offset}; "
              "leaving zero-filled tail for ovsdb-server recovery", file=sys.stderr)
        return True


def main():
    if len(sys.argv) != 2:
        print("usage: ovn-db-check.py DATABASE", file=sys.stderr)
        return 2
    path = sys.argv[1]
    try:
        result = subprocess.run(["ovsdb-tool", "check-cluster", path],
                                capture_output=True, text=True, check=False)
        if result.returncode == 0:
            sys.stdout.write(result.stdout)
            sys.stderr.write(result.stderr)
            return 0
        if validate_zero_tail(path):
            return 0
        sys.stdout.write(result.stdout)
        sys.stderr.write(result.stderr)
        return result.returncode
    except (OSError, ValueError) as error:
        print(f"{path}: Raft validation failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
