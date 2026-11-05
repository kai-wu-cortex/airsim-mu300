#!/bin/sh
# Invoke inside f50-private-mount; both bind mounts stay private to AirSIM.
set -eu
export F50_AIRSIM_ROOT=${F50_AIRSIM_ROOT:-/tmp/f50-audio-stage}
cd "$F50_AIRSIM_ROOT"
umask 077
r="$PWD/apk-files/usr/lib/airsim"
a="$r/alsa/usr"
export PATH="$PWD/bin:$a/bin:/opt/mu300/busybox-bin:$PATH"
export LD_LIBRARY_PATH="$a/lib"
export ALSA_CONFIG_PATH="$a/share/alsa/alsa.conf" ALSA_CONFIG_DIR="$a/share/alsa"
export MU300_AGDSP_FW="$r/deps/agdsp.bin" MU300_RUN_DIR_LIB="$PWD/run-dir.sh"
export MU300_VOICE_TMP=${F50_AIRSIM_RUNTIME:-$PWD}/voice-self-test MU300_VOICE_CONF=/dev/null
export AIRSIM_RUNTIME_PROFILE=mu300-openwrt AIRSIM_VOICE_BACKEND=mu300
export AIRSIM_DATA_DIR="$PWD/lan-data"
mode=$(jsonfilter -i "$AIRSIM_DATA_DIR/management.json" -e '@.mode')
relay=$(jsonfilter -i "$AIRSIM_DATA_DIR/management.json" -e '@.relay_url' || :)
case "$mode:$relay" in
    local:) ;;
    *) echo 'Unapproved AirSIM Relay configuration' >&2; exit 1 ;;
esac
export AIRSIM_MU300_MODE="$mode" AIRSIM_MU300_RELAY="$relay"

mount --bind "$PWD/bin/mu300-voice" /opt/mu300/bin/mu300-voice
mount --bind "$PWD/f50-airsim-at.sh" /opt/mu300/bin/mu300-at
# AirSIM sees neither Bark's inbox nor a native SMS sender.
if [ -d /etc/mu300/sms-pool/msg ]; then
    mount --bind "$PWD/empty-sms" /etc/mu300/sms-pool/msg
fi
if [ -f /opt/mu300/bin/mu300-sms ]; then
    mount --bind "$PWD/deny-sms.sh" /opt/mu300/bin/mu300-sms
fi
pidfile=${F50_AIRSIM_RUNTIME:-$PWD}/lan-supervisor.pid
echo "$$" > "$pidfile"
trap 'exit 0' INT TERM HUP
trap '
    if [ -n "${bridge:-}" ]; then kill "$bridge" 2>/dev/null || :; fi
    if [ -n "${agent:-}" ]; then kill "$agent" 2>/dev/null || :; fi
    wait || :
    rm -f "$pidfile"
' EXIT
logdir=${F50_AIRSIM_RUNTIME:-$PWD}
"$r/airsim-agent" >"$logdir/agent.log" 2>&1 & agent=$!
"$r/airsim-mu300d" --interface br-lan --data "$AIRSIM_DATA_DIR" >"$logdir/bridge.log" 2>&1 & bridge=$!
while kill -0 "$agent" 2>/dev/null && kill -0 "$bridge" 2>/dev/null; do
    sleep 2
done
exit 1
