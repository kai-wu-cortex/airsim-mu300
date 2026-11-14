#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=$(cat "$HERE/VERSION")
PACKAGE_OUT=${AIRSIM_PACKAGE_OUT:-dist}
case "$PACKAGE_OUT" in /*|*..*|*[!a-zA-Z0-9_./-]*|'') echo 'Invalid package output directory' >&2; exit 1 ;; esac
PACKAGE_DIR=$HERE/$PACKAGE_OUT
LICENSES='PolyForm-Noncommercial-1.0.0 AND GPL-2.0-only AND GPL-2.0-or-later AND LGPL-2.1-or-later AND MIT AND BSD-3-Clause'
sh "$HERE/scripts/build.sh"
sh "$HERE/scripts/fetch-deps.sh"
sh "$HERE/scripts/fetch-sources.sh"
STAGE=$(mktemp -d "$HERE/out/package.XXXXXX")
cp -R "$HERE/packaging/openwrt/root/." "$STAGE/"
node "$HERE/scripts/stamp-ui.mjs" "$STAGE"
cp "$HERE/out/linux-arm64/airsim-agent" "$HERE/out/linux-arm64/airsim-mu300d" "$STAGE/usr/lib/airsim/"
cp "$HERE/VERSION" "$STAGE/usr/lib/airsim/VERSION"
POST_INSTALL=/work/packaging/openwrt/post-install
PRE_UPGRADE=/work/packaging/openwrt/pre-upgrade
PRE_INSTALL=
if [ "${AIRSIM_COMPLETE_INSTALL:-1}" = 1 ]; then
 LICENSES="$LICENSES AND LicenseRef-Undocumented-DSP-Firmware"
 # Candidate only: the signed APK carries the exact already-audited bundle.
 # Never combine arbitrary modules with the complete-install profile.
 AUDIO_BUNDLE=${AIRSIM_AUDIO_BUNDLE:-$HERE/out/audio-deps/airsim-audio-0.1.0-r16.tar.gz}
 [ "$(shasum -a 256 "$AUDIO_BUNDLE" | cut -d' ' -f1)" = 5af9b2dd888905b453b61273668736c24e1dd67b821d5fdb5a59c807a0134af3 ] || { echo 'Audio bundle hash mismatch' >&2; exit 1; }
 mkdir -p "$STAGE/usr/lib/airsim/deps"
 cp "$AUDIO_BUNDLE" "$STAGE/usr/lib/airsim/deps/airsim-audio-0.1.0-r16.tar.gz"
 # Offline recovery copy comes only from the fixed public upstream archive/cache.
 FIRMWARE=$(sh "$HERE/scripts/fetch-firmware.sh")
 cp "$FIRMWARE" "$STAGE/usr/lib/airsim/deps/agdsp.bin"
 [ "$(shasum -a 256 "$STAGE/usr/lib/airsim/deps/agdsp.bin" | cut -d' ' -f1)" = 378ceea78dbf013e10625e71efd6fab487d727e371884ae8daf078da2828b937 ] || { echo 'Bundled firmware digest mismatch' >&2; exit 1; }
 mkdir -p "$STAGE/usr/share/doc/airsim"
 cp "$HERE/docs/DSP_FIRMWARE_PROVENANCE.md" "$STAGE/usr/share/doc/airsim/"
 cp "$HERE/packaging/openwrt/audio-preflight" "$HERE/packaging/openwrt/audio-setup" "$STAGE/usr/lib/airsim/"
 chmod 755 "$STAGE/usr/lib/airsim/audio-preflight" "$STAGE/usr/lib/airsim/audio-setup" "$STAGE/etc/init.d/airsim-audio-setup"
 cp "$HERE/packaging/openwrt/audio-preflight" "$STAGE.pre-upgrade"
 cat "$HERE/packaging/openwrt/pre-upgrade" >> "$STAGE.pre-upgrade"
 POST_INSTALL=/work/packaging/openwrt/post-install-complete
 PRE_UPGRADE=/work/out/${STAGE##*/}.pre-upgrade
 PRE_INSTALL=/work/packaging/openwrt/audio-preflight
else
 rm -f "$STAGE/etc/init.d/airsim-audio-setup"
