#!/bin/bash

# CashStratum Monitor
#
# Follows the pool log and shows a filtered, colourised view:
#   - one compact row per active miner (address without the network prefix),
#   - one POOL row per minute instead of three Pool:{...} lines,
#   - one line per miner connect (+) and disconnect (-), with session length,
#   - quick reconnect loops folded into a single FLAP warning,
#   - blocks, difficulty changes (rental/password/pattern) and errors highlighted.
# Idle users and the pool's own API probes are hidden. Use --all for the raw log.
#
# Usage: monitor.sh [--all] [--groups FILE] [-h|--help]
#
# Environment:
#   CASHSTRATUM_DIR / CKPOOL_DIR  install dir holding logs/cashstratum.log
#   CS_MONITOR_GROUPS             groups file (see --help); --groups wins
#   CS_MONITOR_FLAP_SECS          sessions shorter than this count as a flap (default 5)
#   NO_COLOR                      disable colours when set to any value
#   CS_MONITOR_FOLLOW=0           read the whole log once instead of following it

usage() {
    cat <<'EOF'
Usage: monitor.sh [--all] [--groups FILE] [-h|--help]

  (default)       filtered view: active miners, pool summary, connects,
                  flaps, blocks, difficulty changes and errors
  --all           every log line, colourised (the previous monitor view)
  --groups FILE   label and colour chosen addresses (overrides CS_MONITOR_GROUPS
                  and <install dir>/monitor-groups.txt)

Groups file: one address per line, then a label and an optional colour
(green, yellow, blue, magenta, cyan, white, red or a 0-255 colour number).
Lines starting with # are ignored. The network prefix is optional.

  qexampleaddress0000000000000000000000000000  ours   green
  qexampleaddress1111111111111111111111111111  watch  yellow
EOF
}

MODE=filtered
GROUPS_FILE="${CS_MONITOR_GROUPS:-}"
GROUPS_EXPLICIT=0
[ -n "$GROUPS_FILE" ] && GROUPS_EXPLICIT=1
while [ $# -gt 0 ]; do
    case "$1" in
        --all) MODE=all ;;
        --groups)
            [ $# -ge 2 ] || { echo "monitor.sh: --groups needs a file" >&2; exit 2; }
            GROUPS_FILE="$2"; GROUPS_EXPLICIT=1; shift ;;
        --groups=*) GROUPS_FILE="${1#--groups=}"; GROUPS_EXPLICIT=1 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "monitor.sh: unknown option: $1" >&2; usage >&2; exit 2 ;;
    esac
    shift
done

