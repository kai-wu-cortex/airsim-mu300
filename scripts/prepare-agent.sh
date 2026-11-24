#!/bin/sh
# Read only the pinned upstream commit. Never patch the user's AirSIM checkout.
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
BASE=$(cat "$HERE/patches/AIRSIM_BASE")
mkdir -p "$HERE/out"
TASK_SOURCE=$(mktemp -d "$HERE/out/agent-source.XXXXXX")
if [ -n "${AIRSIM_UPSTREAM_REPO:-}" ]; then
 # Explicit source override remains read-only and pinned; never fetch HEAD.
 git -C "$AIRSIM_UPSTREAM_REPO" cat-file -e "$BASE^{commit}"
 git -C "$AIRSIM_UPSTREAM_REPO" archive --format=tar --output="$TASK_SOURCE/upstream.tar" "$BASE" module/module-agent LICENSE NOTICE THIRD_PARTY_NOTICES.md
 tar -xf "$TASK_SOURCE/upstream.tar" -C "$TASK_SOURCE"
else
 python3 "$HERE/scripts/preserved-sources.py" agent-base "$TASK_SOURCE"
fi
git -C "$TASK_SOURCE" init -q
git -C "$TASK_SOURCE" add module LICENSE NOTICE THIRD_PARTY_NOTICES.md
git -C "$TASK_SOURCE" -c user.name='MU300 build' -c user.email='build@localhost' -c commit.gpgsign=false commit -qm 'Pinned upstream Agent baseline'
git -C "$TASK_SOURCE" apply --check "$HERE/patches/AirSIM-mu300.patch"
git -C "$TASK_SOURCE" apply "$HERE/patches/AirSIM-mu300.patch"
python3 "$HERE/scripts/preserved-sources.py" agent-vendor "$TASK_SOURCE/module/module-agent" --module "$TASK_SOURCE/module/module-agent"
printf '%s\n' "$TASK_SOURCE"
