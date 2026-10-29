#!/bin/bash
# Removes the LaunchAgent written by install-schedule.sh and, unless --keep-wake is given,
# cancels the repeating pmset wake (with sudo). `pmset repeat cancel` clears every repeating
# power event, not only Matcha's.
set -eu

LABEL="io.matcha.daily"
PLIST_PATH="$HOME/Library/LaunchAgents/$LABEL.plist"

launchctl bootout "gui/$(id -u)/$LABEL" 2>/dev/null || true
rm -f "$PLIST_PATH"
echo "LaunchAgent $LABEL removed"

if [ "${1:-}" != "--keep-wake" ]; then
	sudo pmset repeat cancel
	pmset -g sched
fi
