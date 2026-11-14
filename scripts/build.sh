#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
python3 "$HERE/scripts/check-develop-consistency.py"
AGENT_TREE=$(sh "$HERE/scripts/prepare-agent.sh")
AGENT_SOURCE=$AGENT_TREE/module/module-agent
VERSION=$(cat "$HERE/VERSION")
mkdir -p "$HERE/out/linux-arm64"
# Nested worktrees can stamp the enclosing repository instead of this release;
# the isolated Agent also has a temporary baseline commit. Use explicit release
# versions and the accompanying committed source archive, not those VCS stamps.
(cd "$HERE"; CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath -ldflags="-s -w -X main.version=$VERSION" -o out/linux-arm64/airsim-mu300d ./cmd/airsim-mu300d)
(cd "$AGENT_SOURCE"; CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath -ldflags="-s -w -X main.agentVersion=0.4.7-mu300.$VERSION" -o "$HERE/out/linux-arm64/airsim-agent" .)
mkdir -p "$HERE/out/upstream-notices"
cp "$AGENT_TREE/LICENSE" "$AGENT_TREE/NOTICE" "$AGENT_TREE/THIRD_PARTY_NOTICES.md" "$HERE/out/upstream-notices/"
# Preserve licenses for the exact Go dependencies compiled into both executables.
(cd "$HERE"; go mod vendor -o "$HERE/out/_vendor-plugin")
(cd "$AGENT_SOURCE"; go mod vendor -o "$HERE/out/_vendor-agent")
for component in plugin agent; do
 vendor=$HERE/out/_vendor-$component
 licenses=$HERE/out/dependency-licenses/$component
 mkdir -p "$licenses"
 cp "$vendor/modules.txt" "$licenses/modules.txt"
 find "$vendor" -type f \( -iname '*license*' -o -iname '*copying*' -o -iname '*notice*' -o -name PATENTS \) | while IFS= read -r file; do
  relative=${file#"$vendor"/}
  mkdir -p "$licenses/$(dirname "$relative")"
  cp "$file" "$licenses/$relative"
 done
done
cp "$(go env GOROOT)/LICENSE" "$HERE/out/dependency-licenses/Go-LICENSE"
python3 "$HERE/scripts/check-develop-consistency.py" --binaries "$HERE/out/linux-arm64"
printf 'Built %s\n' "$HERE/out/linux-arm64"
