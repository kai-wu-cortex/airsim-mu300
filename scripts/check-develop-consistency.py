#!/usr/bin/env python3
"""Check package UI/ACL/catalog and binary version consistency; never run ARM code."""
import argparse
import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]

def check(root=ROOT, binaries=None):
    errors = []
    web = root / 'packaging/openwrt/root'
    menu = json.loads((web / 'usr/share/luci/menu.d/luci-app-airsim.json').read_text())
    for entry in menu.values():
        action = entry.get('action', {})
        if action.get('type') == 'view':
            if not (web / 'www/luci-static/resources/view' / (action['path'] + '.js')).is_file():
                errors.append('menu view missing: ' + action['path'])
    acl = json.loads((web / 'usr/share/rpcd/acl.d/luci-app-airsim.json').read_text())['luci-app-airsim']
    read = set(acl['read']['ubus']['airsim'])
    write = set(acl['write']['ubus']['airsim'])
    if 'device_read' not in read or 'device_write' not in write or 'device_write' in read:
        errors.append('maintenance read/write ACL mismatch')
    rpc = (web / 'usr/libexec/rpcd/airsim').read_text()
    for name in read | write:
        if '"' + name + '"' not in rpc:
            errors.append('ACL method absent from rpcd: ' + name)
    for page in (web / 'www/luci-static/resources/view/airsim').glob('*.js'):
        for dependency in re.findall(r"'require view\.airsim\.([a-z_]+)(?: as [a-z_]+)?';", page.read_text()):
            if not (page.parent / (dependency + '.js')).is_file():
                errors.append('missing UI dependency: ' + dependency)
    catalog = json.loads((root / 'cmd/airsim-mu300d/audio_catalog.json').read_text())
    profiles = catalog['profiles']
    if catalog['version'] != 1 or len({p['id'] for p in profiles}) != len(profiles):
        errors.append('invalid audio catalog')
    package = (root / 'scripts/package-apk.sh').read_text()
    kernel = (root / 'cmd/airsim-mu300d/kernel.go').read_text()
    setup = (root / 'packaging/openwrt/audio-setup').read_text()
    preflight = (root / 'packaging/openwrt/audio-preflight').read_text()
    for p in profiles:
        if not p['available']:
            continue
        if not re.fullmatch(r'[a-f0-9]{64}', p.get('sha256', '')) or p['sha256'] not in package or p['sha256'] not in setup:
            errors.append('audio bundle pin mismatch: ' + p['id'])
        if p.get('kernel') not in kernel or p['kernel'] not in preflight or p.get('config_sha256', '') not in kernel or p['config_sha256'] not in preflight:
            errors.append('audio kernel/config pin mismatch: ' + p['id'])
    for path in [root/'packaging/openwrt/audio-setup', root/'packaging/openwrt/audio-preflight']:
        if 'ANY_BUILD=1' in path.read_text() or 'SKIP_ABI=1' in path.read_text():
            errors.append('forbidden audio verification bypass')
    if binaries:
        version = (root / 'VERSION').read_text().strip()
        for name,stamp in [('airsim-mu300d',version),('airsim-agent','0.4.7-mu300.'+version)]:
            p = binaries/name
            if not p.is_file():
                errors.append('missing binary: '+name)
                continue
            metadata = subprocess.run(['go','version','-m',str(p)],capture_output=True,text=True,check=True).stdout
            # Go -trimpath omits ldflags from build info. The linker stores each
            # -X string as a NUL-terminated value; require the exact build stamp.
            if b'\0'+stamp.encode()+b'\0' not in p.read_bytes() or 'GOARCH=arm64' not in metadata or 'GOOS=linux' not in metadata or 'CGO_ENABLED=0' not in metadata:
                errors.append('binary build/version mismatch: '+name)
    return errors

if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--binaries', type=Path)
    args = p.parse_args()
    failures = check(binaries=args.binaries)
    print(json.dumps({'passed':not failures,'errors':failures},ensure_ascii=False,indent=2))
    raise SystemExit(bool(failures))
