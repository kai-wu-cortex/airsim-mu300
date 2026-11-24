#!/usr/bin/env python3
"""Preserve/verify/restore exact build inputs, never fetch or execute them."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def checked_path(base, relative):
    p = PurePosixPath(relative)
    if p.is_absolute() or '..' in p.parts or len(p.parts) < 2 or p.parts[0] != 'out':
        raise ValueError('invalid recovery path')
    target = base / p
    if any(x.is_symlink() for x in [target, *target.parents]):
        raise ValueError('symlink in recovery path')
    return target


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def run(action, vault, root=ROOT):
    lock_path = root / 'third_party/recovery.lock.json'
    lock_bytes = lock_path.read_bytes()
    lock = json.loads(lock_bytes)
    if lock['version'] != 1:
        raise ValueError('unsupported recovery lock')
    if action != 'capture' and (vault / 'recovery.lock.json').read_bytes() != lock_bytes:
        raise ValueError('vault lock differs from reviewed repository lock')
    source = root if action == 'capture' else vault
    # Validate the whole inventory before restoring even one file.
    paths = []
    for item in lock['files']:
        f = checked_path(source, item['path'])
        if not f.is_file() or digest(f) != item['sha256']:
            raise ValueError('missing or corrupt recovery input: ' + item['path'])
        paths.append((item, f))
    if action == 'verify':
        return len(paths)
    if action == 'capture':
        if vault.exists() or vault.is_symlink():
            raise ValueError('use a new vault directory; existing snapshots are immutable')
        vault.parent.mkdir(parents=True, exist_ok=True)
        dest = Path(tempfile.mkdtemp(prefix='.airsim-vault-', dir=vault.parent))
    else:
        dest = root
    # Reject destination links before mutating any input.
    for item, _ in paths:
        checked_path(dest, item['path'])
    try:
        for item, f in paths:
            target = checked_path(dest, item['path'])
            if target.is_file() and digest(target) == item['sha256']:
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            fd, temporary = tempfile.mkstemp(prefix='.restore-', dir=target.parent)
            try:
                with os.fdopen(fd, 'wb') as out, f.open('rb') as incoming:
                    shutil.copyfileobj(incoming, out)
                if digest(Path(temporary)) != item['sha256']:
                    raise ValueError('input changed during copy')
                os.replace(temporary, target)
            finally:
                if os.path.exists(temporary):
                    os.unlink(temporary)
        if action == 'capture':
            (dest / 'recovery.lock.json').write_bytes(lock_bytes)
            # Firmware is a separate public upstream backup, with unchanged terms.
            shutil.copyfile(root / 'docs/DSP_FIRMWARE_PROVENANCE.md', dest / 'DSP_FIRMWARE_PROVENANCE.md')
            os.rename(dest, vault)
    finally:
        if action == 'capture' and dest.exists():
            shutil.rmtree(dest)
    return len(paths)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['capture', 'verify', 'restore'])
    parser.add_argument('vault', type=Path)
    args = parser.parse_args()
    count = run(args.action, args.vault.absolute())
    print(f'{args.action}: {count} pinned inputs verified')