fi
mkdir -p "$STAGE/usr/lib/airsim/alsa" "$STAGE/usr/lib/airsim/bin" "$PACKAGE_DIR"
mkdir -p "$STAGE/usr/share/doc/airsim"
cp "$HERE/THIRD_PARTY_NOTICES.md" "$STAGE/usr/share/doc/airsim/"
cp "$HERE/out/upstream-notices/LICENSE" "$STAGE/usr/share/doc/airsim/AirSIM-LICENSE"
cp "$HERE/out/upstream-notices/NOTICE" "$STAGE/usr/share/doc/airsim/AirSIM-NOTICE"
cp "$HERE/out/upstream-notices/THIRD_PARTY_NOTICES.md" "$STAGE/usr/share/doc/airsim/AirSIM-THIRD_PARTY_NOTICES.md"
cp "$HERE/LICENSE" "$HERE/NOTICE" "$HERE/SOURCES.md" "$STAGE/usr/share/doc/airsim/"
cp "$HERE/docs/PCM_GAIN.md" "$HERE/docs/RELEASE_v$VERSION.md" "$STAGE/usr/share/doc/airsim/"
cp "$HERE/docs/KERNEL_SELECTION.md" "$HERE/patches/mu300-update-7.3.patch" "$STAGE/usr/share/doc/airsim/"
cp -R "$HERE/licenses" "$STAGE/usr/share/doc/airsim/"
cp -R "$HERE/out/dependency-licenses" "$STAGE/usr/share/doc/airsim/"
# License texts come from the pinned source archives, not an unpublished sibling checkout.
tar -xOf "$HERE/out/audio-deps/mu300-audio-5.4.tar.gz" ./licence/COPYING > "$STAGE/usr/share/doc/airsim/GPL-2.0"
tar -xOf "$HERE/out/audio-deps/mu300-audio-5.4.tar.gz" ./licence/LICENSE > "$STAGE/usr/share/doc/airsim/mu300-audio-LICENSE"
tar -xOf "$HERE/out/release-sources/alsa-lib-1.2.11.tar.bz2" alsa-lib-1.2.11/COPYING > "$STAGE/usr/share/doc/airsim/alsa-lib-COPYING"
tar -xOf "$HERE/out/release-sources/alsa-utils-1.2.11.tar.bz2" alsa-utils-1.2.11/COPYING > "$STAGE/usr/share/doc/airsim/alsa-utils-COPYING"
# Recovery modules are a separate, explicitly installed and ABI-checked asset.
# Official OpenWrt user-space packages are isolated below /usr/lib/airsim.
# Do not install their generic 6.12 kernel dependencies on mu300-linux 5.4.
docker run --rm -v "$HERE:/work" node:22-alpine sh -c '
 set -eu
 dest="$1/usr/lib/airsim/alsa"
 apk extract --allow-untrusted --destination "$dest" /work/out/alsa-packages/alsa-lib-1.2.11-r1.apk /work/out/alsa-packages/alsa-utils-1.2.11-r1.apk
 ' sh "/work/out/${STAGE##*/}"
for cmd in amixer aplay arecord; do
	cp "$HERE/packaging/openwrt/alsa-wrapper" "$STAGE/usr/lib/airsim/bin/$cmd"
done
find "$STAGE" -type d -exec chmod 755 {} +
chmod 755 "$STAGE/usr/lib/airsim/dashboard-setup"
chmod 755 "$STAGE/etc/init.d/airsim" "$STAGE/usr/libexec/rpcd/airsim" "$STAGE/usr/lib/airsim/prepare" "$STAGE/usr/lib/airsim/bin/"* "$STAGE/usr/lib/airsim/airsim-"*
chmod 755 "$STAGE/etc/init.d/airsim-update" "$STAGE/etc/init.d/airsim-device-settings"
KEY=${AIRSIM_PACKAGE_KEY:-$HERE/out/signing/airsim-dev.pem}
mkdir -p "$(dirname "$KEY")"
if [ ! -s "$KEY" ]; then
	(umask 077; openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$KEY" 2>/dev/null)
fi
openssl pkey -in "$KEY" -pubout -out "$PACKAGE_DIR/airsim-dev.pem" 2>/dev/null
# ADB v3 compatible with device apk-tools 3.0.5. Retain parent directory
# records as well: apk-tools 3.0.8 extract validates parents within the archive
# rather than trusting pre-existing directories in the destination.
set --
[ -z "$PRE_INSTALL" ] || set -- --script "pre-install:$PRE_INSTALL"
docker run --rm -v "$HERE:/work" -v "$KEY:/signing/key.pem:ro" node:22-alpine apk mkpkg "$@" \
 --compat 3.0.0_pre3 --compression deflate:9 --xattrs=no --sign-key /signing/key.pem \
 --info name:luci-app-airsim --info "version:$VERSION" --info arch:aarch64_generic \
 --info 'description:AirSIM for MU300' --info "license:$LICENSES" \
 --info 'depends:luci-base rpcd uclient-fetch ca-bundle' \
 --script "post-install:$POST_INSTALL" \
 --script "post-upgrade:$POST_INSTALL" \
 --script "pre-upgrade:$PRE_UPGRADE" \
 --script pre-deinstall:/work/packaging/openwrt/pre-deinstall \
 --files "/work/out/${STAGE##*/}" --output "/work/$PACKAGE_OUT/luci-app-airsim-$VERSION.apk"
(cd "$PACKAGE_DIR"; shasum -a 256 "luci-app-airsim-$VERSION.apk" airsim-dev.pem > SHA256SUMS)
printf 'Package: %s\n' "$PACKAGE_DIR/luci-app-airsim-$VERSION.apk"
