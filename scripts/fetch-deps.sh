#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
mkdir -p "$HERE/out/alsa-packages"
fetch() {
	name=$1 expected=$2
	file="$HERE/out/alsa-packages/$name"
	if [ ! -f "$file" ]; then
		[ "${AIRSIM_OFFLINE:-0}" != 1 ] || { echo "Offline dependency missing: $name; restore dependency vault" >&2; exit 1; }
		curl -fL --proto '=https' --connect-timeout 15 --max-time 180 --retry 3 \
			"https://downloads.openwrt.org/releases/25.12.6/packages/aarch64_generic/packages/$name" -o "$file"
	fi
	actual=$(shasum -a 256 "$file" | cut -d' ' -f1)
	[ "$actual" = "$expected" ] || { echo "Dependency digest mismatch: $name" >&2; exit 1; }
}
fetch alsa-lib-1.2.11-r1.apk 8a64a215c6592301bd358243801fdc4526a616ab1fec88709fbdb681e6d159c8
fetch alsa-utils-1.2.11-r1.apk 3c84a77d6edc3821b0737571a6b33f7693ba1881f35ac6e9619d053c150a3878
mkdir -p "$HERE/out/audio-deps"
for name in mu300-audio-5.4.tar.gz SHA256SUMS; do
	file="$HERE/out/audio-deps/$name"
	if [ ! -f "$file" ]; then
		if [ -f "$HERE/backups/mu300-audio-2026-10-26/release-assets/v1.0.5/$name" ]; then
			cp "$HERE/backups/mu300-audio-2026-10-26/release-assets/v1.0.5/$name" "$file"
		else
			[ "${AIRSIM_OFFLINE:-0}" != 1 ] || { echo "Offline audio missing: $name; restore dependency vault" >&2; exit 1; }
			# Original upstream may be unavailable. Both copies must match the
			# independently pinned bytes below; the mirror is not a new version.
			mirror=$name
			[ "$name" != SHA256SUMS ] || mirror=mu300-audio-v1.0.5-SHA256SUMS
			curl -fL --proto '=https' --connect-timeout 15 --max-time 180 \
				"https://github.com/dikeckaan/mu300-audio/releases/download/v1.0.5/$name" -o "$file.part" || \
			curl -fL --proto '=https' --connect-timeout 15 --max-time 180 --retry 3 \
				"https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v0.1.0-r12/$mirror" -o "$file.part"
			mv "$file.part" "$file"
		fi
	fi
	case "$name" in
		mu300-audio-5.4.tar.gz) expected=8e2eb2774c485a2df4e28e4e77592a457ffbb626f1dace3b6d1cdc338b786e2b ;;
		SHA256SUMS) expected=42f991d76e0246a1385fbe46e75102dcd46db138dbfd816e853f4f245b02cf91 ;;
	esac
	[ "$(shasum -a 256 "$file" | cut -d' ' -f1)" = "$expected" ] || { echo "Audio dependency digest mismatch: $name" >&2; exit 1; }
done

# Audited current-kernel recovery, bundled by the complete-install candidate.
if [ "${AIRSIM_COMPLETE_INSTALL:-1}" = 1 ] && [ -z "${AIRSIM_AUDIO_BUNDLE:-}" ]; then
 file="$HERE/out/audio-deps/airsim-audio-0.1.0-r16.tar.gz"
 if [ ! -f "$file" ]; then
  [ "${AIRSIM_OFFLINE:-0}" != 1 ] || { echo 'Offline recovery bundle missing; restore dependency vault' >&2; exit 1; }
  curl -fL --proto '=https' --connect-timeout 15 --max-time 180 --retry 3 \
   https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v0.1.0-r16/airsim-audio-0.1.0-r16.tar.gz -o "$file.part"
  mv "$file.part" "$file"
 fi
 [ "$(shasum -a 256 "$file" | cut -d' ' -f1)" = 5af9b2dd888905b453b61273668736c24e1dd67b821d5fdb5a59c807a0134af3 ] || { echo 'Recovery bundle digest mismatch' >&2; exit 1; }
fi
