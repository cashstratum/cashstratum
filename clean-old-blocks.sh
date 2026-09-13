#!/bin/bash
# Prune aged sharelogs while preserving every indexed solve height.
# Before first use, provide a verified complete historical index with
# --seed-index FILE (one eight-digit lowercase hexadecimal height per line).
# An empty seed explicitly declares that the pool has never solved a block.
# Seeding only initializes/extends protection; it never deletes logs or heights.
# Log scans cannot reconstruct solves whose logs have already disappeared.
set -uo pipefail

CASHSTRATUM_DIR="${CASHSTRATUM_DIR:-${CKPOOL_DIR:-$HOME/cashstratum}}"
CASHSTRATUM_LOG_DIR="$CASHSTRATUM_DIR/logs"
DAYS_TO_KEEP="${DAYS_TO_KEEP:-60}"
ROTATED_LOG_DAYS="${ROTATED_LOG_DAYS:-30}"
SOLVE_INDEX="${SOLVE_INDEX:-$CASHSTRATUM_DIR/solved-heights.idx}"
LOG_FILE="$CASHSTRATUM_LOG_DIR/cleanup.log"
INDEX_MARKER='# cashstratum solve index v1: verified baseline'
DRY_RUN=0
SEED_INDEX=''
WORK_DIR=''
LOCK_DIR=''

