#!/bin/bash
# Installs a LaunchAgent that runs Matcha daily (via run-daily.sh) and, with sudo, a
# `pmset repeat wakeorpoweron` a few minutes before the run so a sleeping Mac wakes for it.
#
#   scripts/macos/install-schedule.sh --time 00:05 [--wake-lead 3] [--dir PATH]
#                                     [--no-wake] [--dry-run]
#
# Undo with scripts/macos/uninstall-schedule.sh.
set -eu

LABEL="io.matcha.daily"
HERE="$(cd "$(dirname "$0")" && pwd)"
DIR="$(cd "$HERE/../.." && pwd)"
TIME=""
LEAD=3
WAKE=1
DRY=0

usage() { sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }

while [ $# -gt 0 ]; do
	case "$1" in
	--time) TIME="$2"; shift 2 ;;
	--wake-lead) LEAD="$2"; shift 2 ;;
	--dir) DIR="$(cd "$2" && pwd)"; shift 2 ;;
	--no-wake) WAKE=0; shift ;;
	--dry-run) DRY=1; shift ;;
	*) usage ;;
	esac
done

if ! [[ "$TIME" =~ ^([01]?[0-9]|2[0-3]):([0-5][0-9])$ ]]; then
	echo "--time HH:MM is required (24-hour, local time)" >&2
	usage
fi
HOUR=$((10#${BASH_REMATCH[1]}))
MIN=$((10#${BASH_REMATCH[2]}))
[[ "$LEAD" =~ ^[0-9]+$ ]] && [ "$LEAD" -ge 1 ] && [ "$LEAD" -le 60 ] || { echo "--wake-lead must be 1-60 minutes" >&2; exit 2; }

w=$(((HOUR * 60 + MIN - LEAD + 1440) % 1440))
WAKE_AT=$(printf '%02d:%02d:00' $((w / 60)) $((w % 60)))

PLIST_PATH="$HOME/Library/LaunchAgents/$LABEL.plist"
LOG="$HOME/Library/Logs/matcha-daily.log"
PLIST="<?xml version=\"1.0\" encoding=\"UTF-8\"?>
<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">
<plist version=\"1.0\">
<dict>
	<key>Label</key><string>$LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>/bin/bash</string>
		<string>$HERE/run-daily.sh</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict><key>MATCHA_DIR</key><string>$DIR</string></dict>
	<!-- The calendar entry is the daily run; the 15-minute interval catches up on days when the
	     Mac was asleep, on battery or offline then. run-daily.sh skips once it has run today. -->
	<key>StartCalendarInterval</key>
	<dict><key>Hour</key><integer>$HOUR</integer><key>Minute</key><integer>$MIN</integer></dict>
	<key>StartInterval</key><integer>900</integer>
	<key>StandardOutPath</key><string>$LOG</string>
	<key>StandardErrorPath</key><string>$LOG</string>
</dict>
</plist>"

PMSET_CMD="sudo pmset repeat wakeorpoweron MTWRFSU $WAKE_AT"

if [ "$DRY" = 1 ]; then
	echo "# would write $PLIST_PATH"
	echo "$PLIST"
	echo "# would run: launchctl bootout gui/$(id -u)/$LABEL; launchctl bootstrap gui/$(id -u) $PLIST_PATH"
	[ "$WAKE" = 1 ] && echo "# would run: $PMSET_CMD"
	exit 0
fi

[ "$(uname)" = Darwin ] || { echo "this installs a macOS LaunchAgent; use --dry-run elsewhere" >&2; exit 1; }
[ -x "$DIR/matcha" ] || { echo "no matcha binary in $DIR (go build -o matcha .)" >&2; exit 1; }

mkdir -p "$(dirname "$PLIST_PATH")" "$(dirname "$LOG")"
echo "$PLIST" >"$PLIST_PATH"
plutil -lint "$PLIST_PATH" >/dev/null
launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$PLIST_PATH"
echo "LaunchAgent $LABEL loaded: runs at $(printf '%02d:%02d' "$HOUR" "$MIN"), catches up every 15 minutes; log in $LOG"

if [ "$WAKE" = 1 ]; then
	echo
	echo "Current power schedule (pmset -g sched):"
	pmset -g sched
	if pmset -g sched | grep -q "Repeating power events"; then
		echo "Note: pmset keeps one repeating wake/poweron event; the one above will be replaced."
	fi
	echo
	echo "Scheduling a daily wake at $WAKE_AT (needs your password):"
	echo "  $PMSET_CMD"
	$PMSET_CMD
	pmset -g sched
fi
