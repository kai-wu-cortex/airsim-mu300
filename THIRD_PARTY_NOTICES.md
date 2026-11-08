# Third-party components

- AirSIM Agent: pinned public commit `cc2ade3124ac34bec7d8c441f4b66a57a6dfec62`
  from https://github.com/kai-wu-cortex/AirSIM, with the MU300-owned patch.
  PolyForm Noncommercial 1.0.0; its LICENSE, NOTICE and THIRD_PARTY_NOTICES.md
  are included in the package and source bundle. No rights are changed here.
- mu300-audio v1.0.5: kernel modules GPL-2.0, scripts/tools MIT; original archive
  and licence files retained inside the dependency archive. Source:
  https://github.com/dikeckaan/mu300-audio/tree/v1.0.5
  Original author: dikeckaan and contributors. The original URL currently
  returns 404; preserved source commit `ea03fba2e79684a729b16e4dc481316cf4ec2e39`:
  https://github.com/kai-wu-cortex/mu300-audio/tree/ea03fba2e79684a729b16e4dc481316cf4ec2e39
  The mirror is not an assertion of official endorsement or a GitHub fork relationship.
- mu300-linux: https://github.com/dikeckaan/mu300-linux, by dikeckaan and
  contributors. The plugin relies on its installed platform/AT/SMS services;
  it does not redistribute a complete OS image. Source/build context retains
  its upstream notices. Audio upstream also credits kanoqwq/e5-linux, Enceka
  and Unisoc; those notices remain in the source archive.
- OpenWrt alsa-lib 1.2.11-r1: LGPL-2.1-or-later; alsa-utils 1.2.11-r1:
  GPL-2.0-or-later. Official package source feed:
  https://downloads.openwrt.org/releases/25.12.6/packages/aarch64_generic/packages/
  Source: https://www.alsa-project.org/wiki/Download
- Go libraries: exact dependency lists and per-library LICENSE/NOTICE files are
  packaged under `/usr/share/doc/airsim/dependency-licenses/`; corresponding
  vendored source is in the release source bundle. They retain their licenses.

R23 retains the fixed offline DSP firmware backup from the public
`dikeckaan/mu300-audio-firmware` commit
`8a3fbd2cd0556796d0ffef5be05b4c21a743f868`, not from a user's device.
SHA-256: `378ceea78dbf013e10625e71efd6fab487d727e371884ae8daf078da2828b937`.
Upstream provides no license declaration. All original rights remain reserved;
the project's PolyForm/GPL/MIT licenses do not cover this separate binary.
See `docs/DSP_FIRMWARE_PROVENANCE.md` (also installed beside this notice) for
the source, fixed identity and unresolved license status. Both online and offline
installation paths verify the same pinned hash; existing firmware is preserved.
The local signing key is a development key, not an established public feed key.
Release distribution must include required licence texts, corresponding GPL
source/build information, and the upstream AirSIM distribution notices.
See SOURCES.md for exact snapshots, hashes, rebuild steps and the unavailable
original audio build-kit limitation. Firmware is never included in the source bundle.
