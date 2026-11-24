#!/bin/sh
# Preserve a pinned public upstream copy for offline candidate builds.
# stdout is only the verified cache path. Never read a device's firmware.
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
CACHE=${AIRSIM_FIRMWARE_CACHE:-$HERE/out/firmware-backup}
REPO=${AIRSIM_FIRMWARE_REPO:-$CACHE/mu300-audio-firmware.git}
UPSTREAM=https://github.com/dikeckaan/mu300-audio-firmware.git
COMMIT=8a3fbd2cd0556796d0ffef5be05b4c21a743f868
SHA=378ceea78dbf013e10625e71efd6fab487d727e371884ae8daf078da2828b937
mkdir -p "$CACHE"
valid() { [ -f "$1" ] && [ ! -L "$1" ] && [ "$(shasum -a 256 "$1" | cut -d' ' -f1)" = "$SHA" ]; }
if valid "$CACHE/agdsp.bin"; then
 printf '%s\n' "$CACHE/agdsp.bin"
 exit 0
fi
work=$(mktemp -d "$CACHE/.fetch.XXXXXX")
trap 'rm -rf "$work"' EXIT
if [ ! -e "$REPO" ] && [ "${AIRSIM_OFFLINE:-0}" != 1 ]; then
 # Clone only when missing; never prune a saved mirror or depend on upstream HEAD.
 if git -c http.lowSpeedLimit=1024 -c http.lowSpeedTime=30 clone --mirror "$UPSTREAM" "$work/upstream.git" >&2; then
  mkdir -p "$(dirname "$REPO")"
  mv "$work/upstream.git" "$REPO"
 fi
fi
if git -C "$REPO" show "$COMMIT:agdsp.bin" > "$work/agdsp.bin" 2>/dev/null && valid "$work/agdsp.bin"; then
 :
else
 [ "${AIRSIM_OFFLINE:-0}" != 1 ] || { echo 'Offline firmware missing; restore dependency vault' >&2; exit 1; }
 curl -fL --proto '=https' --proto-redir '=https' --connect-timeout 15 --max-time 60 \
  https://github.com/dikeckaan/mu300-audio-firmware/releases/download/v1.0/agdsp.bin -o "$work/agdsp.bin" >&2
 valid "$work/agdsp.bin" || { echo 'Upstream firmware digest mismatch; cache not replaced' >&2; exit 1; }
fi
chmod 644 "$work/agdsp.bin"
mv "$work/agdsp.bin" "$CACHE/agdsp.bin"
printf '%s  agdsp.bin\n' "$SHA" > "$CACHE/SHA256SUMS"
printf 'upstream=%s\ncommit=%s\nsha256=%s\nlicense=not-specified-by-upstream\n' "$UPSTREAM" "$COMMIT" "$SHA" > "$CACHE/PROVENANCE"
printf '%s\n' "$CACHE/agdsp.bin"
