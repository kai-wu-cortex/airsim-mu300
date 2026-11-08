# Release sources and reproducibility

Applies to the `v0.1.0-r23` development preview. It adds optional native Tailscale
IPv4 manual pairing and per-call cloud receive buffer negotiation. These features
and the final R23 APK have automated coverage but no independent hardware acceptance.
Existing cloud early-media v1 and VoWLAN authentication remain in place.
PR #29 interfaces, SMS, long-call stability and offline firmware recovery retain
their documented untested boundaries. Audio remains 8 kHz PCM. Release assets:
<https://github.com/kai-wu-cortex/airsim-mu300/releases/tag/v0.1.0-r23>.
Download `airsim-mu300-0.1.0-r23-sources.tar.gz` alongside the binary package.
It contains plugin sources, the patched Agent and its notices, vendored Go sources,
audio driver/tool sources, ALSA sources, OpenWrt recipes and MU300 kernel build recipes.
No private signing keys, device state or user messages/audio are included. The source
archive does not contain the DSP binary; the APK's independent
firmware backup and its upstream license status are documented in
`docs/DSP_FIRMWARE_PROVENANCE.md`.

## Source map

| Component | Exact source / build information |
| --- | --- |
| Plugin / bridge | This repository's release tag; `go.mod`, `go.sum`, `scripts/`, `packaging/` |
| AirSIM Agent | `kai-wu-cortex/AirSIM` commit `cc2ade3124ac34bec7d8c441f4b66a57a6dfec62`, plus `patches/AirSIM-mu300.patch` |
| MU300 Audio v1.0.5 | Preserved commit `ea03fba2e79684a729b16e4dc481316cf4ec2e39` in `kai-wu-cortex/mu300-audio`; includes full audio sources, patches, tools, Dockerfiles, `kernel/kits/kits.lock` |
| MU300 kernel build context | `dikeckaan/mu300-linux` `v2026.10.19.1`, commit `c760e49e28e872cc0a51a656f36206cecaaf091f`; kernel build/patch/config recipes used by the matching 5.4 kit |
| alsa-lib 1.2.11 | <https://www.alsa-project.org/files/pub/lib/alsa-lib-1.2.11.tar.bz2>, LGPL-2.1-or-later; COPYING retained |
| alsa-utils 1.2.11 | <https://www.alsa-project.org/files/pub/utils/alsa-utils-1.2.11.tar.bz2>, GPL-2.0-or-later; COPYING retained |
| OpenWrt package recipes | `openwrt/packages` commit `4f3530534ce198fdc90e7667502706e9ee179692`, `libs/alsa-lib/`, `sound/alsa-utils/` (version 1.2.11-r1) |
| Go libraries | Exact versions from each `go.mod` / `go.sum`; vendored source and upstream license/notice files included |

Archive hashes and URLs are pinned in `scripts/fetch-sources.sh`; binary dependency
hashes in `scripts/fetch-deps.sh`. The binary audio dependency is mirrored byte-for-byte
with its original checksum manifest because the original repository currently returns 404.

## Rebuilding the plugin

Follow README's build commands. There is no dependency on an unpublished sibling
`mu300-audio` checkout. Build from this repository plus the publicly pinned AirSIM
Git commit; the helper creates an isolated patched source tree. Custom builds must
use their own signing key and distribute its fingerprint separately. The private
release key is intentionally absent, so signed APK bytes are not reproducible.

The source bundle's `plugin/` and `AirSIM/` directories can be siblings. The bundled
AirSIM directory is a patched snapshot, not a Git repository: for the ordinary build
script clone the pinned upstream repo as documented, or compile the snapshot directly
with `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build` inside `module/module-agent`.
Use the vendored modules to avoid resolving different dependency versions.

## Rebuilding audio and ALSA dependencies

The R23 APK contains the bridge, Agent, isolated ALSA tools and the hash-pinned
`airsim-audio-0.1.0-r16.tar.gz` recovery payload: 24 rebuilt modules and static tools.
The installation workflow selects only its exact kernel/configuration profile,
backs up replaced files and continues preparation after an arranged boot when
needed. For missing DSP firmware, try the pinned upstream URL, then fall back to
the APK's verified offline copy from a locally preserved upstream repository.
Both paths require the same fixed SHA-256; existing device firmware is preserved.
This binary has no upstream license declaration and is not relicensed under the
plugin's license; see `docs/DSP_FIRMWARE_PROVENANCE.md`. The legacy kernel modules
are not used.

- Corresponding patched audio source and integration recipes are pinned by
  `scripts/fetch-sources.sh` and included as `dependencies/recovered-audio-r16-source.tar.gz`
  (the public `airsim-audio-0.1.0-r16-sources.tar.gz`); the plugin bundle includes legacy tool
  source for provenance. Pinned current sources, hashes and compiler/build steps
  are in `packaging/openwrt-audio/sources.json`, its README and the CI workflow.
- Kernel source: Enceka/ZTE commit `b50db5b6224c11db42a246e8fcdaad1e1d59f478`;
  current system recipe v2026.10.30.1; audio commit
  `53cbe6a7344b7fa175ff87a73630f05e5862f483` plus the two supplied patches.
  The kernel source's signing private key is excluded from extraction. No built
  kernel Image is distributed or installed. Build the matching full symbol table
  and audio modules using the isolated Docker workflow, then run the ABI audit.
- The completed kernel/module build is recorded by CI run 41300853446. All 24
  modules passed 978 import checks and comparison with 202 official modules.
  Short bidirectional calls were confirmed on one U30 Air; other targets remain
  unverified. The old v1.0.5 kernel modules are not used.
- ALSA: use the included tarballs plus the corresponding OpenWrt recipe directories
  in the OpenWrt 25.12.6 SDK for `aarch64_generic`, retaining recipe patches/options.
  They remain external executables/shared libraries below `/usr/lib/airsim/alsa/`,
  not statically linked into the noncommercial Go binaries. Modified compatible
  replacements may be built/installed without a plugin source-license restriction
  being applied to those separately licensed components.

The release does not supply the whole OpenWrt operating system or its toolchain.
System source and SDK provenance: <https://downloads.openwrt.org/releases/25.12.6/>.
Do not describe the entire firmware/toolchain as hermetic or bit-for-bit reproduced.

## Publishing discipline

Run tests, prepare the package, commit reviewed source, then run
`scripts/package-sources.sh` and publish its output together with the APK, public key,
installer and SHA256SUMS. Source archives remain available with the release, not
merely an offer to provide them later. Preserve every upstream notice and licence.
Never commit `backups/`, `out/`, an entire workspace, credentials or firmware to
the source tree. The maintainer requested the fixed DSP backup during R18 development, retained through R23. Its upstream
redistribution terms remain undocumented; this release decision is not a grant
of rights or a declaration that the independent binary is open-source.

The APK builder retains explicit parent directory records (`--compat 3.0.0_pre3`)
so the v3 package remains compatible with device apk-tools 3.0.5 and the stricter
archive extraction checks in apk-tools 3.0.8. Signature verification is unchanged.