while [[ $# -gt 0 ]]; do
    case "$1" in
        --help|-h)
            echo 'Usage: clean-old-blocks.sh [--dry-run] [--seed-index FILE]'
            echo 'Set CASHSTRATUM_DIR to the pool tree. A verified historical solve index is required.'
            echo 'Seeds contain eight-digit lowercase hex heights; an empty seed declares no prior solves.'
            echo 'Seeding never prunes. Verify completeness before importing; logs cannot recover lost history.'
            exit 0 ;;
        --dry-run) DRY_RUN=1; shift ;;
        --seed-index)
            [[ $# -ge 2 && -n "$2" ]] || { echo '--seed-index requires a file' >&2; exit 1; }
            SEED_INDEX="$2"; shift 2 ;;
        *) echo "Unknown option: $1" >&2; exit 1 ;;
    esac
done

log_message() {
    local msg
    msg="[$(date '+%Y-%m-%d %H:%M:%S')] $1"
    echo "$msg"
    if [[ $DRY_RUN -eq 0 && -d "$CASHSTRATUM_LOG_DIR" ]]; then
        echo "$msg" >> "$LOG_FILE" 2>/dev/null || true
    fi
}
die() { log_message "ABORT: $1" >&2; exit 1; }
cleanup() {
    [[ -z "$WORK_DIR" ]] || rm -rf -- "$WORK_DIR"
    [[ -z "$LOCK_DIR" ]] || rmdir -- "$LOCK_DIR"
    return 0
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

[[ "$DAYS_TO_KEEP" =~ ^[0-9]+$ && "$ROTATED_LOG_DAYS" =~ ^[0-9]+$ ]] || die 'retention days must be nonnegative integers'
[[ -d "$CASHSTRATUM_LOG_DIR" ]] || die "log dir not found: $CASHSTRATUM_LOG_DIR"
# Serialize refresh and deletion, so a concurrent run cannot replace a newer
# index with an older snapshot. A leftover lock after SIGKILL requires review.
mkdir "${SOLVE_INDEX}.lock" 2>/dev/null || die "cannot lock $SOLVE_INDEX (another cleanup may be running)"
LOCK_DIR="${SOLVE_INDEX}.lock"
WORK_DIR=$(mktemp -d "${SOLVE_INDEX}.work.XXXXXX") || die 'cannot create index workspace'

# Empty legacy indexes are not evidence of a verified no-solve history.
# Accept them only when an explicit seed supplies that missing verification.
if [[ -z "$SEED_INDEX" ]]; then
    [[ -s "$SOLVE_INDEX" && -r "$SOLVE_INDEX" ]] || die 'a verified solve index is required; use --seed-index FILE before scheduling cleanup'
    grep -qxF "$INDEX_MARKER" "$SOLVE_INDEX" || die 'legacy index has no verified baseline; import it with --seed-index FILE after checking historical completeness'
fi
: > "$WORK_DIR/heights" || die 'cannot prepare index'
for index in "$SOLVE_INDEX" "$SEED_INDEX"; do
    [[ -n "$index" ]] || continue
    if [[ "$index" == "$SOLVE_INDEX" && ! -e "$index" ]]; then
        continue
    fi
    [[ -f "$index" && -r "$index" ]] || die "index is not a readable regular file: $index"
    cat -- "$index" > "$WORK_DIR/input" || die "cannot read index: $index"
    # Validate before trusting any index as protection. The single marker is
    # the explicit acknowledgement of an empty historical baseline.
    while IFS= read -r height || [[ -n "$height" ]]; do
        [[ "$height" == "$INDEX_MARKER" ]] && continue
        [[ "$height" =~ ^[0-9a-f]{8}$ ]] || die "invalid solve index entry in $index"
        printf '%s\n' "$height" >> "$WORK_DIR/heights" || die 'cannot extend index'
    done < "$WORK_DIR/input"
done

# Discover all old/new log files before pruning and preserve read/decompression
# errors. Process substitution would hide failures in find/gzip/grep.
find "$CASHSTRATUM_LOG_DIR" -maxdepth 1 \( -type f -o -type l \) \
    \( -name 'cashstratum.log*' -o -name 'ckpool.log*' \) -print0 > "$WORK_DIR/logs" \
    || die 'cannot enumerate pool logs'
[[ -s "$WORK_DIR/logs" ]] || die 'no pool logs found; refusing an unverified refresh'
read_log() {
    case "$1" in
        *.gz) gzip -cd -- "$1" ;;
        *.bz2|*.xz|*.zst|*.zip) echo "unsupported compressed log: $1" >&2; return 1 ;;
        *) cat -- "$1" ;;
    esac
}
while IFS= read -r -d '' logfile; do
    [[ -f "$logfile" && -r "$logfile" && ! -L "$logfile" ]] || die "pool log is not a readable regular file: $logfile"
    read_log "$logfile" | LC_ALL=C awk '
        { while (match($0, /Solved and confirmed block [0-9]+/)) {
            text = substr($0, RSTART, RLENGTH); sub(/.* /, "", text); print text;
            $0 = substr($0, RSTART + RLENGTH)
        } }
    ' > "$WORK_DIR/parsed" || die "cannot completely read pool log: $logfile"
    while IFS= read -r height; do
        [[ "$height" =~ ^[0-9]+$ && ${#height} -le 10 ]] || die 'invalid block height in pool log'
        decimal=$((10#$height))
        [[ $decimal -le 4294967295 ]] || die 'block height exceeds the directory format'
        printf '%08x\n' "$decimal" >> "$WORK_DIR/heights" || die 'cannot extend index'
    done < "$WORK_DIR/parsed"
done < "$WORK_DIR/logs"

LC_ALL=C sort -u "$WORK_DIR/heights" > "$WORK_DIR/sorted" || die 'cannot sort solve index'
printf '%s\n' "$INDEX_MARKER" > "$WORK_DIR/index" || die 'cannot prepare verified index'
cat "$WORK_DIR/sorted" >> "$WORK_DIR/index" || die 'cannot write verified index'
if [[ $DRY_RUN -eq 0 ]]; then
    mv -- "$WORK_DIR/index" "$SOLVE_INDEX" || die 'cannot publish solve index; refusing to prune'
fi
PROTECTED_COUNT=$(wc -l < "$WORK_DIR/sorted" | tr -d ' ')
log_message "Solve index: $PROTECTED_COUNT protected height(s) at $SOLVE_INDEX"
if [[ -n "$SEED_INDEX" ]]; then
    log_message 'Verified baseline prepared; no pruning performed. Run --dry-run before scheduling cleanup.'
    exit 0
fi

# Read protection from our validated snapshot. No failed index read can become
# an unprotected height decision once deletion begins.
find "$CASHSTRATUM_LOG_DIR" -maxdepth 1 -type d -name '000*' -mtime +"$DAYS_TO_KEEP" -print0 \
    > "$WORK_DIR/directories" || die 'cannot enumerate aged height directories'
while IFS= read -r -d '' directory; do
    height="${directory##*/}"
    [[ "$height" =~ ^[0-9a-f]{8}$ ]] || die "invalid height directory: $directory"
    if grep -qxF "$height" "$WORK_DIR/sorted"; then
        [[ $DRY_RUN -eq 0 ]] || echo "PROTECTED (solved): $height"
    else
        status=$?
        [[ $status -eq 1 ]] || die 'cannot read protection snapshot'
        if [[ $DRY_RUN -eq 1 ]]; then
            echo "WOULD REMOVE: $directory"
        else
            rm -rf -- "$directory" || die "cannot remove $directory"
        fi
    fi
done < "$WORK_DIR/directories"

# Only rotate files that were read successfully in this refresh; no recursive
# find can reach a different tree or newly appeared unindexed rotated file.
while IFS= read -r -d '' logfile; do
    case "${logfile##*/}" in cashstratum.log.*|ckpool.log.*) ;; *) continue ;; esac
    find "$logfile" -maxdepth 0 -type f -mtime +"$ROTATED_LOG_DAYS" -print > "$WORK_DIR/aged" \
        || die "cannot inspect rotated log: $logfile"
    [[ -s "$WORK_DIR/aged" ]] || continue
    if [[ $DRY_RUN -eq 1 ]]; then
        echo "WOULD REMOVE ROTATED LOG: $logfile"
    else
        rm -f -- "$logfile" || die "cannot remove rotated log: $logfile"
    fi
done < "$WORK_DIR/logs"
log_message 'Cleanup complete.'
