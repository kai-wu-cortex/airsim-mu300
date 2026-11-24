#!/bin/sh
# Exact corresponding source snapshots used by the release; never fetch firmware.
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DEST=$HERE/out/release-sources
mkdir -p "$DEST"
fetch() {
 name=$1 digest=$2 url=$3
 if [ ! -f "$DEST/$name" ]; then
  [ "${AIRSIM_OFFLINE:-0}" != 1 ] || { echo "Offline source missing: $name; restore dependency vault" >&2; exit 1; }
  curl -fL --proto '=https' --connect-timeout 15 --max-time 240 --retry 3 "$url" -o "$DEST/$name.part"
  mv "$DEST/$name.part" "$DEST/$name"
 fi
 [ "$(shasum -a 256 "$DEST/$name" | cut -d' ' -f1)" = "$digest" ] || { echo "Source hash mismatch: $name" >&2; exit 1; }
}
fetch alsa-lib-1.2.11.tar.bz2 9f3f2f69b995f9ad37359072fbc69a3a88bfba081fc83e9be30e14662795bb4d https://www.alsa-project.org/files/pub/lib/alsa-lib-1.2.11.tar.bz2
fetch alsa-utils-1.2.11.tar.bz2 9ac6ca3a883f151e568dcf979b8d2e5cbecc51b819bb0e6bb8a2e9b34cc428a7 https://www.alsa-project.org/files/pub/utils/alsa-utils-1.2.11.tar.bz2
fetch mu300-audio-v1.0.5-source.tar.gz e281c14721e2f6c6281af86f7fd47bb84591bb1144b8a3352a31696f1050da0d https://codeload.github.com/kai-wu-cortex/mu300-audio/tar.gz/ea03fba2e79684a729b16e4dc481316cf4ec2e39
fetch openwrt-packages-source.tar.gz c4baab767c5b7f345327e664975eb7e215b94b4eb572960468590eb688aa1476 https://codeload.github.com/openwrt/packages/tar.gz/4f3530534ce198fdc90e7667502706e9ee179692
fetch mu300-linux-kernel-build-source.tar.gz 3bcc1c21005249ea7e7137a16ef068c7f8b15e63834ddef4778e24561018ebf4 https://codeload.github.com/dikeckaan/mu300-linux/tar.gz/c760e49e28e872cc0a51a656f36206cecaaf091f
fetch recovered-audio-r16-source.tar.gz 81bb1902e255f2e1f710fff9f0f84e2011f667ba90a50f616368a35d57646b64 https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v0.1.0-r16/airsim-audio-0.1.0-r16-sources.tar.gz
