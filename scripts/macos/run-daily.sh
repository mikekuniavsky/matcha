#!/bin/bash
# Started by the LaunchAgent that install-schedule.sh writes. Runs Matcha at most once a day,
# only on AC power and once the network is up, under caffeinate so the Mac stays awake until
# the digest is written (a scheduled wake on its own only gives a DarkWake of about 3 minutes).
set -u

DIR="${MATCHA_DIR:-$(cd "$(dirname "$0")/../.." && pwd)}"
STAMP="${MATCHA_STAMP:-$HOME/Library/Application Support/Matcha/last-run}"
today=$(date +%F)

log() { echo "$(date '+%F %T') $*"; }

if [ "$(cat "$STAMP" 2>/dev/null)" = "$today" ]; then
	log "already ran today, skipping"
	exit 0
fi

if ! pmset -g ps | grep -q "AC Power"; then
	log "on battery, skipping"
	exit 0
fi

# Wi-Fi can take a while to come back after a wake; wait up to about a minute.
online=0
for _ in $(seq 12); do
	if curl -sf --max-time 5 -o /dev/null http://captive.apple.com/hotspot-detect.html; then
		online=1
		break
	fi
	sleep 5
done
if [ "$online" != 1 ]; then
	log "offline, skipping"
	exit 0
fi

cd "$DIR" || exit 1
log "running Matcha in $DIR"
# -i: no idle sleep, -s: no system sleep while on AC (this is what keeps a closed lid awake).
caffeinate -i -s ./matcha
rc=$?
if [ "$rc" = 0 ]; then
	mkdir -p "$(dirname "$STAMP")"
	echo "$today" >"$STAMP"
fi
log "Matcha exited with $rc"
exit "$rc"
