#!/usr/bin/env python3
"""Verify and unpack repository-owned sources. No network or implicit refresh."""
import argparse
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import tarfile

ROOT = Path(__file__).resolve().parents[1]


def unpack(component, dest, module=None, root=ROOT):
    lock = json.loads((root / 'third_party/sources.lock.json').read_text())
    if lock['version'] != 1:
        raise ValueError('unsupported source lock')
    entry = lock['archives'][component]
    if component == 'agent-base' and lock['agent_commit'] != (root / 'patches/AIRSIM_BASE').read_text().strip():
        raise ValueError('Agent baseline changed: preserve and review new source first')
    if module is not None:
        for name in ('go.mod', 'go.sum'):
            if hashlib.sha256((module / name).read_bytes()).hexdigest() != entry[name.replace('.', '_') + '_sha256']:
                raise ValueError(f'{component}: {name} changed; regenerate preserved dependencies')
    data = (root / 'third_party' / entry['file']).read_bytes()
    if hashlib.sha256(data).hexdigest() != entry['sha256']:
        raise ValueError(f'{component}: archive digest mismatch')
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as archive:
        members = archive.getmembers()
        seen = set()
        for member in members:
            p = PurePosixPath(member.name)
            if p.is_absolute() or '..' in p.parts or not p.parts or member.name in seen:
                raise ValueError('unsafe archive path')
            seen.add(member.name)
            if not (member.isfile() or member.isdir()):
                raise ValueError('archive links/devices are not supported')
            target = dest / p
            if target.exists() or target.is_symlink():
                if not (member.isdir() and target.is_dir() and not target.is_symlink()):
                    raise ValueError(f'refusing to overwrite {target}')
            for parent in [target, *target.parents]:
                if parent.is_symlink():
                    raise ValueError('symlink in destination')
        for member in members:
            target = dest / member.name
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                target.parent.mkdir(parents=True, exist_ok=True)
                with target.open('xb') as output:
                    output.write(archive.extractfile(member).read())
                target.chmod(0o755 if member.mode & 0o111 else 0o644)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('component', choices=['agent-base', 'agent-vendor', 'plugin-vendor'])
    parser.add_argument('destination', type=Path)
    parser.add_argument('--module', type=Path)
    args = parser.parse_args()
    unpack(args.component, args.destination, args.module)
