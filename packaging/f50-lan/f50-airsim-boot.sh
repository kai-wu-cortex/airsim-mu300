#!/bin/sh
set -eu
umask 077
export F50_AIRSIM_ROOT=/mnt/mu300-disk/extra/airsim-f50
export F50_AIRSIM_RUNTIME=/tmp/airsim-f50-runtime
cd "$F50_AIRSIM_ROOT"
mkdir -p "$F50_AIRSIM_RUNTIME"
chmod 700 "$F50_AIRSIM_RUNTIME"
exec >>"$F50_AIRSIM_RUNTIME/boot.log" 2>&1
[ "$(uname -r)" = 5.4.254-gb50db5b6224c ]
bin/mu300-audio-dsp drivers >/dev/null
[ "$(cat /sys/module/audio_reserve/parameters/missing)" = 0 ]
[ "$(cat /sys/module/audio_reserve/parameters/ranges)" = '0xaf700000,0x300000;0xafa00000,0x600000' ]
ready=no
for attempt in $(seq 1 25); do
    if state=$(mu300-at -t 3 AT+CLCC 2>/dev/null) &&
        [ "$(printf '%s\n' "$state" | tr -d '\r' | tail -n 1)" = OK ] &&
        ! printf '%s\n' "$state" | grep -q '^+CLCC:' &&
        ip -4 addr show dev br-lan | grep -q 'inet '; then
        ready=yes
        break
    fi
    sleep 3
done
if [ "$ready" != yes ]; then
    echo 'Modem or LAN was not ready and idle before the startup deadline' >&2
    exit 1
fi
unset state
r="$PWD/apk-files/usr/lib/airsim"
a="$r/alsa/usr"
export PATH="$PWD/bin:$a/bin:/opt/mu300/busybox-bin:$PATH"
export LD_LIBRARY_PATH="$a/lib"
export ALSA_CONFIG_PATH="$a/share/alsa/alsa.conf" ALSA_CONFIG_DIR="$a/share/alsa"
export MU300_AGDSP_FW="$r/deps/agdsp.bin"
printf '%s  %s\n' 378ceea78dbf013e10625e71efd6fab487d727e371884ae8daf078da2828b937 "$MU300_AGDSP_FW" | sha256sum -c -
if [ ! -f "$F50_AIRSIM_RUNTIME/dsp-ready" ]; then
    timeout 45 sh bin/mu300-audio-dsp start
    : > "$F50_AIRSIM_RUNTIME/dsp-ready"
fi
exec /bin/sh "$PWD/lan-supervisor.sh"
