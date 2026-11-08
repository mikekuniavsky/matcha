#!/bin/bash
# Re-indexes the digests with qmd after a successful Matcha run: `qmd update`, then `qmd embed`.
# Called by run-daily.sh. Skips quietly if qmd is not installed. Set QMD_BIN to its full path
# when launchd's minimal PATH cannot find it (see `which qmd` in Terminal).
set -u

log() { echo "$(date '+%F %T') $*"; }

QMD="${QMD_BIN:-$(command -v qmd || echo "$HOME/.bun/bin/qmd")}"
if [ ! -x "$QMD" ]; then
	log "qmd not found, skipping index update"
	exit 0
fi

log "qmd update"
"$QMD" update || { log "qmd update failed"; exit 1; }
log "qmd embed"
"$QMD" embed || { log "qmd embed failed"; exit 1; }
log "qmd done"
