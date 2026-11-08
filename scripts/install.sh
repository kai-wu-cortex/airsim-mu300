#!/bin/sh
# Fixed-release installer. Inspect this file before running as root on the MU300.
set -eu
RELEASE=0.1.0-r23
BASE=https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v$RELEASE
KEY_SHA=0ce703e76ea4b702194c54b90534d7127874bb38ecda17f7ed6b1add2d2cf1de
PACKAGE=luci-app-airsim-$RELEASE.apk
fail() { echo "AirSIM: $*" >&2; exit 1; }
case "${1:---help}" in
 --check|--install) mode=$1 ;;
 --help) echo 'Usage: sh install.sh --check | --install (on the MU300, as root)'; exit 0 ;;
 *) fail 'Unknown argument; use --check or --install' ;;
esac
[ $# -le 1 ] || fail 'Too many arguments'
[ "$(id -u)" = 0 ] || fail 'Run on the target MU300 as root'
[ "$(uname -m)" = aarch64 ] || fail 'This package requires aarch64'
# Check required capabilities, not a fixed firmware/kernel release.
for tool in apk sha256sum wget jsonfilter mu300-at ubus pidof; do command -v "$tool" >/dev/null || fail "Missing $tool"; done
apk --version | grep -q 'apk-tools 3\.' || fail 'APK v3 is required'
# The complete package includes a hardware-specific audio bundle; validate its profile.
[ "$(uname -r)" = 5.4.254-gb50db5b6224c ] || fail 'No verified bundled audio profile for this kernel'
config=$(gzip -dc /proc/config.gz | grep -E '^(CONFIG_|# CONFIG_)' | LC_ALL=C sort | sha256sum | cut -d' ' -f1)
[ "$config" = 1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3 ] || fail 'Bundled audio does not match kernel configuration'
idle() {
 if pidof mu300-sms >/dev/null; then
  fail 'Wait for the native SMS operation to finish'
 else
  [ "$?" = 1 ] || fail 'Cannot determine native SMS operation state'
 fi
 calls=$(mu300-at -t 3 'AT+CLCC') || fail 'Cannot confirm modem call state'
 printf '%s\n' "$calls" | tr -d '\r' | grep -q '^OK$' || fail 'No valid CLCC reply'
 if printf '%s\n' "$calls" | grep -q '+CLCC:'; then fail 'Finish all calls before installing'; fi
 # Installed service: fail closed if its status is missing or ambiguous.
 # The update monitor shares the binary name but does not own phone/media state.
 services=$(ubus call service list '{"name":"airsim"}') || fail 'Cannot confirm AirSIM service state'
 printf '%s' "$services" | jsonfilter -e '@' >/dev/null || fail 'Invalid AirSIM service state'
 running=$(printf '%s' "$services" | jsonfilter -e '@.airsim.instances.*.running' || true)
 if printf '%s\n' "$running" | grep -q '^true$'; then
  status=$(wget -q -T 5 -O - http://127.0.0.1:7581/status) || fail 'Cannot confirm AirSIM media state'
  [ "$(printf '%s' "$status" | jsonfilter -e '@.audio.active')" = false ] || fail 'Media active or unknown'
  [ "$(printf '%s' "$status" | jsonfilter -e '@.call_active')" = false ] || fail 'Call active or unknown'
 fi
 if ubus list mu300dash 2>/dev/null | grep -q '^mu300dash$'; then
  job=$(ubus call mu300dash sms_job) || fail 'Cannot confirm SMS task state'
  running=$(printf '%s' "$job" | jsonfilter -e '@.running' || true)
  [ "$running" != 1 ] || fail 'Wait for SMS sending to finish'
  printf '%s' "$job" | jsonfilter -e '@.none' -e '@.done' | grep -q '^1$' || fail 'SMS task state unknown'
 fi
}
idle
echo "AirSIM $RELEASE: required tools available, no active call/media/SMS task."
[ "$mode" = --install ] || exit 0
work=$(mktemp -d /tmp/airsim-install.XXXXXX)
chmod 700 "$work"
echo "Downloading to $work (retained for inspection)."
for file in "$PACKAGE" airsim-dev.pem SHA256SUMS; do
 wget -T 60 -O "$work/$file" "$BASE/$file" || fail "Download failed: $file; no package installed"
done
[ "$(sha256sum "$work/airsim-dev.pem" | cut -d' ' -f1)" = "$KEY_SHA" ] || fail 'Signing public key fingerprint mismatch'
for file in "$PACKAGE" airsim-dev.pem; do
 expected=$(awk -v name="$file" '$2 == name {print $1}' "$work/SHA256SUMS")
 [ "${#expected}" = 64 ] || fail "Missing or duplicate checksum: $file"
 case "$expected" in *[!0-9a-f]*) fail 'Invalid checksum' ;; esac
 [ "$(sha256sum "$work/$file" | cut -d' ' -f1)" = "$expected" ] || fail "Checksum mismatch: $file"
done
# --install explicitly trusts this pinned project key, never an arbitrary downloaded key.
mkdir -p "$work/keys"
cp "$work/airsim-dev.pem" "$work/keys/airsim-dev.pem"
apk --keys-dir "$work/keys" verify "$work/$PACKAGE" || fail 'APK signature verification failed'
idle
if [ -e /etc/apk/keys/airsim-dev.pem ]; then
 [ "$(sha256sum /etc/apk/keys/airsim-dev.pem | cut -d' ' -f1)" = "$KEY_SHA" ] || fail 'Existing signing key differs; refusing to replace it'
else
 mkdir -p /etc/apk/keys
 cp "$work/airsim-dev.pem" /etc/apk/keys/airsim-dev.pem
 chmod 644 /etc/apk/keys/airsim-dev.pem
fi
apk --no-network add --upgrade "$work/$PACKAGE"
[ -x /usr/lib/airsim/audio-setup ] || fail 'Complete audio setup helper missing; calls are not ready'
audio_state=$(/usr/lib/airsim/audio-setup --status)
case "$audio_state" in
 ready) echo 'Audio preparation passed. Refresh LuCI → Services → AirSIM. Start and pair.' ;;
 reboot_required) echo 'Audio is staged. Restart into OpenWrt once; preparation resumes automatically. Calls are not ready yet.' ;;
 *) fail 'Audio setup did not complete; inspect the installation error before using calls' ;;
esac
echo 'No phone call or SMS was initiated.'
