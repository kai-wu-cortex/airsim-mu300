#!/bin/sh
# R21 passes commands via stdin. The v2026.10.11 client takes a single argument.
set -eu
original=${F50_AIRSIM_ROOT:-/tmp/f50-audio-stage}/mu300-at-original
seconds=3
if [ "${1:-}" = -t ]; then
    seconds=${2:-}
    shift 2
fi
case "$seconds" in
    ''|*[!0-9]*) exit 2 ;;
esac
[ "$seconds" -ge 1 ] && [ "$seconds" -le 99 ] && [ "$#" -eq 1 ] || exit 2
command=$1
if [ "$command" = - ]; then
    IFS= read -r command || exit 2
    # The agent closes stdin after sending exactly one newline-terminated command.
    extra=
    if IFS= read -r extra || [ -n "$extra" ]; then
        exit 2
    fi
fi
# Only pass supported voice and status commands to the modem.
case "$command" in
    ATD*';')
        number=${command#ATD}
        number=${number%;}
        digits=${number#+}
        case "$digits" in
            ''|*[!0-9]*) exit 2 ;;
        esac
        [ "${#digits}" -le 15 ] || exit 2
        exec "$original" -t "$seconds" "$command" ;;
    AT|AT+CLCC|AT+CHLD\?|AT+CPIN\?|AT+CSQ|AT+COPS\?|AT+CNUM|AT+CREG\?|AT+CEREG\?|ATA|AT+CHUP|AT+CHLD=0|AT+CHLD=1[0-9]|AT+CHLD=2)
        exec "$original" -t "$seconds" "$command" ;;
    *) exit 2 ;;
esac