# Resolve this script's own directory, following symlinks, so a shortcut such as
# ~/monitor.sh pointing into the install dir still resolves to the install dir.
# shellcheck disable=SC1007  # "CDPATH= cd" is a deliberate env prefix, not an assignment
_cs_script_dir() {
    local src="${BASH_SOURCE[0]}" dir hops=0
    while [ -L "$src" ] && [ "$hops" -lt 40 ]; do
        dir="$(CDPATH= cd -- "$(dirname -- "$src")" && pwd)"
        src="$(readlink -- "$src")"
        case "$src" in
            /*) ;;
            *) src="$dir/$src" ;;
        esac
        hops=$((hops + 1))
    done
    CDPATH= cd -- "$(dirname -- "$src")" && pwd
}
SCRIPT_DIR="$(_cs_script_dir)"
# Resolve the install dir by finding one that actually holds a log, so the script
# works from /opt, from a $HOME copy, and for root as well as the service user.
_cs_resolve_dir() {
    local d
    for d in "$@"; do
        [ -n "$d" ] || continue
        if [ -f "$d/logs/cashstratum.log" ] || [ -f "$d/logs/ckpool.log" ]; then
            printf '%s\n' "$d"
            return 0
        fi
    done
    return 1
}
CASHSTRATUM_DIR="${CASHSTRATUM_DIR:-${CKPOOL_DIR:-}}"
if [ -z "$CASHSTRATUM_DIR" ]; then
    CASHSTRATUM_DIR="$(_cs_resolve_dir "$SCRIPT_DIR" "${HOME:-}/cashstratum" "${HOME:-}/ckpool" /opt/cashstratum)" \
        || CASHSTRATUM_DIR="${HOME:-}/cashstratum"
fi
LOG_FILE="$CASHSTRATUM_DIR/logs/cashstratum.log"
[ -f "$LOG_FILE" ] || LOG_FILE="$CASHSTRATUM_DIR/logs/ckpool.log"

if [ ! -f "$LOG_FILE" ]; then
    echo "Error: Log file not found at $LOG_FILE" >&2
    echo "Set CASHSTRATUM_DIR environment variable or ensure CashStratum is installed at $CASHSTRATUM_DIR" >&2
    exit 1
fi

if [ "$GROUPS_EXPLICIT" = 0 ] && [ -f "$CASHSTRATUM_DIR/monitor-groups.txt" ]; then
    GROUPS_FILE="$CASHSTRATUM_DIR/monitor-groups.txt"
fi
if [ -n "$GROUPS_FILE" ] && [ ! -r "$GROUPS_FILE" ]; then
    echo "monitor.sh: groups file not readable: $GROUPS_FILE" >&2
    exit 2
fi

COLOR=1
[ -n "${NO_COLOR+x}" ] && COLOR=0
FLAP_SECS="${CS_MONITOR_FLAP_SECS:-5}"
case "$FLAP_SECS" in ''|*[!0-9]*) FLAP_SECS=5 ;; esac

if [ -t 1 ]; then
    clear
    BAR='▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓'
    TITLE="                 CASHSTRATUM MONITOR SYSTEM  ($MODE view)"
    if [ "$COLOR" = 1 ]; then
        printf '\033[1;32m%s\n%s\n%s\033[0m\n' "$BAR" "$TITLE" "$BAR"
        printf '\033[2mMonitoring: %s\nPress Ctrl+C to exit\033[0m\n\n' "$LOG_FILE"
    else
        printf '%s\n%s\n%s\nMonitoring: %s\nPress Ctrl+C to exit\n\n' "$BAR" "$TITLE" "$BAR" "$LOG_FILE"
    fi
fi

# mawk buffers its input when reading a pipe; -W interactive makes it line-buffered.
AWK_OPTS=()
# (stdin from /dev/null: awks without -W read "version" as a program and wait on stdin)
if awk -W version </dev/null 2>&1 | head -n 1 | grep -q mawk; then
    AWK_OPTS=(-W interactive)
fi

# shellcheck disable=SC2016  # the awk program is single-quoted on purpose
AWK_PROG='
function ord_init(   i) { for (i = 32; i < 127; i++) ORD[sprintf("%c", i)] = i }
function noprefix(s,   i) {
    i = index(s, ":")
    return i ? substr(s, i + 1) : s
}
function base_addr(s,   i) {
    s = noprefix(s); i = index(s, ".")
    return i ? substr(s, 1, i - 1) : s
}
function esc(code) { return COLOR ? "\033[" code "m" : "" }
function reset() { return COLOR ? "\033[0m" : "" }
function color_code(name) {
    if (name ~ /^[0-9]+$/) return "38;5;" name
    if (name == "red") return "31"
    if (name == "green") return "32"
    if (name == "yellow") return "33"
    if (name == "blue") return "34"
    if (name == "magenta") return "35"
    if (name == "cyan") return "36"
    if (name == "white") return "37"
    return ""
}
# Stable colour per address; reds are left out so they keep meaning "error".
function addr_color(a,   h, i, n) {
    if (a in GCOLOR && GCOLOR[a] != "") return "1;" GCOLOR[a]
    if (a in GLABEL) return "1"
    h = 0; n = length(a)
    for (i = 1; i <= n; i++) h = (h * 31 + ORD[substr(a, i, 1)]) % 9973
    return "38;5;" PALETTE[h % NPAL + 1]
}
function label(a) { return (a in GLABEL) ? " [" GLABEL[a] "]" : "" }
# "luck" in User/Pool lines is round effort: work since the last block found,
# as a percent of network difficulty (100 = one expected block of work).
function luck(v) {
    if (v == "") return ""
    v += 0
    return "  luck " sprintf(v < 10 ? "%.2f%%" : (v < 1000 ? "%.1f%%" : "%.0f%%"), v)
}
function jget(s, key,   i, rest, j) {
    i = index(s, "\"" key "\":")
    if (!i) return ""
    rest = substr(s, i + length(key) + 3)
    if (substr(rest, 1, 1) == "\"") {
        rest = substr(rest, 2); j = index(rest, "\"")
        return substr(rest, 1, j - 1)
    }
    j = match(rest, /[,}]/)
    return j ? substr(rest, 1, j - 1) : rest
}
function si(v,   u, i) {
    if (v == "") return "-"
    v += 0; u = "KMGTPEZ"; i = 0
    if (v < 1000) return sprintf("%d", v)
    while (v >= 1000 && i < 7) { v /= 1000; i++ }
    return sprintf(v < 10 ? "%.2f%s" : (v < 100 ? "%.1f%s" : "%.0f%s"), v, substr(u, i, 1))
}
function secs(t) {
    if (t !~ /^[0-9][0-9]:[0-9][0-9]:[0-9][0-9]/) return -1
    return substr(t, 1, 2) * 3600 + substr(t, 4, 2) * 60 + substr(t, 7, 2)
}
function since(a, b,   d) { d = b - a; if (d < 0) d += 86400; return d }
function dur(d) {
    if (d < 60) return d "s"
    if (d < 3600) return sprintf("%dm%02ds", int(d / 60), d % 60)
    return sprintf("%dh%02dm", int(d / 3600), int(d % 3600 / 60))
}
function out(code, text) {
    if (code != "" && COLOR) print esc(code) text reset()
    else print text
    fflush()
}
function is_api_probe(ip, worker) { return ip == "127.0.0.1" && worker ~ /\.cashstratum-api$/ }

BEGIN {
    ord_init()
    NPAL = split("39 45 51 75 81 111 117 141 147 171 177 183 207 213 219 214 220 226 190 154 118 84 49 43", PALETTE, " ")
    if (GROUPS != "") {
        while ((getline gl < GROUPS) > 0) {
            if (gl ~ /^[ \t]*(#|$)/) continue
            ng = split(gl, gf, /[ \t]+/)
            if (gf[1] == "") { for (k = 1; k < ng; k++) gf[k] = gf[k + 1]; ng-- }
            ga = noprefix(gf[1])
            GLABEL[ga] = (ng >= 2) ? gf[2] : ""
            GCOLOR[ga] = (ng >= 3) ? color_code(gf[3]) : ""
        }
        close(GROUPS)
    }
}

{
    line = $0
    if (substr(line, 1, 1) == "[" && (rb = index(line, "] ")) > 0) {
        ts = substr(line, 2, rb - 2); msg = substr(line, rb + 2)
        hms = substr(ts, 12, 8)
    } else {
        ts = ""; msg = line; hms = ""
    }
    now = secs(hms)
}

# ---------- --all: every line, coloured by type ----------
MODE == "all" {
    if (msg ~ /^(User|Worker) [^ ]+:\{/) { n = substr(msg, index(msg, " ") + 1); out(addr_color(base_addr(substr(n, 1, index(n, ":{") - 1))), line) }
    else if (msg ~ /^Authorised client|^Dropped client/) out(msg ~ /^Dropped/ ? "2" : "32", line)
    else if (msg ~ /BLOCK ACCEPTED|Solved and confirmed|Possible block solve|Block solved after|Submitting (possible )?block/) out("1;32", line)
    else if (msg ~ /[Rr]eject|[Ee]rror|ERROR|[Ff]ail|CRITICAL|Unable|[Ii]nvalid|RETURNED|NO RESPONSE/) out("1;31", line)
    else if (msg ~ /^Block hash changed/) out("1;35", line)
    else if (msg ~ /^Network diff set/) out("1;33", line)
    else if (msg ~ /detected from useragent|via password parameter|matched pattern/) out("33", line)
    else if (msg ~ /^ZMQ/) out("34", line)
    else if (msg ~ /^Pool:/) out("36", line)
    else if (msg ~ /^Stored local workbase|^Reaping client/) out("2", line)
    else out("", line)
    next
}

# ---------- filtered view ----------
msg ~ /^(User|Worker) [^ ]+:\{/ {
    kind = substr(msg, 1, index(msg, " ") - 1)
    n = substr(msg, length(kind) + 2); j = index(n, ":{")
    name = noprefix(substr(n, 1, j - 1)); js = substr(n, j + 1)
    h1 = jget(js, "hashrate1m"); rc = jget(js, "rejectedcount")
    if ((h1 == "0" || h1 == "") && (rc == "" || rc + 0 == 0)) next
    a = base_addr(name)
    row = sprintf("%s %s%s  %6s %6s %6s", hms, name, label(a), h1, jget(js, "hashrate5m"), jget(js, "hashrate1hr"))
    if (kind == "User") row = row sprintf("  %sw", jget(js, "workers"))
    row = row "  best " si(jget(js, "bestshare"))
    if (kind == "User") row = row luck(jget(js, "luck"))
    if (rc != "" && rc + 0 > 0) row = row "  rej " rc
    out(addr_color(a), row)
    next
}

# The pool does not always log all three Pool lines in a minute; only show
# values stamped in the same second as the diff line, never stale ones.
msg ~ /^Pool:\{"runtime"/ { P_users = jget(msg, "Users"); P_workers = jget(msg, "Workers"); P_idle = jget(msg, "Idle"); P_rt_at = hms; next }
msg ~ /^Pool:\{"hashrate1m"/ { P_h1 = jget(msg, "hashrate1m"); P_h5 = jget(msg, "hashrate5m"); P_h60 = jget(msg, "hashrate1hr"); P_hr_at = hms; next }
msg ~ /^Pool:\{"diff"/ {
    rej = jget(msg, "rejected") + 0
    row = hms " POOL "
    row = row (P_hr_at == hms ? sprintf(" %s / %s / %s", P_h1, P_h5, P_h60) : "  (hashrate not logged)")
    if (P_rt_at == hms) row = row sprintf("  users %s  workers %s  idle %s", P_users, P_workers, P_idle)
    row = row "  SPS " jget(msg, "SPS1m") luck(jget(msg, "luck"))
    code = "36"
    if (P_rej != "" && rej > P_rej) { row = row "  rejected +" si(rej - P_rej); code = "1;31" }
    P_rej = rej
    out(code, row)
    next
}

# "Client N set difficulty to D via password parameter" and rental/pattern
# floors arrive just before the matching Authorised line: tag the + row.
msg ~ /^Client [^ ]+ set difficulty to [0-9]+ via password parameter/ {
    split(msg, f, " "); TAG[f[2]] = TAG[f[2]] " d=" f[6] " (password)"; next
}
msg ~ /detected from useragent .* for client [^ ]+, applied difficulty/ {
    nf = split(msg, f, " "); cid = ""; for (k = 1; k < nf; k++) if (f[k] == "client") cid = f[k + 1]
    sub(/,$/, "", cid); TAG[cid] = TAG[cid] " " f[1] " floor " f[nf]; next
}
msg ~ /^Client [^ ]+ workername .* matched pattern .* applied difficulty/ {
    nf = split(msg, f, " "); TAG[f[2]] = TAG[f[2]] " pattern " f[nf]; next
}

msg ~ /^Authorised client [^ ]+ [^ ]+ worker / {
    # Authorised client ID IP worker W as user U with diff D
    split(msg, f, " ")
    cid = f[3]; ip = f[4]; w = f[6]; u = f[9]; d = f[12]
    t = (cid in TAG) ? TAG[cid] : ""; delete TAG[cid]
    if (is_api_probe(ip, w)) { API[cid] = 1; next }
    A_T[cid] = now; A_W[cid] = noprefix(w); A_IP[cid] = ip; A_U[cid] = base_addr(u)
    key = A_W[cid] "@" ip
    if ((key in F_N) && F_N[key] >= 2 && now >= 0 && since(F_LAST[key], now) < 60) next
    a = A_U[cid]
    out("1;32", sprintf("%s + %s%s  %s  d=%s%s", hms, A_W[cid], label(a), ip, d, t != "" ? "  [" substr(t, 2) "]" : ""))
    next
}

msg ~ /^Dropped client / {
    # Dropped client ID IP user U worker W [reason=R age=S]
    split(msg, f, " ")
    cid = f[3]; ip = f[4]; u = f[6]; w = f[8]
    if ((cid in API) || is_api_probe(ip, w)) { delete API[cid]; next }
    reason = ""; age = -1
    if (match(msg, / reason=[^ ]+/)) reason = substr(msg, RSTART + 8, RLENGTH - 8)
    if (match(msg, / age=[0-9.]+/)) age = int(substr(msg, RSTART + 5, RLENGTH - 5))
    if (age < 0 && (cid in A_T) && A_T[cid] >= 0 && now >= 0) age = since(A_T[cid], now)
    wn = noprefix(w); a = base_addr(u); key = wn "@" ip
    delete A_T[cid]
    # A flap is two or more sessions shorter than FLAP seconds within a minute;
    # the first short session still prints as a normal drop.
    if (age >= 0 && age < FLAP) {
        if ((key in F_LAST) && since(F_LAST[key], now) >= 60) F_N[key] = 0
        F_N[key]++; F_LAST[key] = now
        if (F_N[key] >= 2) {
            if (!(key in F_SHOWN) || since(F_SHOWN[key], now) >= 30) {
                F_SHOWN[key] = now
                out("1;31", sprintf("%s ! FLAP %s%s  %s  %d quick drops (last session %ss%s)", hms, wn, label(a), ip, F_N[key], age, reason != "" ? ", " reason : ""))
            }
            next
        }
    }
    out("31", sprintf("%s - %s%s  %s%s%s", hms, wn, label(a), ip, age >= 0 ? "  (" dur(age) ")" : "", reason != "" ? "  " reason : ""))
    next
}

msg ~ /^Stored local workbase/ { out("2", hms " " msg); next }
msg ~ /^Reaping client/ { out("2", hms " " msg); next }
msg ~ /SUBMIT BLOCK RETURNED|SUBMIT BLOCK GOT NO RESPONSE|had block [0-9]+ rejected|block submit rejected/ { out("1;31", hms " * " msg); next }
msg ~ /BLOCK ACCEPTED|Solved and confirmed|Possible block solve|Block solved after|Submitting (possible )?block/ { out("1;32", hms " * " msg); next }
msg ~ /^Block hash changed to / { out("1;35", hms " ━━━━ NEW NETWORK BLOCK ━━━━ " substr(msg, 23)); next }
msg ~ /^Network diff set to / { out("33", hms " # network diff " si(substr(msg, 21))); next }
msg ~ /^ZMQ block hash/ { next }
msg ~ /[Rr]eject|[Ee]rror|ERROR|[Ff]ail|CRITICAL|Unable|[Ii]nvalid|rate limited/ { out("1;31", hms " " msg); next }
{ out("", (hms != "" ? hms " " : "") msg) }
'

# The pool's logger writes a message longer than DEFLOGBUFSIZ - 2 (510) bytes as
# several lines stamped within a millisecond or two; long User/Worker stats lines
# get split this way. Rejoin them so every later rule sees the whole message.
# shellcheck disable=SC2016  # the awk program is single-quoted on purpose
JOIN_PROG='
function stamp(s,   rb) {
    if (substr(s, 1, 1) != "[" || !(rb = index(s, "] "))) return ""
    return substr(s, 1, rb)
}
# "[YYYY-MM-DD HH:MM:SS.mmm]" -> milliseconds since the start of the day
function ms(t) {
    return ((substr(t, 13, 2) * 60 + substr(t, 16, 2)) * 60 + substr(t, 19, 2)) * 1000 + substr(t, 22, 3)
}
function emit(s) { print s; fflush() }
{
    ts = stamp($0)
    if (PEND != "") {
        gap = ts != "" ? ms(ts) - ms(PTS) : -1
        if (gap >= 0 && gap <= 50) {
            PTS = ts
            c = substr($0, length(ts) + 2)
            PEND = PEND c
            if (length(c) == CHUNK) next
            emit(PEND); PEND = ""
            next
        }
        emit(PEND); PEND = ""
    }
    if (ts != "" && length($0) - length(ts) - 1 == CHUNK) { PEND = $0; PTS = ts; next }
    emit($0)
}
END { if (PEND != "") emit(PEND) }
'

if [ "${CS_MONITOR_FOLLOW:-1}" = 0 ]; then
    INPUT=(cat -- "$LOG_FILE")
else
    INPUT=(tail -n 10 -F -- "$LOG_FILE")
fi

# LC_ALL=C: the chunk size is in bytes, and gawk counts characters under UTF-8.
"${INPUT[@]}" 2>/dev/null | LC_ALL=C awk "${AWK_OPTS[@]}" -v CHUNK=510 "$JOIN_PROG" | awk "${AWK_OPTS[@]}" \
    -v MODE="$MODE" -v COLOR="$COLOR" -v FLAP="$FLAP_SECS" -v GROUPS="$GROUPS_FILE" \
    "$AWK_PROG"
