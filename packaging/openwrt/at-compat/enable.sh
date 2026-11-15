#!/bin/sh
# Install / remove the mu300-at compatibility wrapper (see mu300-at and README.md).
#
#   enable.sh --check     show what is there now, change nothing
#   enable.sh             install (stock script is kept as mu300-at.vendor)
#   enable.sh --disable   put the stock script back
#
# Only /opt/mu300/bin/mu300-at is touched.  /usr/bin/mu300-at is normally a
# symlink to it, so PATH lookups keep working and no other file is replaced.
set -u

BIN=${MU300_BIN_DIR:-/opt/mu300/bin}
AT=$BIN/mu300-at
VENDOR=$AT.vendor
HERE=${0%/*}
[ "$HERE" = "$0" ] && HERE=.
SRC=$HERE/mu300-at

log() { printf '%s\n' "$*" ; }

version_of() {
	[ -f "$1" ] || { echo missing; return; }
	if grep -q 'mu300-at compatibility wrapper' "$1" 2>/dev/null; then
		echo "compat-wrapper"
	elif [ "$(head -c 2 "$1" 2>/dev/null)" = '#!' ]; then
		# a shell script that is not ours: treat as the stock one.  Matching on the
		# stock script's wording would break the moment the vendor edits a comment.
		echo "stock"
	else
		echo "unknown"
	fi
}

show() {
	log "binary dir : $BIN"
	log "mu300-at   : $(version_of "$AT")"
	log "vendor     : $(version_of "$VENDOR")"
	log "usr symlink: $(readlink /usr/bin/mu300-at 2>/dev/null || echo none)"
	if [ -f /etc/airsim/voice-card ]; then
		log "voice-card : $(cat /etc/airsim/voice-card 2>/dev/null)"
	else
		log "voice-card : absent (voice commands are not pinned)"
	fi
}

case ${1:-} in
--check)
	show
	exit 0
	;;
--disable)
	if [ ! -f "$VENDOR" ]; then
		log "nothing to undo: $VENDOR does not exist"
		exit 0
	fi
	[ "$(version_of "$AT")" = compat-wrapper ] || {
		log "refusing: $AT is not the compat wrapper (found $(version_of "$AT"))"
		exit 1
	}
	cp -f "$VENDOR" "$AT" && chmod 755 "$AT" || exit 1
	log "restored $AT from $VENDOR"
	show
	exit 0
	;;
'') ;;
*)
	log "usage: enable.sh [--check|--disable]"
	exit 2
	;;
esac

[ -x "$SRC" ] || { log "missing $SRC"; exit 1; }
[ -d "$BIN" ] || { log "missing $BIN"; exit 1; }

case $(version_of "$AT") in
compat-wrapper)
	# already installed: refresh the wrapper from this directory
	cp -f "$SRC" "$AT" && chmod 755 "$AT" || exit 1
	log "refreshed $AT"
	exit 0
	;;
stock)
	cp -f "$AT" "$VENDOR" || exit 1
	chmod 755 "$VENDOR"
	;;
*)
	log "unexpected $AT: neither the stock script nor the wrapper; not touching it"
	exit 1
	;;
esac

cp -f "$SRC" "$AT" && chmod 755 "$AT" || {
	log "install failed, restoring stock script"
	cp -f "$VENDOR" "$AT" && chmod 755 "$AT"
	exit 1
}
log "installed compat wrapper at $AT (stock kept as $VENDOR)"
show
