#!/bin/sh
# A separate companion asset: includes the previously approved public DSP backup.
# Never include device data, a whole workspace, or signing keys.
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=$(cat "$HERE/VERSION")
LOCK_SHA=$(shasum -a 256 "$HERE/third_party/recovery.lock.json" | cut -d' ' -f1)
VAULT=${AIRSIM_DEPENDENCY_VAULT:-$HERE/out/recovery-$LOCK_SHA}
if [ -e "$VAULT" ]; then
 python3 "$HERE/scripts/dependency-vault.py" verify "$VAULT"
else
 python3 "$HERE/scripts/dependency-vault.py" capture "$VAULT"
fi
mkdir -p "$HERE/dist"
ARCHIVE=$HERE/dist/airsim-mu300-$VERSION-recovery.tar.gz
python3 - "$VAULT" "$ARCHIVE.part" <<'PY'
import json
from pathlib import Path
import sys
import tarfile
vault = Path(sys.argv[1])
lock = json.loads((vault / 'recovery.lock.json').read_text())
names = [item['path'] for item in lock['files']] + ['recovery.lock.json', 'DSP_FIRMWARE_PROVENANCE.md']
# Do not archive arbitrary files someone later added to the vault.
with tarfile.open(sys.argv[2], 'w:gz') as archive:
    for name in names:
        source = vault / name
        if source.is_symlink() or not source.is_file():
            raise ValueError('invalid recovery member: ' + name)
        archive.add(source, arcname=name, recursive=False)
PY
mv "$ARCHIVE.part" "$ARCHIVE"
(cd "$HERE/dist"; shasum -a 256 "${ARCHIVE##*/}" > "${ARCHIVE##*/}.sha256")
printf 'Recovery companion: %s\n' "$ARCHIVE"
