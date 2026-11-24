#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
[ "$(git -C "$HERE" rev-parse --show-toplevel)" = "$HERE" ] || { echo 'Requires a standalone committed repository' >&2; exit 1; }
git -C "$HERE" diff --quiet HEAD -- . || { echo 'Commit source before archiving' >&2; exit 1; }
[ -z "$(git -C "$HERE" ls-files --others --exclude-standard)" ] || { echo 'Review untracked source before archiving' >&2; exit 1; }
# A release source bundle must not depend solely on upstream URLs or out/ caches.
sh "$HERE/scripts/package-recovery.sh"
VERSION=$(cat "$HERE/VERSION")
sh "$HERE/scripts/fetch-sources.sh"
AGENT_TREE=$(sh "$HERE/scripts/prepare-agent.sh")
STAGE=$(mktemp -d "$HERE/out/source-release.XXXXXX")
mkdir -p "$STAGE/plugin" "$STAGE/AirSIM" "$STAGE/dependencies" "$HERE/dist"
git -C "$HERE" archive --format=tar --output="$STAGE/plugin.tar" HEAD
tar -xf "$STAGE/plugin.tar" -C "$STAGE/plugin"
cp -R "$AGENT_TREE/module" "$AGENT_TREE/LICENSE" "$AGENT_TREE/NOTICE" "$AGENT_TREE/THIRD_PARTY_NOTICES.md" "$STAGE/AirSIM/"
python3 "$HERE/scripts/preserved-sources.py" plugin-vendor "$STAGE/plugin" --module "$STAGE/plugin"
# prepare-agent already includes the checked, preserved Agent vendor sources.
cp "$HERE/out/release-sources/"*.tar.* "$STAGE/dependencies/"
cp "$HERE/SOURCES.md" "$STAGE/"
COPYFILE_DISABLE=1 tar -czf "$HERE/dist/airsim-mu300-$VERSION-sources.tar.gz" -C "$STAGE" plugin AirSIM dependencies SOURCES.md
printf 'Source bundle: %s\n' "$HERE/dist/airsim-mu300-$VERSION-sources.tar.gz"
