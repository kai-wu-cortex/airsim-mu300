#!/usr/bin/env python3
"""Fetch byte-pinned public test sources only; never invoke an updater."""
import hashlib
from pathlib import Path
import urllib.request

dest = Path(__file__).resolve().parents[1] / 'out/kernel73'
dest.mkdir(parents=True, exist_ok=True)
for name, commit, digest in [
    ('mu300-update.legacy', 'c760e49e28e872cc0a51a656f36206cecaaf091f', 'd2b4594130654047d9b91cedb22e8b18b888515192073ff8a94d0cc688b74a23'),
    ('mu300-update.original', '8aadd074673a23a7e1e37fc07cfd1dd3a7773917', 'cda543e8c077e92fffbea947a4662f571a5718ae9b82fba6491801674a6df12e'),
]:
    p = dest / name
    if p.exists() and hashlib.sha256(p.read_bytes()).hexdigest() == digest:
        print(name + ': pinned cache verified')
        continue
    url = f'https://raw.githubusercontent.com/dikeckaan/mu300-linux/{commit}/rootfs/overlay/opt/mu300/bin/mu300-update'
    with urllib.request.urlopen(url, timeout=30) as response:
        content = response.read(1024 * 1024 + 1)
    if hashlib.sha256(content).hexdigest() != digest:
        raise SystemExit(name + ': source digest mismatch')
    p.write_bytes(content)
    print(name + ': downloaded and verified')
