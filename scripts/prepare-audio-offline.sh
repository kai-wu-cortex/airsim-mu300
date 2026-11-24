#!/bin/sh
# Restore the already-patched R16 audio source without contacting any upstream.
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
[ "$#" = 1 ] || { echo 'Usage: prepare-audio-offline.sh /path/to/dependency-vault' >&2; exit 2; }
python3 "$HERE/scripts/dependency-vault.py" restore "$1"
DEST=$HERE/out/openwrt-audio
for name in linux-reference audio-reference release-kernel; do
 [ ! -e "$DEST/$name" ] && [ ! -L "$DEST/$name" ] || { echo "Refusing to replace existing $name" >&2; exit 1; }
done
mkdir -p "$DEST/linux-reference" "$DEST/audio-reference" "$DEST/release-kernel" "$DEST/evidence"
# The vault has verified every archive against the repository lock first.
tar -xzf "$DEST/downloads/linux.tar.gz" --strip-components=1 -C "$DEST/linux-reference"
tar -xzf "$DEST/downloads/modules.tar.gz" -C "$DEST/release-kernel" ./modules ./modules-u30air
tar -xzf "$HERE/out/release-sources/recovered-audio-r16-source.tar.gz" \
 --strip-components=2 -C "$DEST/audio-reference" ./mu300-audio
printf '%s\n' 'Offline audio source ready (R16 patches already applied). No kernel build or device operation performed.'
