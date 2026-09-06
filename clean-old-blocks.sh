#!/bin/bash
#
# CKPool sharelog cleanup — prunes aged per-height directories from the ckpool
# log tree, while permanently protecting any height that produced a block.
#
# WHY THE PROTECTION EXISTS
# ckpool creates one directory per block height (hex-named, e.g. 000ebd93) and
# never removes them. Pruning by age alone is fine for the 99.99% that recorded
# nothing but ordinary shares — but the sharelog of a height you actually SOLVED
# is the per-share record behind a block you were paid for, and there is no
# second copy anywhere. An age-only prune deletes it on schedule like any other
# directory.
#
# So this script refuses to delete anything until it has a solve index, and it
# never deletes a height that appears in one. The index is append-only and
# persistent BY DESIGN: ckpool.log has a bounded window (it is not rotated by
# ckpool, and rotated copies get pruned below), so a solve from six months ago
# is no longer greppable anywhere. Rebuilding the index from scratch each run
# would silently shrink it, and the first prune after that would take the very
# directories this exists to protect.

set -uo pipefail

CKPOOL_DIR="${CASHSTRATUM_DIR:-${CKPOOL_DIR:-$HOME/cashstratum}}"
if [[ "$CKPOOL_DIR" == "$HOME/cashstratum" && ! -d "$HOME/cashstratum" && -d "$HOME/ckpool" ]]; then
    CKPOOL_DIR="$HOME/ckpool"
fi
CKPOOL_LOG_DIR="$CKPOOL_DIR/logs"
DAYS_TO_KEEP="${DAYS_TO_KEEP:-60}"
ROTATED_LOG_DAYS="${ROTATED_LOG_DAYS:-30}"
SOLVE_INDEX="${SOLVE_INDEX:-$CKPOOL_DIR/solved-heights.idx}"
LOG_FILE="$CKPOOL_LOG_DIR/cleanup.log"
DRY_RUN=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run) DRY_RUN=1; shift ;;
        *) echo "Unknown option: $1" >&2; exit 1 ;;
    esac
done

log_message() {
    local msg
    msg="[$(date '+%Y-%m-%d %H:%M:%S')] $1"
    [[ $DRY_RUN -eq 1 ]] && echo "$msg"
    # Guard on the directory, not just the redirect: bash reports a failed
    # redirection on its own stderr before `2>/dev/null` can suppress it, so an
    # abort for a missing log dir would print a confusing bash error ahead of
    # its own explanation.
    [[ -d "$(dirname "$LOG_FILE")" ]] && echo "$msg" >> "$LOG_FILE" 2>/dev/null
    return 0
}

die() { log_message "ABORT: $1"; echo "ABORT: $1" >&2; exit 1; }

[[ -d "$CKPOOL_LOG_DIR" ]] || die "log dir not found: $CKPOOL_LOG_DIR"

# --- Solve index -----------------------------------------------------------
# Merge every solve height currently visible in the logs into the persistent
# index. EVERY ckpool.log* is scanned, rotated copies included, and this runs
# BEFORE the rotated-log prune below -- otherwise deleting a rotated log would
# discard the only remaining evidence of a solve it recorded.
#
# Heights are stored as the 8-hex-digit directory name ckpool itself uses, so
# matching is a plain string compare against the dir basename and no arithmetic
# happens in the delete path.
update_solve_index() {
    local tmp height
    tmp="$(mktemp)" || return 1
    trap 'rm -f "$tmp"' RETURN

    [[ -f "$SOLVE_INDEX" ]] && cat "$SOLVE_INDEX" >> "$tmp"

    while IFS= read -r height; do
        [[ "$height" =~ ^[0-9]+$ ]] && printf '%08x\n' "$height" >> "$tmp"
    done < <(
        find "$CKPOOL_LOG_DIR" -maxdepth 1 -type f \( -name 'ckpool.log*' -o -name 'cashstratum.log*' \) -print0 2>/dev/null \
        | xargs -0 grep -ah -oE 'Solved and confirmed block [0-9]+' 2>/dev/null \
        | awk '{print $NF}'
    )

    # The index can only grow: tmp was seeded from the existing index and only
    # appended to, so sorting it is a superset by construction. No diff check
    # is needed, and adding one would only invent a way to fail.
    sort -u "$tmp" > "$tmp.sorted" || return 1
    mv "$tmp.sorted" "$SOLVE_INDEX" || return 1
    return 0
}

update_solve_index || die "could not build the solve index -- refusing to prune"
[[ -f "$SOLVE_INDEX" ]] || die "solve index missing after build -- refusing to prune"

PROTECTED_COUNT=$(wc -l < "$SOLVE_INDEX" | tr -d ' ')
log_message "Solve index: $PROTECTED_COUNT protected height(s) at $SOLVE_INDEX"

is_protected() { grep -qxF "$1" "$SOLVE_INDEX"; }

# --- Prune aged height dirs ------------------------------------------------
BEFORE_COUNT=$(find "$CKPOOL_LOG_DIR" -maxdepth 1 -type d -name '000*' | wc -l | tr -d ' ')
REMOVED=0
SKIPPED=0

[[ $DRY_RUN -eq 1 ]] && echo "Directories that would be removed:"

while IFS= read -r dir; do
    [[ -n "$dir" ]] || continue
    name="$(basename "$dir")"
    if is_protected "$name"; then
        SKIPPED=$((SKIPPED + 1))
        [[ $DRY_RUN -eq 1 ]] && echo "  PROTECTED (solved): $name"
        continue
    fi
    if [[ $DRY_RUN -eq 1 ]]; then
        echo "  $dir"
    else
        rm -rf "$dir" && REMOVED=$((REMOVED + 1))
    fi
done < <(find "$CKPOOL_LOG_DIR" -maxdepth 1 -type d -name '000*' -mtime +"$DAYS_TO_KEEP" 2>/dev/null | sort)

AFTER_COUNT=$(find "$CKPOOL_LOG_DIR" -maxdepth 1 -type d -name '000*' | wc -l | tr -d ' ')
log_message "Cleanup complete. Removed $REMOVED, protected $SKIPPED, $AFTER_COUNT remaining (was $BEFORE_COUNT)."

# --- Prune aged rotated logs ----------------------------------------------
# Safe only because the solve index was refreshed from these files above.
if [[ $DRY_RUN -eq 1 ]]; then
    echo ""
    echo "Old rotated logs that would be removed:"
    find "$CKPOOL_LOG_DIR" \( -name 'ckpool.log.*' -o -name 'cashstratum.log.*' \) -mtime +"$ROTATED_LOG_DAYS" 2>/dev/null | sort
else
    find "$CKPOOL_LOG_DIR" \( -name 'ckpool.log.*' -o -name 'cashstratum.log.*' \) -mtime +"$ROTATED_LOG_DAYS" -delete 2>/dev/null
fi
